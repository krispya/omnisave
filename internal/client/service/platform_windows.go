//go:build windows

package service

// PlatformManager returns the current user's Windows Task Scheduler manager.
func PlatformManager() Manager {
	manager, err := NewScheduledTask()
	if err != nil {
		return unsupported{}
	}
	return manager
}
