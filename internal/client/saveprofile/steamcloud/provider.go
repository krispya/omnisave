package steamcloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/target/steam/locator"
)

// ProviderName identifies rules derived from Steam's own cloud configuration.
const ProviderName = "steam-ufs"

// Provider answers with the save locations Steam replicates for a game, as
// the game's developer declared them. It is a fallback for games the
// community manifest cannot place on this Device, never a replacement:
// consulted first it would rename the locations of lineages already minted
// under the community rules' spelling.
type Provider struct {
	// roots are Steam installation directories, each holding the appcache
	// this reads a game's configuration from and the userdata that tells an
	// Auto-Cloud game from one reaching Steam through the API.
	roots []string
	// cached indexes each appcache once rather than once per game asked
	// about, since a scan asks about every game the manifest cannot place.
	cached *cache
}

// New reads the cloud configuration cached under the given Steam
// installation roots, in order.
func New(steamRoots ...string) *Provider {
	return &Provider{roots: steamRoots, cached: newCache()}
}

// NewDefault reads the cloud configuration of the Steam installations this
// host conventionally has. A host with none answers nothing, as a host whose
// Steam has never cached the game does. Conventional locations can spell
// one installation twice — a symlink and its target — and canonicalizing
// keeps a large appcache from being parsed and held once per spelling.
func NewDefault() *Provider {
	roots, err := locator.DefaultRoots()
	if err != nil {
		return New()
	}
	seen := make(map[string]bool, len(roots))
	unique := make([]string, 0, len(roots))
	for _, root := range roots {
		canonical := root
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			canonical = resolved
		}
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		unique = append(unique, canonical)
	}
	return New(unique...)
}

// ErrCloudAPI reports a game that reaches Steam Cloud through the API rather
// than by having folders replicated. Steam's configuration then names no
// folder for this source to find — which is a different answer from never
// having heard of the game, and worth saying. It claims nothing beyond
// this source: an API game may still write an ordinary local folder that
// better rules can name, so the game's save location stays unknown rather
// than declared absent.
var ErrCloudAPI = fmt.Errorf(
	"%w: the game stores its Steam Cloud saves through the API", saveprofile.ErrUnplaceable)

// Find reports the folders Steam replicates for one Steam game.
func (p *Provider) Find(_ context.Context, identity target.GameIdentity) (*saveprofile.Profile, error) {
	appID, isSteam := identity.Identifier("steam.app")
	if !isSteam || appID == "" {
		return nil, saveprofile.ErrNotFound
	}
	numericAppID, err := strconv.ParseUint(appID, 10, 32)
	if err != nil {
		return nil, saveprofile.ErrNotFound
	}
	storesThroughAPI := false
	for _, root := range p.roots {
		file, err := p.cached.read(path.Join(root, "appcache", "appinfo.vdf"))
		if err != nil {
			// Missing, malformed, unreadable for permissions, or mid-rewrite
			// by Steam itself: a cache this host cannot read answers nothing,
			// exactly as one Steam never wrote. Discovery reports what it
			// could actually read and never fails the scan over it.
			continue
		}
		app, err := file.section(uint32(numericAppID))
		if err != nil || app == nil {
			continue
		}
		ufs := app.section("ufs")
		if ufs == nil {
			continue
		}
		// Declared folders are a configuration, not an observation. Where
		// Steam has actually synced this game, its own bookkeeping says
		// whether any file came from one of those folders or whether every
		// one of them was written through the API — and a game writing
		// through the API keeps no folder Steam can point at, whatever its
		// configuration still declares.
		if p.syncedThroughAPI(root, appID) {
			storesThroughAPI = true
			continue
		}
		saveFiles := ufs.section("savefiles")
		if saveFiles == nil || len(saveFiles.sections()) == 0 {
			continue
		}
		active := activeFolders(saveFiles, syncedPaths(userdataFor(root), appID))
		rules := p.rules(active, ufs.section("rootoverrides"))
		if len(rules) == 0 {
			continue
		}
		return &saveprofile.Profile{
			Provider:   ProviderName,
			ProviderID: appID,
			Title:      app.section("common").value("name"),
			Rules:      rules,
		}, nil
	}
	if storesThroughAPI {
		return nil, ErrCloudAPI
	}
	return nil, saveprofile.ErrNotFound
}

// activeFolders narrows a game's declared folders to the ones Steam has
// synced files from. Steam records each synced file by its path relative to
// the folder it came from, so a declared folder no recorded path lies under
// is one this game no longer uses — a root left behind by an old version, or
// one for a platform this Device is not.
//
// Evidence only ever narrows: a game Steam has never synced here records
// nothing, and paths that match no declared folder at all mean this reader
// has misunderstood the bookkeeping rather than that every folder is stale.
// Both leave the declaration as it stands.
func activeFolders(saveFiles *node, synced []string) []*node {
	entries := saveFiles.sections()
	if len(synced) == 0 {
		return entries
	}
	var active []*node
	for _, entry := range entries {
		declared := cleanRelative(entry.value("path"))
		for _, path := range synced {
			if pathLiesUnder(path, declared) {
				active = append(active, entry)
				break
			}
		}
	}
	if len(active) == 0 {
		return entries
	}
	return active
}

// pathLiesUnder reports whether a synced path lies under a declared folder.
// A declared folder may name the account owning the save, which is a segment
// the path answers and the declaration only describes.
func pathLiesUnder(path, declared string) bool {
	if declared == "" {
		return true
	}
	wanted := strings.Split(declared, "/")
	segments := strings.Split(path, "/")
	if len(segments) < len(wanted) {
		return false
	}
	for index, segment := range wanted {
		if strings.ContainsAny(segment, "{*") {
			continue
		}
		if !strings.EqualFold(segment, segments[index]) {
			return false
		}
	}
	return true
}

// syncedPaths is every file path Steam's bookkeeping records for one game,
// across every account on the Device, relative to the folder it came from.
func syncedPaths(userdata, appID string) []string {
	accounts, err := os.ReadDir(userdata)
	if err != nil {
		return nil
	}
	var paths []string
	for _, account := range accounts {
		if !account.IsDir() {
			continue
		}
		cached, err := os.ReadFile(path.Join(userdata, account.Name(), appID, "remotecache.vdf"))
		if err != nil {
			continue
		}
		paths = append(paths, cachedPaths(string(cached))...)
	}
	return paths
}

// cachedPaths reads the file names out of a remotecache: the sections one
// level in, each named by the file's path relative to its root. Depth is
// tracked rather than inferred from a name's shape, because a save file's
// name owes this reader nothing — it may carry spaces, or no dot or slash
// at all — while a key/value line always carries its value's quotes.
func cachedPaths(cache string) []string {
	var paths []string
	depth := 0
	for _, line := range strings.Split(cache, "\n") {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case "{":
			depth++
			continue
		case "}":
			depth--
			continue
		}
		if depth != 1 || len(trimmed) < 2 ||
			!strings.HasPrefix(trimmed, `"`) || !strings.HasSuffix(trimmed, `"`) {
			continue
		}
		name := trimmed[1 : len(trimmed)-1]
		if strings.Contains(name, `"`) {
			continue
		}
		paths = append(paths, name)
	}
	return paths
}

// rules turns each replicated folder into save-location rules. Steam
// declares one folder in Windows terms and then says how to reach the same
// place on the other operating systems, so one declaration becomes one rule
// per OS it can be spelled for — the same logical location in each OS's
// vocabulary, which is what lets one lineage follow a player between them
// (FDR-003, decision 11).
func (p *Provider) rules(entries []*node, rootOverrides *node) []saveprofile.Rule {
	overrides := readRootOverrides(rootOverrides)
	spellings := make(map[string]map[string]bool)
	var order []string
	for _, entry := range entries {
		for _, operatingSystem := range reachableOS(entry, overrides) {
			template := templateFor(entry, operatingSystem, overrides)
			if template == "" {
				continue
			}
			if _, known := spellings[template]; !known {
				spellings[template] = make(map[string]bool)
				order = append(order, template)
			}
			spellings[template][operatingSystem] = true
		}
	}

	var rules []saveprofile.Rule
	for _, template := range order {
		for _, operatingSystem := range constraintsFor(spellings[template]) {
			rules = append(rules, saveprofile.Rule{
				ID:    locationID(template),
				Path:  template,
				OS:    operatingSystem,
				Store: "steam",
				Kind:  "save",
			})
		}
	}
	return rules
}

// constraintsFor reports how a spelling should be constrained: a template
// every operating system reaches the same way needs no constraint at all,
// and one reached only from some of them names those.
func constraintsFor(operatingSystems map[string]bool) []string {
	if len(operatingSystems) == len(everyOS) {
		return []string{""}
	}
	constrained := make([]string, 0, len(operatingSystems))
	for _, operatingSystem := range everyOS {
		if operatingSystems[operatingSystem] {
			constrained = append(constrained, operatingSystem)
		}
	}
	return constrained
}

// templateFor builds one operating system's spelling of a replicated folder,
// or the empty string when that OS cannot reach it — a Windows folder with
// no override for macOS describes a place no game there ever writes.
func templateFor(entry *node, operatingSystem string, overrides []rootOverride) string {
	root := entry.value("root")
	relative := cleanRelative(entry.value("path"))
	for _, override := range overrides {
		if !override.appliesTo(root, operatingSystem) {
			continue
		}
		if override.useInstead != "" {
			root = override.useInstead
		}
		relative = override.transform(relative)
		if override.addPath != "" {
			relative = path.Join(override.addPath, relative)
		}
		break
	}
	placeholder := rootPlaceholder(root, operatingSystem)
	if placeholder == "" {
		return ""
	}
	template := placeholder
	if relative != "" {
		template += "/" + relative
	}
	if entry.value("recursive") == "1" {
		template += "/**"
	}
	pattern := entry.value("pattern")
	if pattern == "" {
		pattern = "*"
	}
	return steamTokens.Replace(template + "/" + pattern)
}

// steamTokens rewrites Steam's own path placeholders into the vocabulary the
// resolver expands. Both name the account owning the save, which discovery
// cannot know, so both become the any-account placeholder.
var steamTokens = strings.NewReplacer(
	"{64BitSteamID}", "<storeUserId>",
	"{Steam3AccountID}", "<storeUserId>",
)

// rootOverride is Steam's per-OS replacement for a declared root: another
// root to use instead, a path to insert beneath it, and edits to the path.
type rootOverride struct {
	root       string
	os         string
	compare    string
	useInstead string
	addPath    string
	transforms []pathTransform
}

type pathTransform struct{ find, replace string }

// appliesTo reports whether this override speaks for one root on one OS.
// Steam compares OS versions as well as names; every override in the wild
// asks only for equality, and one asking for anything else is left alone
// rather than applied on a comparison this reader never made.
func (o rootOverride) appliesTo(root, operatingSystem string) bool {
	return o.root == strings.ToLower(root) && o.os == operatingSystem && o.compare == "="
}

func (o rootOverride) transform(relative string) string {
	for _, transform := range o.transforms {
		if transform.find == "" {
			continue
		}
		relative = strings.ReplaceAll(relative, transform.find, transform.replace)
	}
	return cleanRelative(relative)
}

func readRootOverrides(overrides *node) []rootOverride {
	var parsed []rootOverride
	for _, entry := range overrides.sections() {
		override := rootOverride{
			root:       strings.ToLower(entry.value("root")),
			os:         normalizeOS(entry.value("os")),
			compare:    entry.value("oscompare"),
			useInstead: entry.value("useinstead"),
			addPath:    cleanRelative(entry.value("addpath")),
		}
		for _, transform := range entry.section("pathtransforms").sections() {
			override.transforms = append(override.transforms, pathTransform{
				find:    cleanRelative(transform.value("find")),
				replace: cleanRelative(transform.value("replace")),
			})
		}
		parsed = append(parsed, override)
	}
	return parsed
}

// rootPlaceholder maps a Steam root onto the save-profile vocabulary for one
// operating system. A root naming another OS's folder maps to nothing: that
// is Steam saying this game does not keep its saves there on this platform.
func rootPlaceholder(root, operatingSystem string) string {
	// SteamCloudDocuments is deliberately absent. It is a root this reader
	// cannot place: nothing here says which directory it expands to on any
	// operating system, and every app in a real appcache used one of the
	// roots below. Guessing would hand back a path some game does not use,
	// which is the failure this whole package exists to avoid, so a game
	// declaring it is left unknown for a source that does know.
	switch strings.ToLower(root) {
	case "gameinstall":
		return "<base>"
	case "winmydocuments", "windocuments":
		return onlyOn(operatingSystem, saveprofile.OSWindows, "<winDocuments>")
	case "winappdatalocal":
		return onlyOn(operatingSystem, saveprofile.OSWindows, "<winLocalAppData>")
	case "winappdatalocallow":
		return onlyOn(operatingSystem, saveprofile.OSWindows, "<winLocalAppDataLow>")
	case "winappdataroaming":
		return onlyOn(operatingSystem, saveprofile.OSWindows, "<winAppData>")
	case "winsavedgames":
		return onlyOn(operatingSystem, saveprofile.OSWindows, "<winSavedGames>")
	case "machome":
		return onlyOn(operatingSystem, saveprofile.OSMacOS, "<home>")
	case "macappsupport":
		return onlyOn(operatingSystem, saveprofile.OSMacOS, "<macAppSupport>")
	case "macdocuments":
		return onlyOn(operatingSystem, saveprofile.OSMacOS, "<home>/Documents")
	case "linuxhome":
		return onlyOn(operatingSystem, saveprofile.OSLinux, "<home>")
	case "linuxxdgdatahome":
		return onlyOn(operatingSystem, saveprofile.OSLinux, "<xdgData>")
	case "linuxxdgconfighome":
		return onlyOn(operatingSystem, saveprofile.OSLinux, "<xdgConfig>")
	default:
		return ""
	}
}

func onlyOn(operatingSystem, required, placeholder string) string {
	if operatingSystem == required {
		return placeholder
	}
	return ""
}

// everyOS is the operating systems a rule can be spelled for.
var everyOS = []string{saveprofile.OSLinux, saveprofile.OSMacOS, saveprofile.OSWindows}

// reachableOS reports the operating systems that can reach a replicated
// folder: the platforms it names, plus any an override was written for. An
// override exists precisely because that OS reaches the folder another way,
// so a platform list that omits it is describing where Steam syncs rather
// than where the game keeps its saves — and taking the list literally would
// leave that OS with no save location at all.
func reachableOS(entry *node, overrides []rootOverride) []string {
	reachable := make(map[string]bool)
	for _, operatingSystem := range platformsOf(entry) {
		reachable[operatingSystem] = true
	}
	root := entry.value("root")
	for _, override := range overrides {
		if strings.EqualFold(override.root, root) && override.os != "" && override.compare == "=" {
			reachable[override.os] = true
		}
	}
	return constraintsForSet(reachable)
}

// platformsOf reports the operating systems a replicated folder names.
// "all", or no platform section at all, means every one of them.
func platformsOf(entry *node) []string {
	platforms := entry.section("platforms")
	if platforms == nil {
		return everyOS
	}
	wanted := make(map[string]bool, len(platforms.values))
	for _, value := range platforms.values {
		if strings.EqualFold(value, "all") {
			return everyOS
		}
		if operatingSystem := normalizeOS(value); operatingSystem != "" {
			wanted[operatingSystem] = true
		}
	}
	if len(wanted) == 0 {
		return everyOS
	}
	return constraintsForSet(wanted)
}

// constraintsForSet lists a set of operating systems in a stable order.
func constraintsForSet(operatingSystems map[string]bool) []string {
	listed := make([]string, 0, len(operatingSystems))
	for _, operatingSystem := range everyOS {
		if operatingSystems[operatingSystem] {
			listed = append(listed, operatingSystem)
		}
	}
	return listed
}

func normalizeOS(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "windows", "win32", "win64":
		return saveprofile.OSWindows
	case "macos", "osx", "macosx", "darwin":
		return saveprofile.OSMacOS
	case "linux":
		return saveprofile.OSLinux
	default:
		return ""
	}
}

// syncedThroughAPI reports whether Steam's own cloud bookkeeping shows this
// game writing every synced file through the API rather than from a
// replicated folder. Steam records the root each cached file came from, and a
// file written through the API is one that came from no root at all.
//
// Every account on the Device is considered: one account's stale cache must
// not answer for another's, so a single file recorded from a real folder is
// enough to say this game is not API-only. A game Steam has never synced here
// has no bookkeeping and is left to its declared configuration.
//
// Only "no root" versus "some root" is read. Steam's wider root numbering is
// not relied on, so this can confirm that folders are in use but not which
// declared folder a given file came from.
func (p *Provider) syncedThroughAPI(steamRoot, appID string) bool {
	return StoresThroughAPI(steamRoot, appID)
}

// StoresThroughAPI is the package's answer to whether a game's cloud saves
// move through the store's API on this Device (see syncedThroughAPI). It is
// exported because the same evidence decides whether a placement must
// reconcile the store's registry: a folder-replicated game is watched by
// Steam itself, while an API game trusts only what the API was told
// (FDR-005).
func StoresThroughAPI(steamRoot, appID string) bool {
	userdata := userdataFor(steamRoot)
	accounts, err := os.ReadDir(userdata)
	if err != nil {
		return false
	}
	synced := false
	for _, account := range accounts {
		if !account.IsDir() {
			continue
		}
		cached, err := os.ReadFile(path.Join(userdata, account.Name(), appID, "remotecache.vdf"))
		if err != nil {
			continue
		}
		roots := cachedRoots(string(cached))
		if len(roots) == 0 {
			continue
		}
		synced = true
		if !roots[apiFileRoot] || len(roots) > 1 {
			return false
		}
	}
	return synced
}

// apiFileRoot is the root Steam records for a file that belongs to no
// replicated folder — one the game wrote through the API.
const apiFileRoot = "0"

func cachedRoots(cache string) map[string]bool {
	roots := make(map[string]bool)
	for _, line := range strings.Split(cache, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 || strings.Trim(fields[0], `"`) != "root" {
			continue
		}
		roots[strings.Trim(fields[1], `"`)] = true
	}
	return roots
}

func userdataFor(steamRoot string) string {
	return path.Join(steamRoot, "userdata")
}

func cleanRelative(value string) string {
	// Some entries quote their values; a quotation mark is never part of the
	// path Steam means, and one left in would spell a folder no game has.
	cleaned := strings.Trim(strings.ReplaceAll(value, `\`, "/"), `"`)
	cleaned = strings.Trim(strings.ReplaceAll(cleaned, `"`, ""), "/")
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func locationID(template string) string {
	sum := sha256.Sum256([]byte(template))
	return hex.EncodeToString(sum[:4])
}
