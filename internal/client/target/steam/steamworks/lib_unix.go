//go:build darwin || linux

package steamworks

import "github.com/ebitengine/purego"

func openLibrary(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

func lookup(handle uintptr, name string) (uintptr, error) {
	return purego.Dlsym(handle, name)
}
