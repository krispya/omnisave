package host

import (
	"os"
	"testing"
)

// fakeHost serves a fixed set of probe files, standing in for one machine.
func fakeHost(files map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		content, ok := files[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return []byte(content), nil
	}
}

func TestPlatformDistinguishesSteamDeckFromLinux(t *testing.T) {
	hosts := []struct {
		name  string
		goos  string
		files map[string]string
		want  string
	}{
		{
			name:  "plain linux desktop",
			goos:  "linux",
			files: map[string]string{"/etc/os-release": "ID=arch\n"},
			want:  "linux",
		},
		{
			name:  "steam deck lcd by hardware",
			goos:  "linux",
			files: map[string]string{"/sys/class/dmi/id/product_name": "Jupiter\n"},
			want:  PlatformSteamDeck,
		},
		{
			name:  "steam deck oled by hardware",
			goos:  "linux",
			files: map[string]string{"/sys/class/dmi/id/product_name": "Galileo\n"},
			want:  PlatformSteamDeck,
		},
		{
			name: "steamos without dmi data",
			goos: "linux",
			files: map[string]string{
				"/etc/os-release": "ID=steamos\nVARIANT_ID=\"steamdeck\"\n",
			},
			want: PlatformSteamDeck,
		},
		{
			name:  "other operating systems pass through",
			goos:  "darwin",
			files: map[string]string{"/sys/class/dmi/id/product_name": "Jupiter\n"},
			want:  "darwin",
		},
	}
	for _, host := range hosts {
		t.Run(host.name, func(t *testing.T) {
			if got := platform(host.goos, fakeHost(host.files)); got != host.want {
				t.Errorf("platform = %q, want %q", got, host.want)
			}
		})
	}
}
