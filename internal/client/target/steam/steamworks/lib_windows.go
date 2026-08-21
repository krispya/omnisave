//go:build windows

package steamworks

import "syscall"

func openLibrary(path string) (uintptr, error) {
	handle, err := syscall.LoadLibrary(path)
	return uintptr(handle), err
}

func lookup(handle uintptr, name string) (uintptr, error) {
	address, err := syscall.GetProcAddress(syscall.Handle(handle), name)
	return uintptr(address), err
}
