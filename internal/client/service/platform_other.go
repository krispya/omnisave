//go:build !linux

package service

// PlatformManager returns the service manager for this platform. macOS has a
// launch agent and Windows a scheduled task, and both are the same shape as
// the systemd unit — per-user, no administrator, started by the session. They
// are not written yet, so those platforms run the client from a terminal and
// say so plainly when asked (ADR-016).
func PlatformManager() Manager {
	return unsupported{}
}
