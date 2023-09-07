package xz_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/CAFxX/httpcompression"
	"github.com/CAFxX/httpcompression/contrib/ulikunitz/xz"
	pxz "github.com/ulikunitz/xz"
)

var _ httpcompression.CompressorProvider = &xz.Compressor{}

func TestXz(t *testing.T) {
	t.Parallel()

	s := []byte("hello world!")

	c, err := xz.New(pxz.WriterConfig{})
	if err != nil {
		t.Fatal(err)
	}
	b := &bytes.Buffer{}
	w := c.Get(b)
	w.Write(s)
	w.Close()

	r, err := pxz.NewReader(b)
	if err != nil {
		t.Fatal(err)
	}
	d, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s, d) {
		t.Fatalf("decoded string mismatch\ngot: %q\nexp: %q", string(s), string(d))
	}
}
