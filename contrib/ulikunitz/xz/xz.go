package xz

import (
	"fmt"
	"io"

	"github.com/CAFxX/httpcompression/contrib/internal/utils"
	"github.com/ulikunitz/xz"
)

const (
	Encoding = "xz"
)

type compressor struct {
	opts xz.WriterConfig
}

func New(opts xz.WriterConfig) (c *compressor, err error) {
	defer func() {
		if r := recover(); r != nil {
			c, err = nil, fmt.Errorf("panic: %v", r)
		}
	}()

	tw, err := opts.NewWriter(io.Discard)
	if err != nil {
		return nil, fmt.Errorf("xz: writer initialization: NewWriter: %w", err)
	}
	if err := utils.CheckWriter(tw); err != nil {
		return nil, fmt.Errorf("xz: writer initialization: %w", err)
	}

	c = &compressor{opts: opts}
	return c, nil
}

func (c *compressor) Get(w io.Writer) io.WriteCloser {
	gw, _ := c.opts.NewWriter(w)
	return &xzWriter{gw}
}

type xzWriter struct {
	*xz.Writer
}
