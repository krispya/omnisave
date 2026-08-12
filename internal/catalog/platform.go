package catalog

import "strings"

// platformCompanies are hardware manufacturers that prefix provider platform
// names, as in No-Intro's "Nintendo - Super Nintendo Entertainment System".
var platformCompanies = []string{
	"Nintendo", "Sega", "Sony", "Microsoft", "Atari", "NEC", "SNK", "Bandai",
	"Commodore", "Coleco", "Mattel", "Philips", "Sinclair", "Magnavox", "GCE", "Fairchild",
}

// SplitPlatform separates a provider platform into company and non-duplicated name.
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
