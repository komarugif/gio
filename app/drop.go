// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"net/url"
	"os"
	"strings"

	"gioui.org/f32"
)

// DropEvent tells of files another program drags over the window and
// drops on it. A drag that comes over the window starts with DropEnter,
// goes on with a DropMove for each move, and ends with DropLeave, or with
// Drop when it is let go over the window. Only drags that carry local files
// are told of.
type DropEvent struct {
	Kind DropKind
	// Position is where the pointer is, in pixels from the top left of
	// what the program draws.
	Position f32.Point
	// Paths are the files dragged, as far as they are known yet: a platform
	// may learn them some time after the drag came over the window, and
	// then tells them with a DropMove. A Drop brings them, or none if they
	// could not be had.
	Paths []string
}

// DropKind is the stage of a drag.
type DropKind uint8

const (
	// DropEnter is a drag that came over the window.
	DropEnter DropKind = iota
	// DropMove is a drag that moved over the window, or whose files came
	// to be known.
	DropMove
	// DropLeave is a drag that left the window, or was given up.
	DropLeave
	// Drop is a drag let go over the window.
	Drop
)

func (DropEvent) ImplementsEvent() {}

func (k DropKind) String() string {
	switch k {
	case DropEnter:
		return "DropEnter"
	case DropMove:
		return "DropMove"
	case DropLeave:
		return "DropLeave"
	case Drop:
		return "Drop"
	}
	return "DropKind(?)"
}

// uriListPaths reads the local files of a text/uri-list (RFC 2483): a
// URI a line, comments after #. URIs that are not of local files are left
// out.
func uriListPaths(list string) []string {
	var paths []string
	for _, line := range strings.Split(list, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Scheme != "file" || !localHost(u.Host) {
			continue
		}
		if u.Path != "" {
			paths = append(paths, u.Path)
		}
	}
	return paths
}

// localHost reports whether the host of a file URI is this machine: none,
// localhost, or its name, which some file managers write.
func localHost(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	name, err := os.Hostname()
	return err == nil && strings.EqualFold(host, name)
}

// uriList writes Windows paths as a text/uri-list.
func uriList(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		u := url.URL{Scheme: "file", Path: strings.ReplaceAll(p, `\`, "/")}
		if strings.HasPrefix(u.Path, "//") {
			host, rest, _ := strings.Cut(u.Path[2:], "/")
			u.Host, u.Path = host, "/"+rest
		} else {
			u.Path = "/" + u.Path
		}
		b.WriteString(u.String() + "\r\n")
	}
	return b.String()
}
