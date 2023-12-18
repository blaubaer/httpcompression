package httpcompression

import (
	"fmt"
	"github.com/CAFxX/httpcompression/contrib/andybalholm/brotli"
	cgzip "github.com/CAFxX/httpcompression/contrib/compress/gzip"
	"github.com/CAFxX/httpcompression/contrib/compress/zlib"
	"github.com/CAFxX/httpcompression/contrib/klauspost/zstd"
	"log"
	"net/http"
)

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
	c, err := zlib.New(zlib.Options{Level: level})
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
	return Compressor(zlib.Encoding, -300, g)
}

// GzipCompressor is an option to specify a custom compressor factory for Gzip.
func GzipCompressor(g CompressorProvider) Option {
	return Compressor(cgzip.Encoding, -200, g)
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

func ErrorHandler(handler func(w http.ResponseWriter, r *http.Request, err error)) Option {
	return func(c *config) error {
		c.errorHandler = handler
		return nil
	}
}

// Used for functional configuration.
type config struct {
	minSize      int                 // Specifies the minimum response size to gzip. If the response length is bigger than this value, it is compressed.
	contentTypes []parsedContentType // Only compress if the response is one of these content-types. All are accepted if empty.
	blacklist    bool
	prefer       PreferType
	compressor   comps
	errorHandler func(w http.ResponseWriter, r *http.Request, err error)
}

func (c config) handleError(w http.ResponseWriter, r *http.Request, err error) {
	if c.errorHandler != nil {
		c.errorHandler(w, r, err)
	} else {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		log.Printf("ERROR: %v", err)
	}
}

type comps map[string]comp

type comp struct {
	comp     CompressorProvider
	priority int
}
