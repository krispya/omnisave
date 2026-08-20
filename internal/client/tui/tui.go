// Package tui presents client scans in a terminal interface.
package tui

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/host"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

type adapterStage int

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{
		Light: "#111111",
		Dark:  "#FAFAFA",
	})
	nameStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{
		Light: "#111111",
		Dark:  "#EDEDED",
	})
	mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#666666",
		Dark:  "#888888",
	})
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#0070F3",
		Dark:  "#3291FF",
	})
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#238636",
		Dark:  "#3FB950",
	})
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#CF222E",
		Dark:  "#F85149",
	})
)

const (
	adapterWaiting adapterStage = iota
	adapterSearching
	adapterComplete
	adapterFailed
)

type adapterState struct {
	name    string
	stage   adapterStage
	results []client.TargetScan
	err     error
}

type model struct {
	ctx      context.Context
	scanner  *client.Scanner
	verbose  bool
	clear    bool
	version  string
	adapters []adapterState
	events   chan tea.Msg
	spinner  spinner.Model
	results  []client.TargetScan
	done     bool
	aborted  bool
	err      error
}

// provenance is what a reader of a pasted report needs to know about the
// device that produced it.
func (m model) provenance() []string {
	facts := []string{host.Platform() + "/" + runtime.GOARCH}
	if m.version != "" {
		return append([]string{m.version}, facts...)
	}
	return facts
}

type progressMsg client.ScanProgress

type finishedMsg struct {
	results []client.TargetScan
	err     error
}

var (
	// ErrAborted indicates that the user left an interactive client view.
	ErrAborted = errors.New("client interaction aborted")
	// ErrNoGames indicates that a scan produced nothing that can be tracked.
	ErrNoGames = errors.New("no games discovered")
	// ErrNoSaves indicates that tracked games have no discovered saves to bind.
	ErrNoSaves = errors.New("no local saves discovered")
	// ErrNoOmnisaves indicates that the server has no binding destinations.
	ErrNoOmnisaves = errors.New("no Omnisaves available")
)

// Run scans configured adapters and renders their progress and results.
func Run(ctx context.Context, scanner *client.Scanner, verbose bool) error {
	_, err := Scan(ctx, scanner, verbose, "")
	return err
}

// Scan renders discovery progress and returns the completed results. A
// verbose scan explains where discovery looked, and names the build that
// looked there so its report stands on its own in an issue.
func Scan(ctx context.Context, scanner *client.Scanner, verbose bool, version string) ([]client.TargetScan, error) {
	return runScan(ctx, scanner, scanOptions{verbose: verbose, version: version})
}

// ScanForSelection renders scan progress that clears itself once discovery
// finishes, so a prompt that follows is the only thing left on screen.
func ScanForSelection(ctx context.Context, scanner *client.Scanner) ([]client.TargetScan, error) {
	return runScan(ctx, scanner, scanOptions{clear: true})
}

type scanOptions struct {
	verbose bool
	clear   bool
	version string
}

func runScan(ctx context.Context, scanner *client.Scanner, options scanOptions) ([]client.TargetScan, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	names := scanner.AdapterNames()
	adapters := make([]adapterState, 0, len(names))
	for _, name := range names {
		adapters = append(adapters, adapterState{name: name})
	}
	indicator := spinner.New()
	indicator.Spinner = spinner.Dot
	indicator.Style = accentStyle
	m := model{
		ctx:      ctx,
		scanner:  scanner,
		verbose:  options.verbose,
		clear:    options.clear,
		version:  options.version,
		adapters: adapters,
		events:   make(chan tea.Msg, len(names)*2+1),
		spinner:  indicator,
	}

	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return nil, err
	}
	completed := final.(model)
	if completed.aborted {
		return nil, ErrAborted
	}
	return completed.results, completed.err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.startScan(), m.spinner.Tick)
}

func (m model) startScan() tea.Cmd {
	return func() tea.Msg {
		go func() {
			results, err := m.scanner.ScanWithProgress(m.ctx, func(event client.ScanProgress) {
				m.events <- progressMsg(event)
			})
			m.events <- finishedMsg{results: results, err: err}
		}()
		return <-m.events
	}
}

func (m model) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		return <-m.events
	}
}

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyMsg:
		switch message.String() {
		case "q", "ctrl+c", "esc":
			m.aborted = true
			return m, tea.Quit
		}
	case spinner.TickMsg:
		if !m.done {
			var command tea.Cmd
			m.spinner, command = m.spinner.Update(message)
			return m, command
		}
	case progressMsg:
		progress := client.ScanProgress(message)
		for index := range m.adapters {
			if m.adapters[index].name != progress.Adapter {
				continue
			}
			switch progress.Stage {
			case client.ScanStarted:
				m.adapters[index].stage = adapterSearching
			case client.ScanCompleted:
				m.adapters[index].stage = adapterComplete
				m.adapters[index].results = progress.Results
			case client.ScanFailed:
				m.adapters[index].stage = adapterFailed
				m.adapters[index].err = progress.Err
			}
			break
		}
		return m, m.waitForEvent()
	case finishedMsg:
		m.done = true
		m.results = message.results
		m.err = message.err
		return m, tea.Quit
	}
	return m, nil
}

type gameChoice struct {
	id       string
	label    string
	selected bool
}

type gameChoiceGroup struct {
	label   string
	choices []gameChoice
}

// SelectTrackedGames prompts for the discovered games this machine should sync.
func SelectTrackedGames(scans []client.TargetScan, tracked map[string]bool) ([]string, error) {
	groups := trackingChoiceGroups(scans, tracked)
	if len(groups) == 0 {
		return nil, ErrNoGames
	}

	selections := make([][]string, len(groups))
	fields := make([]huh.Field, 0, len(groups))
	for groupIndex, group := range groups {
		for _, choice := range group.choices {
			if choice.selected {
				selections[groupIndex] = append(selections[groupIndex], choice.id)
			}
		}
		fields = append(fields, trackingMultiSelect(group, &selections[groupIndex]))
	}
	form := huh.NewForm(
		huh.NewGroup(fields...).
			Title("Choose games to sync"),
	).WithTheme(trackingTheme())
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, ErrAborted
		}
		return nil, err
	}
	var selected []string
	for _, selection := range selections {
		selected = append(selected, selection...)
	}
	return selected, nil
}

func trackingMultiSelect(group gameChoiceGroup, selection *[]string) *huh.MultiSelect[string] {
	options := make([]huh.Option[string], 0, len(group.choices))
	for _, choice := range group.choices {
		options = append(options, huh.NewOption(choice.label, choice.id))
	}
	height := len(options) + 2
	if height > 10 {
		height = 10
	}
	return huh.NewMultiSelect[string]().
		Title(group.label).
		Options(options...).
		Height(height).
		Value(selection)
}

func trackingChoiceGroups(scans []client.TargetScan, tracked map[string]bool) []gameChoiceGroup {
	var groups []gameChoiceGroup
	for _, scan := range scans {
		if len(scan.Games) == 0 {
			continue
		}
		group := gameChoiceGroup{label: displayName(scan.Target.Adapter)}
		for _, game := range scan.Games {
			title := game.Game.Identity.DisplayTitle(game.Game.ID)
			stats := summarize([]client.TargetScan{{Games: []client.GameScan{game}}})
			metadata := "  ·  " + strings.Join(stats.saveStats(), " · ")
			group.choices = append(group.choices, gameChoice{
				id:       game.Game.ID,
				label:    title + mutedStyle.Render(metadata),
				selected: tracked[game.Game.ID],
			})
		}
		groups = append(groups, group)
	}
	return groups
}

// BindingSelection maps one discovered native save to one server Omnisave.
type BindingSelection struct {
	Local    tracking.LocalSave
	Omnisave omnisave.Omnisave
}

type bindingChoice struct {
	label string
	index int
}

// SelectBinding prompts for one exact local-to-server save mapping.
func SelectBinding(local []tracking.LocalSave, remote []omnisave.Omnisave, bindings []tracking.Binding) (BindingSelection, error) {
	if len(local) == 0 {
		return BindingSelection{}, ErrNoSaves
	}
	if len(remote) == 0 {
		return BindingSelection{}, ErrNoOmnisaves
	}
	localChoices, remoteChoices := bindingChoices(local, remote, bindings)
	localIndex, remoteIndex := 0, 0
	form := huh.NewForm(huh.NewGroup(
		bindingSelect("Local save", localChoices, &localIndex),
		bindingSelect("Omnisave", remoteChoices, &remoteIndex),
	).Title("Choose what this machine syncs"))
	form.WithTheme(trackingTheme())
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return BindingSelection{}, ErrAborted
		}
		return BindingSelection{}, err
	}
	return BindingSelection{Local: local[localIndex], Omnisave: remote[remoteIndex]}, nil
}

func bindingSelect(title string, choices []bindingChoice, value *int) *huh.Select[int] {
	options := make([]huh.Option[int], 0, len(choices))
	for _, choice := range choices {
		options = append(options, huh.NewOption(choice.label, choice.index))
	}
	height := len(options) + 2
	if height > 10 {
		height = 10
	}
	return huh.NewSelect[int]().
		Title(title).
		Options(options...).
		Height(height).
		Filtering(len(options) > 6).
		Value(value)
}

func bindingChoices(local []tracking.LocalSave, remote []omnisave.Omnisave, bindings []tracking.Binding) ([]bindingChoice, []bindingChoice) {
	remoteNames := make(map[string]string, len(remote))
	for _, save := range remote {
		remoteNames[save.ID] = save.DisplayName
	}
	localChoices := make([]bindingChoice, 0, len(local))
	for index, save := range local {
		parts := []string{displayName(save.Adapter), save.GameTitle, save.Kind, count(save.FileCount, "file"), formatBytes(save.Size)}
		for _, binding := range bindings {
			if binding.Adapter == save.Adapter && binding.TargetID == save.TargetID && binding.LocalSaveID == save.ID {
				name := remoteNames[binding.OmnisaveID]
				if name == "" {
					name = shortID(binding.OmnisaveID)
				}
				parts = append(parts, "currently "+name)
				break
			}
		}
		localChoices = append(localChoices, bindingChoice{label: strings.Join(parts, " · "), index: index})
	}
	remoteChoices := make([]bindingChoice, 0, len(remote))
	for index, save := range remote {
		parts := []string{save.DisplayName, "game " + shortID(save.GameID)}
		if save.CurrentRevisionID == nil {
			parts = append(parts, "no revisions")
		} else {
			parts = append(parts, "current "+shortID(*save.CurrentRevisionID))
		}
		remoteChoices = append(remoteChoices, bindingChoice{label: strings.Join(parts, " · "), index: index})
	}
	return localChoices, remoteChoices
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func trackingTheme() *huh.Theme {
	theme := huh.ThemeBase()
	theme.Focused.Base = theme.Focused.Base.BorderForeground(mutedStyle.GetForeground())
	theme.Focused.Title = titleStyle
	theme.Focused.Description = mutedStyle
	theme.Focused.MultiSelectSelector = accentStyle.SetString("› ")
	theme.Focused.SelectSelector = accentStyle.SetString("› ")
	theme.Focused.SelectedPrefix = successStyle.SetString("◉ ")
	theme.Focused.UnselectedPrefix = mutedStyle.SetString("○ ")
	theme.Focused.SelectedOption = nameStyle.UnsetBold()
	theme.Focused.UnselectedOption = mutedStyle
	theme.Blurred = theme.Focused
	return theme
}

func (m model) View() string {
	if m.done && m.clear {
		return ""
	}
	var view strings.Builder
	view.WriteString(titleStyle.Render("Omnisave") + "\n")
	switch {
	case !m.done:
		view.WriteString(mutedStyle.Render("Searching for local saves") + "\n\n")
	case m.verbose:
		// A verbose report is written to be pasted into an issue, so it
		// names the build and the platform that produced it.
		view.WriteString(mutedStyle.Render(strings.Join(
			append([]string{"Save locations"}, m.provenance()...), " · ")) + "\n\n")
	default:
		view.WriteString(mutedStyle.Render("Scan complete") + "\n\n")
	}
	for _, adapter := range m.adapters {
		view.WriteString(renderAdapter(adapter, m.verbose, m.spinner.View()))
	}
	if !m.done {
		view.WriteString("\n" + mutedStyle.Render("Press q to quit.") + "\n")
	}
	return view.String()
}

func renderAdapter(adapter adapterState, verbose bool, spinner string) string {
	var view strings.Builder
	name := displayName(adapter.name)
	switch adapter.stage {
	case adapterWaiting:
		fmt.Fprintf(&view, "%s %s  %s\n", mutedStyle.Render("○"), nameStyle.Render(name), mutedStyle.Render("waiting"))
	case adapterSearching:
		fmt.Fprintf(&view, "%s %s  %s\n", spinner, nameStyle.Render(name), accentStyle.Render("searching"))
	case adapterFailed:
		fmt.Fprintf(&view, "%s %s  %s\n", errorStyle.Render("!"), nameStyle.Render(name), errorStyle.Render(adapter.err.Error()))
	case adapterComplete:
		summary := summarize(adapter.results)
		if summary.Targets == 0 {
			fmt.Fprintf(&view, "%s %s  %s\n", mutedStyle.Render("–"), nameStyle.Render(name), mutedStyle.Render("not installed"))
			return view.String()
		}
		fmt.Fprintf(&view, "%s %s  %s\n", successStyle.Render("✓"), nameStyle.Render(name), mutedStyle.Render(summary.String()))
		view.WriteString(renderDetails(adapter.results, verbose))
	}
	return view.String()
}

type summary struct {
	Targets int
	Games   int
	Saves   int
	Files   int
	Bytes   int64
	Latest  time.Time
}

func summarize(scans []client.TargetScan) summary {
	result := summary{Targets: len(scans)}
	for _, scan := range scans {
		result.Games += len(scan.Games)
		for _, game := range scan.Games {
			result.Saves += len(game.Saves)
			for _, save := range game.Saves {
				result.Files += len(save.Files)
				for _, file := range save.Files {
					result.Bytes += file.Size
					if file.Modified.After(result.Latest) {
						result.Latest = file.Modified
					}
				}
			}
		}
	}
	return result
}

func (s summary) String() string {
	return strings.Join([]string{
		count(s.Targets, "target"),
		count(s.Games, "game"),
		count(s.Saves, "save"),
	}, " · ")
}

func (s summary) saveStats() []string {
	parts := []string{count(s.Saves, "save")}
	if s.Files > 0 {
		parts = append(parts, count(s.Files, "file"), formatBytes(s.Bytes))
	}
	if !s.Latest.IsZero() {
		parts = append(parts, "latest "+s.Latest.Local().Format("Jan 2 15:04"))
	}
	return parts
}

func renderDetails(scans []client.TargetScan, verbose bool) string {
	if !verbose {
		return renderGames(scans)
	}
	return renderVerbose(scans)
}

func renderGames(scans []client.TargetScan) string {
	var games []client.GameScan
	for _, scan := range scans {
		games = append(games, scan.Games...)
	}
	if len(games) == 0 {
		return "  └─ " + mutedStyle.Render("No supported games found") + "\n"
	}

	var view strings.Builder
	for index, game := range games {
		title := game.Game.Identity.DisplayTitle(game.Game.ID)
		stats := summarize([]client.TargetScan{{Games: []client.GameScan{game}}})
		fmt.Fprintf(&view, "  %s %s  %s\n", treeBranch(index, len(games)), nameStyle.Render(title), mutedStyle.Render(strings.Join(stats.saveStats(), " · ")))
	}
	return view.String()
}

func treeBranch(index, total int) string {
	if index == total-1 {
		return "└─"
	}
	return "├─"
}

func displayName(name string) string {
	switch name {
	case "retroarch":
		return "RetroArch"
	case "steam":
		return "Steam"
	default:
		return name
	}
}

func count(value int, singular string) string {
	suffix := "s"
	if value == 1 {
		suffix = ""
	}
	return fmt.Sprintf("%d %s%s", value, singular, suffix)
}

func formatBytes(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	value := float64(bytes)
	for _, unit := range units {
		value /= 1024
		if value < 1024 || unit == units[len(units)-1] {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%d B", bytes)
}
