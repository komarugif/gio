// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"image"
	"os"
	"reflect"
	"testing"

	"gioui.org/f32"
)

func TestURIListPaths(t *testing.T) {
	name, _ := os.Hostname()
	list := "# a comment\r\nfile:///home/me/a%20b.png\r\nfile://localhost/tmp/%D1%84.txt\r\n" +
		"file://" + name + "/srv/c\r\nfile://elsewhere/d\r\nhttps://example.com/e\r\n\r\nfile:///f"
	want := []string{"/home/me/a b.png", "/tmp/ф.txt", "/srv/c", "/f"}
	if got := uriListPaths(list); !reflect.DeepEqual(got, want) {
		t.Fatalf("%q, want %q", got, want)
	}
}

// Drops wait in order, a move after a move replaces it and keeps the files
// it told, and positions leave out the fallback decorations.
func TestDropEventsQueue(t *testing.T) {
	w := new(Window)
	w.lastFrame.off = image.Pt(0, 30)
	for _, e := range []DropEvent{
		{Kind: DropEnter, Position: f32.Pt(10, 40)},
		{Kind: DropMove, Position: f32.Pt(11, 41), Paths: []string{"/a"}},
		{Kind: DropMove, Position: f32.Pt(12, 42)},
		{Kind: Drop, Position: f32.Pt(13, 43), Paths: []string{"/a"}},
	} {
		w.processEvent(e)
	}
	want := []DropEvent{
		{Kind: DropEnter, Position: f32.Pt(10, 10)},
		{Kind: DropMove, Position: f32.Pt(12, 12), Paths: []string{"/a"}},
		{Kind: Drop, Position: f32.Pt(13, 13), Paths: []string{"/a"}},
	}
	for i, want := range want {
		e, ok := w.nextEvent()
		if got, _ := e.(DropEvent); !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("event %d: %#v, want %#v", i, e, want)
		}
	}
	if e, ok := w.nextEvent(); ok {
		t.Fatalf("one more event: %#v", e)
	}
}

func TestURIListOfWindowsPaths(t *testing.T) {
	got := uriList([]string{`C:\Users\a b\pic.png`, `\\server\share\doc.pdf`})
	want := "file:///C:/Users/a%20b/pic.png\r\nfile://server/share/doc.pdf\r\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
