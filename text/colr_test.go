// SPDX-License-Identifier: Unlicense OR MIT

package text

import (
	"image"
	"image/color"
	"os"
	"testing"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype/tables"
	"github.com/go-text/typesetting/fontscan"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"

	giofont "gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/op"
)

// layeredEmoji returns a shaper on the system's fonts, its emoji face and
// that face's glyph of U+1F600, when the glyph is a COLR version 0 one, as
// in the Segoe UI Emoji of Windows 10. The test is skipped otherwise.
func layeredEmoji(t *testing.T) (*shaperImpl, *font.Face, font.GID, font.GlyphColor) {
	s := newShaperImpl(true, nil)
	s.query = fontscan.Query{Families: []string{fontscan.Emoji}}
	s.fontMap.SetQuery(s.query)
	face := s.ResolveFace('\U0001F600')
	if face == nil {
		t.Skip("no emoji font")
	}
	gid, ok := face.NominalGlyph('\U0001F600')
	if !ok {
		t.Skip("no emoji font")
	}
	glyph, ok := face.GlyphData(gid).(font.GlyphColor)
	if !ok {
		t.Skip("the emoji font has no color layers")
	}
	if _, ok := glyph.Paint.(tables.PaintColrLayersResolved); !ok {
		t.Skip("the emoji font's layers are not COLR version 0")
	}
	return s, face, gid, glyph
}

// TestColorGlyphImage checks that the layers of a color glyph are drawn in
// their colors, inside the glyph's box.
func TestColorGlyphImage(t *testing.T) {
	_, face, gid, glyph := layeredEmoji(t)
	const ppem = 64
	img, off, ok := colorGlyphImage(face, gid, glyph, fixed.I(ppem))
	if !ok {
		t.Fatal("the glyph was not drawn")
	}
	if size := img.Bounds().Size(); size.X < ppem/2 || size.X > ppem*2 || size.Y < ppem/2 || size.Y > ppem*2 {
		t.Errorf("the image is %v for %d pixels per em", size, ppem)
	}
	// The face sits on the baseline or a little under it, and left of it
	// nothing is drawn.
	if off.Y > -ppem/2 || off.Y < -ppem*2 || off.X < -ppem/4 || off.X > ppem/2 {
		t.Errorf("the image is at %v from the glyph's origin", off)
	}
	colors := map[color.RGBA]int{}
	for y := range img.Rect.Dy() {
		for x := range img.Rect.Dx() {
			if c := img.RGBAAt(x, y); c.A == 0xff {
				colors[c]++
			}
		}
	}
	if len(colors) < 2 {
		t.Errorf("%d opaque colors in the image, want the layers' several", len(colors))
	}
	if c := img.RGBAAt(0, 0); c.A != 0 {
		t.Errorf("the corner of a round face is %v, want transparent", c)
	}
	if c := img.RGBAAt(img.Rect.Dx()/2, img.Rect.Dy()/4); c.A != 0xff {
		t.Errorf("the forehead is %v, want opaque", c)
	}
}

// TestColorGlyphBitmap checks that Bitmaps shows a color glyph, which Shape
// has no outline for.
func TestColorGlyphBitmap(t *testing.T) {
	s, face, gid, _ := layeredEmoji(t)
	id := newGlyphID(fixed.I(32), s.faceToIndex[face.Font], gid)
	s.Bitmaps(new(op.Ops), []Glyph{{ID: id}})
	shown, ok := s.bitmapGlyphCache.Get(id)
	if !ok || shown.size == (image.Point{}) {
		t.Fatalf("no image of the color glyph: cached %v, size %v", ok, shown.size)
	}
}

// TestEmojiFaceInBoldAndMonospace checks that an emoji is found in queries
// whose aspect or family the emoji font does not match, as a theme's bold
// text and its monospace text are, with a font of the program's own loaded.
func TestEmojiFaceInBoldAndMonospace(t *testing.T) {
	own, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	s := newShaperImpl(true, []FontFace{{Face: own}})
	// Asked for by its family, the emoji font is found without a fallback.
	s.query = fontscan.Query{Families: []string{fontscan.Emoji}}
	s.fontMap.SetQuery(s.query)
	emoji := s.ResolveFace('\U0001F600')
	if emoji == nil {
		t.Skip("no emoji font")
	}
	if _, ok := emoji.NominalGlyph('\U0001F600'); !ok {
		t.Skip("no emoji font")
	}
	sans := []string{"Noto Sans", "Noto Color Emoji", "sans-serif"}
	for _, q := range []struct {
		name     string
		families []string
		font     giofont.Font
	}{
		{"bold", sans, giofont.Font{Weight: giofont.Bold}},
		{"italic", sans, giofont.Font{Style: giofont.Italic}},
		{"monospace", []string{"Noto Sans Mono", "monospace"}, giofont.Font{}},
	} {
		s.query = fontscan.Query{Families: q.families, Aspect: opentype.FontToDescription(q.font).Aspect}
		s.fontMap.SetQuery(s.query)
		face := s.ResolveFace('\U0001F600')
		if face == nil {
			t.Errorf("%s: no face", q.name)
			continue
		}
		if _, ok := face.NominalGlyph('\U0001F600'); !ok {
			t.Errorf("%s: the face found has no emoji", q.name)
		}
		// The text itself stays in the query's fonts.
		if text := s.ResolveFace('a'); text == face {
			t.Errorf("%s: a letter went to the emoji's face", q.name)
		}
	}
}

// TestEmojiFamily checks that the emoji family of the shaper draws the
// emoji and the emoji sequences before the system's emoji font does, and
// leaves the rest to the text's fonts. A second font of the system that
// has emoji, a symbol font, stands for a font of the program's own.
func TestEmojiFamily(t *testing.T) {
	system := newShaperImpl(true, nil)
	resolve := func(family string) *font.Face {
		system.fontMap.SetQuery(fontscan.Query{Families: []string{family}})
		face := system.fontMap.ResolveFace('\U0001F600')
		if face == nil {
			return nil
		}
		if _, ok := face.NominalGlyph('\U0001F600'); !ok {
			return nil
		}
		return face
	}
	systemEmoji := resolve(fontscan.Emoji)
	if systemEmoji == nil {
		t.Skip("no emoji font")
	}
	var second *font.Face
	for _, family := range []string{"Segoe UI Symbol", "Noto Emoji", "Symbola", "DejaVu Sans", "Twemoji Mozilla", "Apple Symbols"} {
		if face := resolve(family); face != nil && face.Font != systemEmoji.Font {
			second = face
			break
		}
	}
	if second == nil {
		t.Skip("no second font with emoji")
	}
	data, err := os.ReadFile(system.fontMap.FontLocation(second.Font).File)
	if err != nil {
		t.Skip(err)
	}
	faces, err := opentype.ParseCollection(data)
	if err != nil {
		t.Fatal(err)
	}
	s := newShaperImpl(true, faces)
	s.emojiFamily = string(faces[0].Font.Typeface)
	s.query = fontscan.Query{Families: []string{"sans-serif", fontscan.Emoji}}
	s.fontMap.SetQuery(s.query)
	own := s.ownEmojiFace()
	if own == nil {
		t.Fatal("the emoji family has no face")
	}
	for _, c := range []struct {
		text string
		own  bool
	}{
		{"\U0001F600", true},
		{"\U0001F600️", true},
		{"a", false},
		{"1", false},
	} {
		if _, ok := own.NominalGlyph([]rune(c.text)[0]); !ok {
			continue
		}
		face := s.resolveFaceAt([]rune(c.text), 0)
		if got := face == own; got != c.own {
			t.Errorf("%q in the emoji family's face: %v, want %v", c.text, got, c.own)
		}
	}
	// The query is the text's again.
	if face := s.ResolveFace('a'); face == own {
		t.Error("a letter is in the emoji family's face after the emoji")
	}
}

func TestEmojiPresentation(t *testing.T) {
	for r, want := range map[rune]bool{
		'\U0001F600': true, '\U0001F1F7': true, '⌚': true, '⭐': true, '✅': true, '❤': true, '☀': true,
		'1': false, '#': false, '©': false, '→': false, '↔': false, 'a': false, '中': false,
	} {
		if got := emojiPresentation(r); got != want {
			t.Errorf("%U: %v, want %v", r, got, want)
		}
	}
}
