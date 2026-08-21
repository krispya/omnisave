package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

// The verbose scan explains where save discovery looked. A game reading "no
// save available" is otherwise indistinguishable from a game the community
// manifest has never heard of, so the view names every location a profile
// rule reached and what was there. It is a debugging view: it prefers the
// specific fact over the gentle one, and its output is meant to be pasted
// into an issue.

// verboseIndent nests one game's findings under it, matching the track
// report's event indent so both reports read the same way.
const (
	verboseIndent = "      "
	pathIndent    = "        "
	fileIndent    = "          "
)

// listedFiles caps the files named under one save. A save holding hundreds
// would bury every other game in the report, and the names past the first few
// say nothing the count does not.
const listedFiles = 5

// renderVerbose explains one adapter's scan: each target, each game beneath
// it, and what the game's save-location rules reached.
func renderVerbose(scans []client.TargetScan) string {
	var view strings.Builder
	for _, scan := range scans {
		location := scan.Target.Location
		if location == "" {
			location = scan.Target.Root
		}
		fmt.Fprintf(&view, "  %s  %s\n",
			mutedStyle.Render(shorten(filepath.Clean(location))),
			mutedStyle.Render(scan.Target.Source))
		if len(scan.Games) == 0 {
			fmt.Fprintf(&view, "%s%s\n", verboseIndent, mutedStyle.Render("No supported games found"))
			continue
		}
		width := 0
		for _, game := range scan.Games {
			if length := len([]rune(gameTitle(game))); length > width {
				width = length
			}
		}
		for _, game := range scan.Games {
			view.WriteString(renderVerboseGame(game, width))
		}
	}
	return view.String()
}

func gameTitle(game client.GameScan) string {
	return game.Game.Identity.DisplayTitle(game.Game.ID)
}

// renderVerboseGame is one game's line and the findings beneath it. The line
// says what is true of the game; every finding is one muted sentence, and
// paths sit beneath the sentence that counted them.
func renderVerboseGame(game client.GameScan, width int) string {
	title := gameTitle(game)
	glyph := mutedStyle.Render("○")
	if len(game.Saves) > 0 {
		glyph = successStyle.Render("✓")
	}
	padding := strings.Repeat(" ", width-len([]rune(title)))
	var view strings.Builder
	fmt.Fprintf(&view, "\n  %s %s%s  %s\n",
		glyph, nameStyle.Render(title), padding, mutedStyle.Render(verboseStatus(game)))
	for _, line := range verboseLines(game) {
		view.WriteString(line + "\n")
	}
	return view.String()
}

// verboseStatus is the standing state a game carries: what discovery holds
// for it, or the reason it holds nothing.
func verboseStatus(game client.GameScan) string {
	if len(game.Saves) > 0 {
		files, bytes := 0, int64(0)
		for _, save := range game.Saves {
			files += len(save.Files)
			for _, file := range save.Files {
				bytes += file.Size
			}
		}
		return count(files, "file") + " · " + formatBytes(bytes)
	}
	if game.Profile.Consulted && !game.Profile.Found {
		return "No save-location rules"
	}
	return "No save found"
}

// verboseLines is everything known about one game's discovery, in the order a
// reader needs it: what the game is, what its rules did, and what was found.
func verboseLines(game client.GameScan) []string {
	var lines []string
	sentence := func(text string) {
		lines = append(lines, verboseIndent+mutedStyle.Render(text))
	}
	path := func(text string) {
		lines = append(lines, pathIndent+mutedStyle.Render(text))
	}

	if identity := identityLine(game.Game); identity != "" {
		sentence(identity)
	}
	if game.Game.Environment.Runtime == target.RuntimeProton && game.Game.Environment.PrefixRoot != "" {
		sentence("Proton prefix " + shorten(game.Game.Environment.PrefixRoot))
	}

	// A profile save's files belong to the rules that found them, so they are
	// listed there rather than repeated as a save of their own. An adapter's
	// own save has no rule to explain it and gets its own block.
	found := make(map[string][]target.File)
	var adapterSaves []target.Save
	for _, save := range game.Saves {
		if _, fromProfile := save.Metadata["profile_provider"]; !fromProfile {
			adapterSaves = append(adapterSaves, save)
			continue
		}
		for _, file := range save.Files {
			found[file.LocationID] = append(found[file.LocationID], file)
		}
	}

	if refused := game.Profile.RefusedMirror; refused > 0 {
		sentence(fmt.Sprintf(
			"%s refused inside Steam's own cloud area, which is a transport and never a save",
			count(refused, "location")))
	}
	switch {
	case !game.Profile.Consulted:
		sentence("Save-location rules were not consulted")
	case errors.Is(game.Profile.Err, saveprofile.ErrNoSaveFolder):
		sentence("Steam keeps this game's cloud saves through its API, so it has no save folder here")
	case !game.Profile.Found:
		sentence("No save-location rules for " + storeIdentity(game.Game))
	default:
		sentence("Rules from " + game.Profile.Provider + " " + quoted(game.Profile.Title))
		for _, group := range groupOutcomes(game.Profile.Rules) {
			sentence(group.headline)
			for _, entry := range group.entries {
				path(entry.line)
				for _, file := range fileLines(found[entry.locationID], game.Game) {
					lines = append(lines, fileIndent+mutedStyle.Render(file))
				}
			}
		}
	}

	for _, save := range adapterSaves {
		lines = append(lines, renderVerboseSave(save, game.Game)...)
	}
	return lines
}

// fileLines names the files behind one location, capped so a save holding
// hundreds does not bury the games below it.
func fileLines(files []target.File, game target.InstalledGame) []string {
	var lines []string
	for index, file := range files {
		if index == listedFiles {
			lines = append(lines, fmt.Sprintf("and %d more", len(files)-listedFiles))
			break
		}
		lines = append(lines, fmt.Sprintf(
			"%s (%s)", relativeTo(file.Path, game), formatBytes(file.Size)))
	}
	return lines
}

// renderVerboseSave names one save the adapter found itself — a Steam Cloud
// mirror, say — which no save-location rule accounts for.
func renderVerboseSave(save target.Save, game target.InstalledGame) []string {
	var bytes int64
	for _, file := range save.Files {
		bytes += file.Size
	}
	lines := []string{verboseIndent + mutedStyle.Render(fmt.Sprintf(
		"%s save from the adapter, %s · %s",
		save.Kind, count(len(save.Files), "file"), formatBytes(bytes)))}
	for _, file := range fileLines(save.Files, game) {
		lines = append(lines, pathIndent+mutedStyle.Render(file))
	}
	return lines
}

// outcomeGroup is one headline and the locations it counted. Locations that
// met the same fate share a sentence: five sentences saying "not found" read
// as five problems when they are one.
type outcomeGroup struct {
	headline string
	entries  []outcomeEntry
}

// outcomeEntry is one location beneath a headline, keyed by the location it
// stands for so the files found there can be listed under it.
type outcomeEntry struct {
	line       string
	locationID string
}

// groupOutcomes gathers rule outcomes into one group per fate, ordered so the
// locations discovery actually reached come before the rules it never tried.
// A manifest entry spells one path once per platform it applies to, so a rule
// excluded here whose location another rule already reached is dropped: the
// place was searched, and naming it again as skipped reads as a second place.
func groupOutcomes(outcomes []saveprofile.RuleOutcome) []outcomeGroup {
	reached := make(map[string]bool)
	for _, outcome := range outcomes {
		if outcome.Outcome != saveprofile.OutcomeInapplicable {
			reached[outcome.Rule.ID] = true
		}
	}
	// One location excluded under several constraints is still one location,
	// so its constraints are collected onto a single entry.
	excluded := make(map[string][]string)
	var excludedOrder []string
	byHeadline := make(map[string][]outcomeEntry)
	var order []string
	for _, outcome := range outcomes {
		if outcome.Outcome == saveprofile.OutcomeInapplicable {
			if reached[outcome.Rule.ID] {
				continue
			}
			if _, seen := excluded[outcome.Rule.ID]; !seen {
				excludedOrder = append(excludedOrder, outcome.Rule.ID)
			}
			excluded[outcome.Rule.ID] = appendUnique(excluded[outcome.Rule.ID], constraint(outcome.Rule))
			continue
		}
		headline := headlineFor(outcome)
		if _, seen := byHeadline[headline]; !seen {
			order = append(order, headline)
		}
		byHeadline[headline] = append(byHeadline[headline], outcomeEntry{
			line: entryFor(outcome), locationID: outcome.Rule.ID,
		})
	}
	if len(excludedOrder) > 0 {
		headline := "# not applicable here"
		order = append(order, headline)
		byID := make(map[string]saveprofile.Rule, len(outcomes))
		for _, outcome := range outcomes {
			byID[outcome.Rule.ID] = outcome.Rule
		}
		for _, id := range excludedOrder {
			byHeadline[headline] = append(byHeadline[headline], outcomeEntry{
				line:       byID[id].Path + "  " + strings.Join(excluded[id], ", "),
				locationID: id,
			})
		}
	}
	sort.SliceStable(order, func(left, right int) bool {
		return headlineRank(order[left]) < headlineRank(order[right])
	})
	groups := make([]outcomeGroup, 0, len(order))
	for _, headline := range order {
		entries := byHeadline[headline]
		groups = append(groups, outcomeGroup{
			headline: strings.Replace(headline, "#", pluralNoun(len(entries), headline), 1),
			entries:  entries,
		})
	}
	return groups
}

// headlineFor names a fate with a "#" standing in for the count, so rules
// sharing a fate collapse onto one sentence before it is worded.
func headlineFor(outcome saveprofile.RuleOutcome) string {
	switch outcome.Outcome {
	case saveprofile.OutcomeFound:
		return "# searched"
	case saveprofile.OutcomeMissing:
		return "# not found"
	case saveprofile.OutcomeEmpty:
		return "# holding no files"
	case saveprofile.OutcomeUnreadable:
		return "# unreadable"
	case saveprofile.OutcomeLinked:
		return "# skipped as symlinks"
	case saveprofile.OutcomeAmbiguous:
		return "# with ambiguous casing"
	case saveprofile.OutcomeUnexpandable:
		return "# with a placeholder this device cannot fill"
	default:
		return "# with an unknown outcome"
	}
}

// headlineRank orders the groups: locations discovery reached come first,
// then trouble, then the rules it never tried.
func headlineRank(headline string) int {
	switch {
	case strings.HasSuffix(headline, "searched"):
		return 0
	case strings.HasSuffix(headline, "not found"):
		return 1
	case strings.HasSuffix(headline, "not applicable here"):
		return 9
	default:
		return 5
	}
}

// pluralNoun counts a group in the noun its fate calls for. A rule excluded
// before any path was expanded never became a location.
func pluralNoun(size int, headline string) string {
	noun := "location"
	if strings.HasSuffix(headline, "not applicable here") || strings.Contains(headline, "placeholder") {
		noun = "rule"
	}
	return count(size, noun)
}

// appendUnique keeps a constraint list free of the repeats a manifest entry
// creates when several of its spellings carry the same platform.
func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// entryFor is one location line: where the rule looked, and the template it
// came from. The template is what a maintainer compares against the manifest
// and what shows a substitution going wrong, so a debugging view carries both
// — except where the template only restates the path, which is most of them.
func entryFor(outcome saveprofile.RuleOutcome) string {
	if outcome.Path == "" {
		return outcome.Rule.Path
	}
	line := shorten(outcome.Path)
	if outcome.Files > 0 {
		line += fmt.Sprintf("  (%s)", count(outcome.Files, "file"))
	}
	if restatesPath(outcome.Rule.Path, shorten(outcome.Path)) {
		return line
	}
	return line + "  " + outcome.Rule.Path
}

// restatesPath reports whether a template says nothing the expanded path does
// not already show. A rule reading "<home>/Library/Preferences" beside
// "~/Library/Preferences" is the same sentence twice; one reading
// "<winAppData>/…/<storeUserId>" is not.
func restatesPath(template, path string) bool {
	restated := strings.ReplaceAll(filepath.ToSlash(template), "<home>", "~")
	return restated == filepath.ToSlash(path)
}

// constraint words why a rule never applied here.
func constraint(rule saveprofile.Rule) string {
	switch {
	case rule.OS != "" && rule.Store != "":
		return "for " + rule.OS + " on " + rule.Store
	case rule.OS != "":
		return "for " + rule.OS
	case rule.Store != "":
		return "for the " + rule.Store + " store"
	default:
		return "not applicable"
	}
}

// identityLine names the game the way discovery matched it, which is the
// first thing to check when the wrong entry answered.
func identityLine(game target.InstalledGame) string {
	var parts []string
	if identity := storeIdentity(game); identity != "" {
		parts = append(parts, identity)
	}
	if game.InstallRoot != "" {
		parts = append(parts, "installed at "+shorten(game.InstallRoot))
	}
	return strings.Join(parts, ", ")
}

func storeIdentity(game target.InstalledGame) string {
	if namespace, id, ok := game.Identity.StoreIdentifier(); ok {
		return namespace + " " + id
	}
	return game.ID
}

// unidentified games carry neither a store identifier nor an id, which only
// happens for adapters that name a game by its files alone.

func quoted(text string) string {
	if text == "" {
		return "(untitled entry)"
	}
	return `"` + text + `"`
}

// relativeTo shortens a discovered file for reading. Inside a Proton prefix
// the prefix is already named once above, so the path beneath it is what
// distinguishes one location from another.
func relativeTo(path string, game target.InstalledGame) string {
	prefix := game.Environment.PrefixRoot
	if game.Environment.Runtime == target.RuntimeProton && prefix != "" {
		if relative, err := filepath.Rel(prefix, path); err == nil && !strings.HasPrefix(relative, "..") {
			return relative
		}
	}
	return shorten(path)
}

var homeDirectory = sync.OnceValue(func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
})

// shorten writes a path against the home directory. It is shorter to read and
// keeps the account name out of a report meant to be pasted into an issue.
// Nothing is elided: a path a reader is meant to go and check must stay exact.
func shorten(path string) string {
	home := homeDirectory()
	if home == "" || path == home {
		return path
	}
	if relative, found := strings.CutPrefix(path, home+string(filepath.Separator)); found {
		return "~" + string(filepath.Separator) + relative
	}
	return path
}
