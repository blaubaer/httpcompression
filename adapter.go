package httpcompression // import "github.com/CAFxX/httpcompression"

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"sync"

	"github.com/CAFxX/httpcompression/contrib/andybalholm/brotli"
	"github.com/CAFxX/httpcompression/contrib/compress/flate"
	cgzip "github.com/CAFxX/httpcompression/contrib/compress/gzip"
	"github.com/CAFxX/httpcompression/contrib/klauspost/zstd"
)

const (
	vary            = "Vary"
	acceptEncoding  = "Accept-Encoding"
	contentEncoding = "Content-Encoding"
	contentType     = "Content-Type"
	contentLength   = "Content-Length"
	gzipEncoding    = "gzip"
)

type codings map[string]float64

const (
	// DefaultMinSize is the default minimum response body size for which we enable compression.
	//
	// 200 is a somewhat arbitrary number; in experiments compressing short text/markup-like sequences
	// with different compressors we saw that sequences shorter that ~180 the output generated by the
	// compressor would sometime be larger than the input.
	// This default may change between versions.
	// In general there can be no one-size-fits-all value: you will want to measure if a different
	// minimum size improves end-to-end performance for your workloads.
	DefaultMinSize = 200
)

// Adapter returns a HTTP handler wrapping function (a.k.a. middleware)
// which can be used to wrap an HTTP handler to transparently compress the response
// body if the client supports it (via the Accept-Encoding header).
// It is possible to pass one or more options to modify the middleware configuration.
// If no options are provided, no compressors are enabled and therefore the adapter
// is a no-op.
// An error will be returned if invalid options are given.
func Adapter(opts ...Option) (func(http.Handler) http.Handler, error) {
	c := config{
		prefer:     PreferServer,
		compressor: comps{},
	}
	for _, o := range opts {
		err := o(&c)
		if err != nil {
			return nil, err
		}
	}

	if len(c.compressor) == 0 {
		// No compressors have been configured, so there is no useful work
		// that this adapter can do.
		return func(h http.Handler) http.Handler {
			return h
		}, nil
	}

	bufPool := &sync.Pool{}
	writerPool := &sync.Pool{}

	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addVaryHeader(w.Header(), acceptEncoding)

			accept := parseEncodings(r.Header.Get(acceptEncoding))
			common := acceptedCompression(accept, c.compressor)
			if len(common) == 0 {
				h.ServeHTTP(w, r)
				return
			}

			gw, _ := writerPool.Get().(*compressWriter)
			if gw == nil {
				gw = &compressWriter{}
			}
			*gw = compressWriter{
				ResponseWriter: w,
				config:         c,
				accept:         accept,
				common:         common,
				pool:           bufPool,
			}
			defer func() {
				// Important: gw.Close() must be called *always*, as this will
				// in turn Close() the compressor. This is important because
				// it is guaranteed by the CompressorProvider interface, and
				// because some compressors may be implemented via cgo, and they
				// may rely on Close() being called to release memory resources.
				// TODO: expose the error
				_ = gw.Close() // expose the error
				*gw = compressWriter{}
				writerPool.Put(gw)
			}()

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
	value = http.CanonicalHeaderKey(value)
	for _, v := range h.Values(vary) {
		if http.CanonicalHeaderKey(v) == value {
			return
		}
	}
	h.Add(vary, value)
}

// DefaultAdapter is like Adapter, but it includes sane defaults for general usage.
// Currently the defaults enable gzip and brotli compression, and set a minimum body size
// of 200 bytes.
// The provided opts override the defaults.
// The defaults are not guaranteed to remain constant over time: if you want to avoid this
// use Adapter directly.
func DefaultAdapter(opts ...Option) (func(http.Handler) http.Handler, error) {
	defaults := []Option{
		DeflateCompressionLevel(flate.DefaultCompression),
		GzipCompressionLevel(gzip.DefaultCompression),
		BrotliCompressionLevel(brotli.DefaultCompression),
		defaultZstandardCompressor(),
		MinSize(DefaultMinSize),
	}
	opts = append(defaults, opts...)
	return Adapter(opts...)
}

// Used for functional configuration.
type config struct {
	minSize      int                 // Specifies the minimum response size to gzip. If the response length is bigger than this value, it is compressed.
	contentTypes []parsedContentType // Only compress if the response is one of these content-types. All are accepted if empty.
	blacklist    bool
	prefer       PreferType
	compressor   comps
}

type comps map[string]comp

type comp struct {
	comp     CompressorProvider
	priority int
}

// Option can be passed to Handler to control its configuration.
type Option func(c *config) error

// MinSize is an option that controls the minimum size of payloads that
// should be compressed. The default is DefaultMinSize.
func MinSize(size int) Option {
	return func(c *config) error {
		if size < 0 {
			return fmt.Errorf("minimum size can not be negative: %d", size)
		}
		c.minSize = size
		return nil
	}
}

// DeflateCompressionLevel is an option that controls the Deflate compression
// level to be used when compressing payloads.
// The default is flate.DefaultCompression.
func DeflateCompressionLevel(level int) Option {
	c, err := flate.New(flate.Options{Level: level})
	if err != nil {
		return errorOption(err)
	}
	return DeflateCompressor(c)
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
	c, err := brotli.New(brotli.Options{Quality: level})
	if err != nil {
		return errorOption(err)
	}
	return BrotliCompressor(c)
}

// DeflateCompressor is an option to specify a custom compressor factory for Deflate.
func DeflateCompressor(g CompressorProvider) Option {
	return Compressor(flate.Encoding, -300, g)
}

// GzipCompressor is an option to specify a custom compressor factory for Gzip.
func GzipCompressor(g CompressorProvider) Option {
	return Compressor(gzipEncoding, -200, g)
}

// BrotliCompressor is an option to specify a custom compressor factory for Brotli.
func BrotliCompressor(b CompressorProvider) Option {
	return Compressor(brotli.Encoding, -100, b)
}

// ZstandardCompressor is an option to specify a custom compressor factory for Zstandard.
func ZstandardCompressor(b CompressorProvider) Option {
	return Compressor(zstd.Encoding, -50, b)
}

func NewDefaultGzipCompressor(level int) (CompressorProvider, error) {
	return cgzip.New(cgzip.Options{Level: level})
}

func defaultZstandardCompressor() Option {
	zstdComp, err := zstd.New()
	if err != nil {
		return errorOption(fmt.Errorf("initializing zstd compressor: %w", err))
	}
	return ZstandardCompressor(zstdComp)
}

func errorOption(err error) Option {
	return func(_ *config) error {
		return err
	}
}
