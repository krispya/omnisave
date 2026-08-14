//go:build !linux && !darwin && !windows

package service

// PlatformManager returns the fallback manager on unsupported platforms.
func PlatformManager() Manager {
	return unsupported{}
}
