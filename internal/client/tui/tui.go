// Package tui presents client scans in a terminal interface.
package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/krisbaumgartner/omnisave/internal/client"
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
	adapters []adapterState
	events   chan tea.Msg
	spinner  spinner.Model
	done     bool
	err      error
}

type progressMsg client.ScanProgress

type finishedMsg struct {
	err error
}

// Run scans configured adapters and renders their progress and results.
func Run(ctx context.Context, scanner *client.Scanner, verbose bool) error {
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
		verbose:  verbose,
		adapters: adapters,
		events:   make(chan tea.Msg, len(names)*2+1),
		spinner:  indicator,
	}

	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return err
	}
	return final.(model).err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.startScan(), m.spinner.Tick)
}

func (m model) startScan() tea.Cmd {
	return func() tea.Msg {
		go func() {
			_, err := m.scanner.ScanWithProgress(m.ctx, func(event client.ScanProgress) {
				m.events <- progressMsg(event)
			})
			m.events <- finishedMsg{err: err}
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
		m.err = message.err
		return m, tea.Quit
	}
	return m, nil
}

func (m model) View() string {
	var view strings.Builder
	view.WriteString(titleStyle.Render("OmniSave") + "\n")
	if m.done {
		view.WriteString(mutedStyle.Render("Scan complete") + "\n\n")
	} else {
		view.WriteString(mutedStyle.Render("Searching for local saves") + "\n\n")
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
	parts := []string{
		count(s.Targets, "target"),
		count(s.Games, "game"),
	}
	return strings.Join(append(parts, s.saveStats()...), " · ")
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

	var view strings.Builder
	for scanIndex, scan := range scans {
		location := scan.Target.Location
		if location == "" {
			location = scan.Target.Root
		}
		targetLocation := fmt.Sprintf("%s (%s)", filepath.Clean(location), scan.Target.Source)
		fmt.Fprintf(&view, "  %s %s\n", treeBranch(scanIndex, len(scans)), mutedStyle.Render(targetLocation))
		childIndent := "  " + treeChildIndent(scanIndex, len(scans))
		if len(scan.Games) == 0 {
			fmt.Fprintf(&view, "%s└─ %s\n", childIndent, mutedStyle.Render("No supported games found"))
			continue
		}
		for gameIndex, game := range scan.Games {
			title := game.Game.Identity.Title
			if title == "" {
				title = game.Game.Identity.Source + " " + game.Game.Identity.ID
			}
			stats := summarize([]client.TargetScan{{Games: []client.GameScan{game}}})
			fmt.Fprintf(&view, "%s%s %s  %s\n", childIndent, treeBranch(gameIndex, len(scan.Games)), nameStyle.Render(title), mutedStyle.Render(strings.Join(stats.saveStats(), " · ")))
			if len(game.Saves) == 0 {
				continue
			}
			saveIndent := childIndent + treeChildIndent(gameIndex, len(scan.Games))
			for saveIndex, save := range game.Saves {
				fileSummary := summary{}
				for _, file := range save.Files {
					fileSummary.Files++
					fileSummary.Bytes += file.Size
					if file.Modified.After(fileSummary.Latest) {
						fileSummary.Latest = file.Modified
					}
				}
				fmt.Fprintf(&view, "%s%s %s %s", saveIndent, treeBranch(saveIndex, len(game.Saves)), accentStyle.Render(save.Kind), mutedStyle.Render("· "+count(fileSummary.Files, "file")+" · "+formatBytes(fileSummary.Bytes)))
				if !fileSummary.Latest.IsZero() {
					fmt.Fprintf(&view, " %s", mutedStyle.Render("· latest "+fileSummary.Latest.Local().Format("Jan 2 15:04")))
				}
				view.WriteByte('\n')
				fileIndent := saveIndent + treeChildIndent(saveIndex, len(game.Saves))
				for fileIndex, file := range save.Files {
					fileDetails := fmt.Sprintf("%s (%s)", filepath.Clean(file.Path), formatBytes(file.Size))
					fmt.Fprintf(&view, "%s%s %s\n", fileIndent, treeBranch(fileIndex, len(save.Files)), mutedStyle.Render(fileDetails))
				}
			}
		}
	}
	return view.String()
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
		title := game.Game.Identity.Title
		if title == "" {
			title = game.Game.Identity.Source + " " + game.Game.Identity.ID
		}
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

func treeChildIndent(index, total int) string {
	if index == total-1 {
		return "   "
	}
	return "│  "
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
