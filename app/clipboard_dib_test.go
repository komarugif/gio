// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func decodePNG(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestDIBToPNG(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	src.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	src.SetNRGBA(2, 1, color.NRGBA{0, 0, 255, 128})
	var enc bytes.Buffer
	png.Encode(&enc, src)
	dib, err := pngToDIB(enc.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	out, err := dibToPNG(dib)
	if err != nil {
		t.Fatal(err)
	}
	got := decodePNG(t, out)
	for _, p := range []image.Point{{0, 0}, {2, 1}, {1, 1}} {
		if a, b := color.NRGBAModel.Convert(got.At(p.X, p.Y)), src.NRGBAAt(p.X, p.Y); a != b {
			t.Errorf("at %v: %v, want %v", p, a, b)
		}
	}

	// 24 bits, top down, rows padded to 4 bytes.
	const w, h = 2, 2
	b := make([]byte, 40+8*h)
	binary.LittleEndian.PutUint32(b[0:], 40)
	binary.LittleEndian.PutUint32(b[4:], w)
	binary.LittleEndian.PutUint32(b[8:], uint32(0x100000000-h))
	binary.LittleEndian.PutUint16(b[14:], 24)
	copy(b[40:], []byte{0, 0, 255, 0, 255, 0, 0, 0})
	out, err = dibToPNG(b)
	if err != nil {
		t.Fatal(err)
	}
	got = decodePNG(t, out)
	if c := color.NRGBAModel.Convert(got.At(0, 0)); c != (color.NRGBA{255, 0, 0, 255}) {
		t.Errorf("24 bits: %v", c)
	}
	if c := color.NRGBAModel.Convert(got.At(1, 0)); c != (color.NRGBA{0, 255, 0, 255}) {
		t.Errorf("24 bits: %v", c)
	}

	// 32 bits whose alpha is all 0: opaque.
	dib[40+3] = 0
	for i := 40 + 3; i < len(dib); i += 4 {
		dib[i] = 0
	}
	out, _ = dibToPNG(dib)
	if _, _, _, a := decodePNG(t, out).At(1, 0).RGBA(); a != 0xffff {
		t.Errorf("alpha 0 kept: %x", a)
	}

	if _, err := dibToPNG(b[:30]); err == nil {
		t.Error("a short header was read")
	}
}
