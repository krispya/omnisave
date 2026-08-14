//go:build darwin

package service

// PlatformManager returns the current user's macOS LaunchAgent manager.
func PlatformManager() Manager {
	manager, err := NewLaunchd()
	if err != nil {
		return unsupported{}
	}
	return manager
}
