package host

import "os"

// DeviceName is the name a Device takes on first run: the machine's hostname,
// or a placeholder when the hostname cannot be read. The name is only a
// default; once tracking state holds one, that name is the Device's.
func DeviceName() string {
	if name, err := os.Hostname(); err == nil && name != "" {
		return name
	}
	return "this-device"
}
