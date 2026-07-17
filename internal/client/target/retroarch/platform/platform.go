// Package platform describes emulator platforms supported by the RetroArch adapter.
package platform

// Profile maps one emulated platform to its RetroArch files.
type Profile struct {
	ID             string
	PlaylistNames  []string
	SaveExtensions []string
}
