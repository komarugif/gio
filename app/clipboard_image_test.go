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

// A PNG goes on the Windows clipboard as a bottom-up 32-bit BGRA bitmap.
func TestPNGToDIB(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	img.SetNRGBA(1, 1, color.NRGBA{R: 9, G: 8, B: 7, A: 128})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	dib, err := pngToDIB(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(dib) != 40+2*2*4 || binary.LittleEndian.Uint32(dib[4:]) != 2 || binary.LittleEndian.Uint16(dib[14:]) != 32 {
		t.Fatalf("header %v", dib[:40])
	}
	pixels := dib[40:]
	// The top row is last.
	if top := pixels[8:12]; !bytes.Equal(top, []byte{3, 2, 1, 255}) {
		t.Fatalf("the top-left pixel is %v", top)
	}
	if bottom := pixels[4:8]; !bytes.Equal(bottom, []byte{7, 8, 9, 128}) {
		t.Fatalf("the bottom-right pixel is %v", bottom)
	}
}
