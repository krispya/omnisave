package catalog

import "strings"

// platformCompanies are hardware manufacturers that prefix provider platform
// names, as in No-Intro's "Nintendo - Super Nintendo Entertainment System".
var platformCompanies = []string{
	"Nintendo", "Sega", "Sony", "Microsoft", "Atari", "NEC", "SNK", "Bandai",
	"Commodore", "Coleco", "Mattel", "Philips", "Sinclair", "Magnavox", "GCE", "Fairchild",
}

// SplitPlatform separates a provider's platform name into the hardware company
// and the platform's own name. The company prefix is dropped from the name only
// when it is a duplication — the remainder still names the company, as in
// "Nintendo Super Nintendo Entertainment System" — so self-branded names like
// "Nintendo Entertainment System" or "Sega Saturn" stay whole.
func SplitPlatform(raw string) (company, name string) {
	name = strings.Join(strings.Fields(raw), " ")
	if before, after, found := strings.Cut(name, " - "); found {
		return strings.TrimSpace(before), strings.TrimSpace(after)
	}
	for _, candidate := range platformCompanies {
		if !strings.HasPrefix(name, candidate+" ") {
			continue
		}
		remainder := strings.TrimSpace(strings.TrimPrefix(name, candidate+" "))
		if remainder != "" && strings.Contains(remainder, candidate) {
			return candidate, remainder
		}
		return candidate, name
	}
	return "", name
}
