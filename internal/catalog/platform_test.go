package catalog_test

import (
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
)

// Providers name platforms inconsistently: No-Intro style with a dash,
// dashless with the company duplicated, self-branded, or bare. SplitPlatform
// keeps the company and the platform's own name separate in every case.
func TestSplitPlatform(t *testing.T) {
	cases := []struct {
		raw     string
		company string
		name    string
	}{
		{"Nintendo - Super Nintendo Entertainment System", "Nintendo", "Super Nintendo Entertainment System"},
		{"Nintendo Super Nintendo Entertainment System", "Nintendo", "Super Nintendo Entertainment System"},
		{"Nintendo Entertainment System", "Nintendo", "Nintendo Entertainment System"},
		{"Sega Saturn", "Sega", "Sega Saturn"},
		{"Nintendo 64", "Nintendo", "Nintendo 64"},
		{"Super Nintendo Entertainment System", "", "Super Nintendo Entertainment System"},
		{"Linux", "", "Linux"},
		{"", "", ""},
	}
	for _, tc := range cases {
		company, name := catalog.SplitPlatform(tc.raw)
		if company != tc.company || name != tc.name {
			t.Errorf("SplitPlatform(%q) = %q, %q; want %q, %q", tc.raw, company, name, tc.company, tc.name)
		}
	}
}
