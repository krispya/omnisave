package client

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Styles ---

var (
	styleActive   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)  // green
	styleInactive = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))              // gray
	styleConflict = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)  // yellow
	styleError    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))              // red
	styleBold     = lipgloss.NewStyle().Bold(true)
	styleStatus   = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))             // blue
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// --- View modes ---

type viewMode int

const (
	viewGames   viewMode = iota
	viewHistory viewMode = iota
)

// --- List item types ---

type systemItem struct{ name string }

func (s systemItem) FilterValue() string { return s.name }
func (s systemItem) Title() string       { return s.name }
func (s systemItem) Description() string { return "" }

type gameItem struct {
	system  string
	game    string
	enabled bool
	lastSync time.Time
	hasSync  bool
	conflict bool
}

func (g gameItem) FilterValue() string { return g.game }
func (g gameItem) Title() string {
	toggle := styleInactive.Render("[OFF]")
	if g.enabled {
		toggle = styleActive.Render("[ON] ")
	}
	conflict := ""
	if g.conflict {
		conflict = " " + styleConflict.Render("⚠ conflict")
	}
	return fmt.Sprintf("%s %s%s", toggle, g.game, conflict)
}
func (g gameItem) Description() string {
	if g.hasSync {
		return styleDim.Render("last sync: " + g.lastSync.Format("2006-01-02 15:04:05"))
	}
	return styleDim.Render("never synced")
}

type versionItem struct {
	sha256    string
	timestamp time.Time
	isActive  bool
	clientID  string
}

func (v versionItem) FilterValue() string { return v.sha256 }
func (v versionItem) Title() string {
	prefix := v.sha256
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	active := ""
	if v.isActive {
		active = styleActive.Render(" ← active")
	}
	return fmt.Sprintf("%s  %s%s", prefix, v.timestamp.Format("2006-01-02 15:04:05"), active)
}
func (v versionItem) Description() string {
	return styleDim.Render("client: " + v.clientID)
}

// --- Messages ---

type syncEventMsg SyncEvent
type versionsLoadedMsg struct {
	system   string
	game     string
	versions []versionItem
}
type errMsg struct{ err error }
type restoreMsg struct {
	system string
	game   string
	sha256 string
}

// --- Model ---

// Model is the Bubble Tea model for the client TUI.
type Model struct {
	cfg    *Config
	state  *State
	syncer *Syncer
	events <-chan SyncEvent

	mode   viewMode
	width  int
	height int

	// Games view
	systems  []string
	sysIdx   int
	gameList list.Model

	// History view
	histSystem  string
	histGame    string
	versionList list.Model

	spinner     spinner.Model
	syncing     bool
	statusMsg   string
	statusErr   bool
	lastSyncs   map[string]time.Time
	conflicts   map[string]bool
}

// NewModel constructs the TUI model.
func NewModel(cfg *Config, state *State, syncer *Syncer, events <-chan SyncEvent) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	gameDelegate := list.NewDefaultDelegate()
	gameList := list.New(nil, gameDelegate, 0, 0)
	gameList.Title = "Games"
	gameList.SetShowHelp(false)

	versionDelegate := list.NewDefaultDelegate()
	versionList := list.New(nil, versionDelegate, 0, 0)
	versionList.Title = "Version History"
	versionList.SetShowHelp(false)

	return Model{
		cfg:         cfg,
		state:       state,
		syncer:      syncer,
		events:      events,
		spinner:     sp,
		gameList:    gameList,
		versionList: versionList,
		lastSyncs:   make(map[string]time.Time),
		conflicts:   make(map[string]bool),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.waitForEvent())
}

func (m Model) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		ev := <-m.events
		return syncEventMsg(ev)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.gameList.SetSize(msg.Width, msg.Height-4)
		m.versionList.SetSize(msg.Width, msg.Height-4)

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.mode == viewHistory {
				m.mode = viewGames
			}
		case " ", "enter":
			if m.mode == viewGames {
				m.toggleSelected()
			}
		case "s":
			if m.mode == viewGames {
				cmds = append(cmds, m.forceSyncSelected())
			}
		case "h":
			if m.mode == viewGames {
				cmd := m.openHistory()
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		case "r":
			if m.mode == viewHistory {
				cmds = append(cmds, m.restoreSelected())
			}
		}

	case syncEventMsg:
		ev := SyncEvent(msg)
		key := ev.System + "/" + ev.Game
		if ev.Err != nil {
			m.statusMsg = fmt.Sprintf("sync error [%s/%s]: %v", ev.System, ev.Game, ev.Err)
			m.statusErr = true
		} else if ev.Pushed {
			m.lastSyncs[key] = time.Now()
			m.conflicts[key] = ev.Conflict
			m.statusMsg = fmt.Sprintf("synced %s/%s", ev.System, ev.Game)
			if ev.Conflict {
				m.statusMsg += " (conflict resolved)"
			}
			m.statusErr = false
			m.refreshGameList()
		}
		cmds = append(cmds, m.waitForEvent())

	case versionsLoadedMsg:
		items := make([]list.Item, len(msg.versions))
		for i, v := range msg.versions {
			items[i] = v
		}
		m.versionList.SetItems(items)
		m.versionList.Title = fmt.Sprintf("History: %s / %s", msg.system, msg.game)
		m.histSystem = msg.system
		m.histGame = msg.game
		m.mode = viewHistory

	case errMsg:
		m.statusMsg = msg.err.Error()
		m.statusErr = true

	case restoreMsg:
		m.statusMsg = fmt.Sprintf("restored %s/%s @ %s", msg.system, msg.game, msg.sha256[:12])
		m.statusErr = false

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	if m.mode == viewGames {
		var cmd tea.Cmd
		m.gameList, cmd = m.gameList.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		var cmd tea.Cmd
		m.versionList, cmd = m.versionList.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	var sb strings.Builder

	header := styleBold.Render("omnisave")
	if m.mode == viewHistory {
		header += "  " + styleDim.Render("[esc] back  [r] restore  [q] quit")
	} else {
		header += "  " + styleDim.Render("[space] toggle  [s] sync  [h] history  [q] quit")
	}
	sb.WriteString(header + "\n")

	if m.mode == viewGames {
		sb.WriteString(m.gameList.View())
	} else {
		sb.WriteString(m.versionList.View())
	}

	sb.WriteString("\n")
	statusStyle := styleStatus
	if m.statusErr {
		statusStyle = styleError
	}
	if m.statusMsg != "" {
		sb.WriteString(statusStyle.Render(m.statusMsg))
	}

	return sb.String()
}

// --- Helpers ---

func (m *Model) toggleSelected() {
	item, ok := m.gameList.SelectedItem().(gameItem)
	if !ok {
		return
	}
	enabled := !item.enabled
	m.state.SetEnabled(item.system, item.game, enabled)
	m.state.Save()
	m.refreshGameList()
}

func (m *Model) forceSyncSelected() tea.Cmd {
	item, ok := m.gameList.SelectedItem().(gameItem)
	if !ok {
		return nil
	}
	system, game := item.system, item.game
	savesDir := m.cfg.RetroarchSavesDir
	return func() tea.Msg {
		path := filepath.Join(savesDir, system, game+".srm")
		pushed, conflict, err := m.syncer.SyncGame(system, game, path)
		if err == nil && pushed {
			m.state.Save()
		}
		return syncEventMsg(SyncEvent{
			System:   system,
			Game:     game,
			FilePath: path,
			Pushed:   pushed,
			Conflict: conflict,
			Err:      err,
		})
	}
}

func (m *Model) openHistory() tea.Cmd {
	item, ok := m.gameList.SelectedItem().(gameItem)
	if !ok {
		return nil
	}
	system, game := item.system, item.game
	syncer := m.syncer
	return func() tea.Msg {
		infos, err := syncer.ListVersions(system, game)
		if err != nil {
			return errMsg{err}
		}
		versions := make([]versionItem, len(infos))
		for i, v := range infos {
			versions[i] = versionItem{
				sha256:    v.SHA256,
				timestamp: v.Timestamp,
				isActive:  v.IsActive,
				clientID:  v.ClientID,
			}
		}
		return versionsLoadedMsg{system: system, game: game, versions: versions}
	}
}

func (m *Model) restoreSelected() tea.Cmd {
	item, ok := m.versionList.SelectedItem().(versionItem)
	if !ok {
		return nil
	}
	system, game := m.histSystem, m.histGame
	sha256 := item.sha256
	syncer := m.syncer
	state := m.state
	cfg := m.cfg
	return func() tea.Msg {
		destPath := filepath.Join(cfg.RetroarchSavesDir, system, game+".srm")
		if err := syncer.RestoreVersion(system, game, sha256, destPath); err != nil {
			return errMsg{err}
		}
		state.UpdateHash(system, game, sha256)
		state.Save()
		return restoreMsg{system: system, game: game, sha256: sha256}
	}
}

func (m *Model) refreshGameList() {
	items := make([]list.Item, 0)
	for _, sys := range m.systems {
		// Get games from state
		for key, gs := range m.state.Games {
			parts := strings.SplitN(key, "/", 2)
			if len(parts) != 2 || parts[0] != sys {
				continue
			}
			g := gameItem{
				system:  sys,
				game:    parts[1],
				enabled: gs.SyncEnabled,
			}
			if t, ok := m.lastSyncs[key]; ok {
				g.lastSync = t
				g.hasSync = true
			}
			if c, ok := m.conflicts[key]; ok {
				g.conflict = c
			}
			items = append(items, g)
		}
	}
	m.gameList.SetItems(items)
}

// SetSystems sets the known systems for the games list.
func (m *Model) SetSystems(systems []string) {
	m.systems = systems
	m.refreshGameList()
}
