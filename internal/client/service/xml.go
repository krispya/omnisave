package service

import (
	"encoding/xml"
	"strings"
)

// xmlText escapes a value for the text node of a generated native service
// definition. Both launchd and Task Scheduler definitions embed paths chosen
// on the device, so treating them as trusted markup would corrupt valid paths.
func xmlText(value string) string {
	var escaped strings.Builder
	_ = xml.EscapeText(&escaped, []byte(value))
	return escaped.String()
}
