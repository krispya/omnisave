package service

// PlatformManager returns the service manager for this platform, or one that
// reports every platform without an implementation as having no service.
// Linux is systemd's user instance, which is what a Steam Deck runs and what
// every desktop distribution Omnisave targets runs (ADR-016).
func PlatformManager() Manager {
	manager, err := NewSystemd()
	if err != nil {
		// A user with no resolvable config directory or account is a device
		// that cannot hold a service. Nothing else about the client depends
		// on it, so this reads as "no service here".
		return unsupported{}
	}
	return manager
}
