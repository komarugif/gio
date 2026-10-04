// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"math/bits"
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

// dibToPNG encodes a CF_DIB or CF_DIBV5 of 24 or 32 bits a pixel as a PNG.
func dibToPNG(b []byte) ([]byte, error) {
	if len(b) < 40 {
		return nil, errors.New("dib: short header")
	}
	le := binary.LittleEndian
	size := int(le.Uint32(b[0:]))
	w, h := int(int32(le.Uint32(b[4:]))), int(int32(le.Uint32(b[8:])))
	depth, compression := int(le.Uint16(b[14:])), le.Uint32(b[16:])
	colors := int(le.Uint32(b[32:]))
	topDown := h < 0
	if topDown {
		h = -h
	}
	if size < 40 || w <= 0 || h <= 0 || w*h > 1<<28 || depth != 24 && depth != 32 || compression != 0 && compression != 3 {
		return nil, errors.New("dib: unsupported bitmap")
	}
	masks := [4]uint32{0xff0000, 0xff00, 0xff, 0}
	offset := size + colors*4
	if compression == 3 {
		at := 40
		if size == 40 {
			offset += 12
		}
		if len(b) < at+12 {
			return nil, errors.New("dib: short masks")
		}
		for i := range 3 {
			masks[i] = le.Uint32(b[at+i*4:])
		}
		if size >= 56 {
			masks[3] = le.Uint32(b[at+12:])
		}
	} else if depth == 32 {
		masks[3] = 0xff000000
	}
	stride := (w*depth + 31) / 32 * 4
	if len(b) < offset+stride*h {
		return nil, errors.New("dib: short pixels")
	}
	channel := func(px, mask uint32) uint8 {
		if mask == 0 {
			return 0xff
		}
		shift := bits.TrailingZeros32(mask)
		return uint8((px & mask) >> shift * 255 / (mask >> shift))
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	noAlpha := true
	for y := range h {
		row := b[offset+y*stride:]
		if !topDown {
			row = b[offset+(h-1-y)*stride:]
		}
		dst := img.Pix[y*img.Stride:]
		for x := range w {
			var px uint32
			if depth == 24 {
				px = uint32(row[x*3]) | uint32(row[x*3+1])<<8 | uint32(row[x*3+2])<<16
			} else {
				px = le.Uint32(row[x*4:])
			}
			dst[x*4], dst[x*4+1], dst[x*4+2], dst[x*4+3] = channel(px, masks[0]), channel(px, masks[1]), channel(px, masks[2]), channel(px, masks[3])
			noAlpha = noAlpha && dst[x*4+3] == 0
		}
	}
	if noAlpha && masks[3] != 0 {
		// Alpha left at 0: opaque.
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 0xff
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
