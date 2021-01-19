package httpcompression // import "github.com/CAFxX/httpcompression"

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"sync"

	"github.com/CAFxX/httpcompression/contrib/andybalholm/brotli"
	_brotli "github.com/andybalholm/brotli"
)

const (
	vary            = "Vary"
	acceptEncoding  = "Accept-Encoding"
	contentEncoding = "Content-Encoding"
	contentType     = "Content-Type"
	contentLength   = "Content-Length"
	gzipEncoding    = "gzip"
	brotliEncoding  = "br"
)

type codings map[string]float64

const (
	// DefaultMinSize is the default minimum size for which we enable compression.
	// 20 is a very conservative default borrowed from nginx: you will probably want
	// to measure if a higher minimum size improves performance for your workloads.
	DefaultMinSize = 20
)

// Adapter returns a HTTP handler wrapping function (a.k.a. middleware)
// which can be used to wrap an HTTP handler to transparently compress the response
// body if the client supports it (via the Accept-Encoding header).
// It is possible to pass one or more options to modify the middleware configuration.
// An error will be returned if invalid options are given.
func Adapter(opts ...Option) (func(http.Handler) http.Handler, error) {
	c := config{
		prefer:     PreferServer,
		compressor: comps{},
	}
	for _, o := range opts {
		o(&c)
		if c.validationErr != nil {
			return nil, c.validationErr
		}
	}

	if err := c.validate(); err != nil {
		return nil, err
	}

	p := &sync.Pool{}

	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addVaryHeader(w.Header(), acceptEncoding)

			accept, err := parseEncodings(r.Header.Get(acceptEncoding))
			if err != nil {
				h.ServeHTTP(w, r)
				return
			}
			common := acceptedCompression(accept, c.compressor)
			if len(common) == 0 {
				h.ServeHTTP(w, r)
				return
			}

			gw := &compressWriter{
				ResponseWriter: w,
				config:         c,
				accept:         accept,
				common:         common,
				pool:           p,
			}
			defer gw.Close()

			if _, ok := w.(http.CloseNotifier); ok {
				w = compressWriterWithCloseNotify{gw}
			} else {
				w = gw
			}

			h.ServeHTTP(w, r)
		})
	}, nil
}

func addVaryHeader(h http.Header, value string) {
	for _, v := range h.Values(vary) {
		if v == value {
			return
		}
	}
	h.Add(vary, value)
}

// DefaultAdapter is like Adapter, but it includes sane defaults for general usage.
// The provided opts override the defaults.
// The defaults are not guaranteed to remain constant over time: if you want to avoid this
// use Adapter directly.
func DefaultAdapter(opts ...Option) (func(http.Handler) http.Handler, error) {
	defaults := []Option{
		GzipCompressionLevel(gzip.DefaultCompression),
		BrotliCompressionLevel(_brotli.DefaultCompression),
		MinSize(DefaultMinSize),
	}
	opts = append(defaults, opts...)
	return Adapter(opts...)
}

// Used for functional configuration.
type config struct {
	minSize       int                 // Specifies the minimum response size to gzip. If the response length is bigger than this value, it is compressed.
	contentTypes  []parsedContentType // Only compress if the response is one of these content-types. All are accepted if empty.
	blacklist     bool
	prefer        PreferType
	compressor    comps
	validationErr error
}

func (c *config) validate() error {
	if c.minSize < 0 {
		return fmt.Errorf("minimum size can not be negative: %d", c.minSize)
	}

	switch c.prefer {
	case PreferServer, PreferClient:
	default:
		return fmt.Errorf("invalid prefer config: %v", c.prefer)
	}

	return nil
}

// Option can be passed to Handler to control its configuration.
type Option func(c *config)

// MinSize is an option that controls the minimum size of payloads that
// should be compressed. The default is DefaultMinSize.
func MinSize(size int) Option {
	return func(c *config) {
		c.minSize = size
	}
}

// GzipCompressionLevel is an option that controls the Gzip compression
// level to be used when compressing payloads.
// The default is gzip.DefaultCompression.
func GzipCompressionLevel(level int) Option {
	c, err := NewDefaultGzipCompressor(level)
	if err != nil {
		return errorOption(err)
	}
	return GzipCompressor(c)
}

// BrotliCompressionLevel is an option that controls the Brotli compression
// level to be used when compressing payloads.
// The default is 3 (the same default used in the reference brotli C
// implementation).
func BrotliCompressionLevel(level int) Option {
	c, err := brotli.New(_brotli.WriterOptions{Quality: level})
	if err != nil {
		return errorOption(err)
	}
	return BrotliCompressor(c)
}

// GzipCompressor is an option to specify a custom compressor factory for Gzip.
func GzipCompressor(g CompressorProvider) Option {
	return Compressor(gzipEncoding, 0, g)
}

// BrotliCompressor is an option to specify a custom compressor factory for Brotli.
func BrotliCompressor(b CompressorProvider) Option {
	return Compressor(brotliEncoding, 1, b)
}

func errorOption(err error) Option {
	return func(c *config) {
		c.validationErr = err
	}
}
