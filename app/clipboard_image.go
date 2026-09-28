// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
	"image/png"
)

// pngToDIB decodes a PNG into a device-independent bitmap, as the Windows
// clipboard's CF_DIB holds one: a BITMAPINFOHEADER and 32-bit BGRA rows,
// bottom up.
func pngToDIB(b []byte) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	bounds := src.Bounds()
	rgba := image.NewNRGBA(image.Rectangle{Max: bounds.Size()})
	draw.Draw(rgba, rgba.Rect, src, bounds.Min, draw.Src)
	w, h := rgba.Rect.Dx(), rgba.Rect.Dy()
	const header = 40
	out := make([]byte, header+w*h*4)
	binary.LittleEndian.PutUint32(out[0:], header)
	binary.LittleEndian.PutUint32(out[4:], uint32(w))
	binary.LittleEndian.PutUint32(out[8:], uint32(h))
	binary.LittleEndian.PutUint16(out[12:], 1)  // planes
	binary.LittleEndian.PutUint16(out[14:], 32) // bits per pixel
	binary.LittleEndian.PutUint32(out[20:], uint32(w*h*4))
	pixels := out[header:]
	for y := range h {
		row := rgba.Pix[y*rgba.Stride : y*rgba.Stride+w*4]
		dst := pixels[(h-1-y)*w*4:]
		for x := range w {
			r, g, bl, a := row[x*4], row[x*4+1], row[x*4+2], row[x*4+3]
			dst[x*4], dst[x*4+1], dst[x*4+2], dst[x*4+3] = bl, g, r, a
		}
	}
	return out, nil
}
