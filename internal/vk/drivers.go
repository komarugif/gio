// SPDX-License-Identifier: Unlicense OR MIT

//go:build linux || freebsd

package vk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Driver is a Vulkan driver as its manifest describes it: what the loader
// would load for it, found where the loader looks.
type Driver struct {
	// Manifest is the file name of the driver's manifest, such as
	// intel_icd.x86_64.json, which names it among the drivers.
	Manifest string
	// Library is the driver's library as the manifest gives it: a path,
	// or a name for dlopen to look for.
	Library string
}

// Drivers lists the Vulkan drivers whose manifests are where the loader
// looks for them on Linux: the configuration directories of XDG, /etc,
// then the data directories of XDG. A manifest found first hides those of
// the same name after it, as for the loader. Nothing is loaded.
func Drivers() []Driver {
	return driversIn(manifestDirs())
}

// manifestDirs lists the directories of driver manifests, in the order the
// loader searches them.
func manifestDirs() []string {
	home, _ := os.UserHomeDir()
	env := func(name, def string) string {
		if v := os.Getenv(name); v != "" {
			return v
		}
		return def
	}
	var dirs []string
	add := func(list string) {
		for _, d := range filepath.SplitList(list) {
			if d != "" {
				dirs = append(dirs, filepath.Join(d, "vulkan", "icd.d"))
			}
		}
	}
	if home != "" {
		add(env("XDG_CONFIG_HOME", filepath.Join(home, ".config")))
	}
	add(env("XDG_CONFIG_DIRS", "/etc/xdg"))
	add("/etc")
	if home != "" {
		add(env("XDG_DATA_HOME", filepath.Join(home, ".local", "share")))
	}
	add(env("XDG_DATA_DIRS", "/usr/local/share:/usr/share"))
	return dirs
}

func driversIn(dirs []string) []Driver {
	seen := make(map[string]bool)
	var drivers []Driver
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			if seen[name] {
				continue
			}
			seen[name] = true
			lib, ok := manifestLibrary(filepath.Join(dir, name))
			if !ok {
				continue
			}
			drivers = append(drivers, Driver{Manifest: name, Library: lib})
		}
	}
	return drivers
}

// manifestLibrary reads the library of a driver's manifest: a path with a
// slash is relative to the manifest's directory, as the loader takes it.
func manifestLibrary(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var m struct {
		ICD struct {
			LibraryPath string `json:"library_path"`
		} `json:"ICD"`
	}
	if err := json.Unmarshal(data, &m); err != nil || m.ICD.LibraryPath == "" {
		return "", false
	}
	lib := m.ICD.LibraryPath
	if strings.Contains(lib, "/") && !filepath.IsAbs(lib) {
		lib = filepath.Join(filepath.Dir(path), lib)
	}
	return lib, true
}
