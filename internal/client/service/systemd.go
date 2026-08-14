package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// UnitName is what the service is called to systemd, and the name every
// systemctl line in here and in the documentation refers to.
const UnitName = "omnisave.service"

// unitTemplate is the service definition. It is written rather than shipped
// as a file because the one thing that varies — which client binary runs — is
// resolved on the device, and a template with one hole is easier to keep
// truthful than a file plus a rewriting step.
//
// The directives that are not obvious:
//
//   - Restart=always, not on-failure: watch exits 0 when it is asked to stop,
//     so on-failure would not bring it back from a clean kill.
//   - StartLimitIntervalSec=0: an unconnected client fails immediately, and
//     systemd's default rate limit would park the service in a failed state
//     after five quick failures. Failing every RestartSec forever is quiet and
//     recoverable; failing permanently is neither. Install refuses to run
//     before the device is connected, so this covers a credential that stops
//     working later rather than one that was never there.
//   - Nice and IOSchedulingClass: the client shares a machine with the game it
//     is protecting, and on a handheld that machine has no headroom to spare.
//     A scan that finishes a second later costs nothing; a frame does.
//   - No After=network-online.target: a user manager cannot order its units
//     against system units, so network readiness is the client's problem and
//     is handled in the loop rather than in the unit.
const unitTemplate = `[Unit]
Description=Omnisave save synchronization
Documentation=https://github.com/krispya/omnisave
StartLimitIntervalSec=0

[Service]
Type=simple
ExecStart=%s watch
Restart=always
RestartSec=30
Nice=10
IOSchedulingClass=idle

[Install]
WantedBy=default.target
`

// Systemd manages the client as a systemd user service. The definition and
// every command it runs are fields so the whole manager is exercisable
// without a systemd to run against.
type Systemd struct {
	// UnitPath is where the definition is written.
	UnitPath string
	// User is whose linger state is read and set.
	User string
	// Run executes one command and returns its combined output.
	Run func(ctx context.Context, name string, arguments ...string) ([]byte, error)
}

// NewSystemd resolves a manager for this user's systemd instance.
func NewSystemd() (*Systemd, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	current, err := user.Current()
	if err != nil {
		return nil, err
	}
	return &Systemd{
		UnitPath: filepath.Join(config, "systemd", "user", UnitName),
		User:     current.Username,
		Run:      runCommand,
	}, nil
}

func runCommand(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, arguments...).CombinedOutput()
}

// systemctl runs one user-instance systemctl command, reporting the tool's own
// words on failure: "Failed to connect to bus" is the whole diagnosis on a
// machine with no user manager, and rephrasing it would lose that.
func (s *Systemd) systemctl(ctx context.Context, arguments ...string) error {
	output, err := s.Run(ctx, "systemctl", append([]string{"--user"}, arguments...)...)
	if err != nil {
		if detail := strings.TrimSpace(string(output)); detail != "" {
			return fmt.Errorf("systemctl --user %s: %s", strings.Join(arguments, " "), detail)
		}
		return fmt.Errorf("systemctl --user %s: %w", strings.Join(arguments, " "), err)
	}
	return nil
}

// query asks systemctl a question whose answer is its output. These commands
// exit non-zero to mean "no" — is-active on a stopped unit is a failure by
// exit code and an answer by output — so the error is the answer and only the
// output is read.
func (s *Systemd) query(ctx context.Context, arguments ...string) string {
	output, _ := s.Run(ctx, "systemctl", append([]string{"--user"}, arguments...)...)
	return strings.TrimSpace(string(output))
}

func (s *Systemd) Install(ctx context.Context, config Config) (Status, error) {
	if strings.TrimSpace(config.Executable) == "" {
		return Status{}, fmt.Errorf("the service needs a client to run")
	}
	if err := os.MkdirAll(filepath.Dir(s.UnitPath), 0o755); err != nil {
		return Status{}, fmt.Errorf("cannot create %s: %w", filepath.Dir(s.UnitPath), err)
	}
	unit := fmt.Sprintf(unitTemplate, quoteArgument(config.Executable))
	if err := os.WriteFile(s.UnitPath, []byte(unit), 0o644); err != nil {
		return Status{}, fmt.Errorf("cannot write %s: %w", s.UnitPath, err)
	}
	if err := s.systemctl(ctx, "daemon-reload"); err != nil {
		return Status{}, err
	}
	if err := s.systemctl(ctx, "enable", UnitName); err != nil {
		return Status{}, err
	}
	// Restart rather than start: an install over an existing service is how
	// the definition changes, and a start would leave the old one running
	// under the new definition.
	if err := s.systemctl(ctx, "restart", UnitName); err != nil {
		return Status{}, err
	}
	// Linger is what makes the service survive having no session at all —
	// the gap while a Steam Deck switches out of Desktop Mode, and every
	// moment between boot and a login. It is an improvement rather than a
	// requirement: without it the service still starts with the session, so
	// a refusal here is reported by the status it returns, not raised.
	s.enableLinger(ctx)
	return s.Status(ctx)
}

func (s *Systemd) enableLinger(ctx context.Context) {
	if s.User == "" {
		return
	}
	_, _ = s.Run(ctx, "loginctl", "enable-linger", s.User)
}

func (s *Systemd) Uninstall(ctx context.Context) error {
	// Disable before the definition is removed: systemctl needs the unit to
	// find the symlinks that start it, and removing the file first would
	// leave them behind pointing at nothing.
	if _, err := os.Stat(s.UnitPath); err == nil {
		if err := s.systemctl(ctx, "disable", "--now", UnitName); err != nil {
			return err
		}
	}
	if err := os.Remove(s.UnitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot remove %s: %w", s.UnitPath, err)
	}
	if err := s.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	// Linger is deliberately left enabled. This device turned it on, but it
	// is a property of the user rather than of this service, and anything
	// else the player runs in the background depends on it too.
	return nil
}

func (s *Systemd) Status(ctx context.Context) (Status, error) {
	status := Status{Definition: s.UnitPath}
	if _, err := os.Stat(s.UnitPath); err != nil {
		if os.IsNotExist(err) {
			return status, nil
		}
		return status, err
	}
	status.Installed = true
	status.Enabled = s.query(ctx, "is-enabled", UnitName) == "enabled"
	status.Running = s.query(ctx, "is-active", UnitName) == "active"
	status.Lingering = s.lingering(ctx)
	return status, nil
}

// lingering reads the user's linger state. The user is named explicitly
// because omitting it only came to mean "whoever is asking" in later systemd
// versions, and the answer matters most on the oldest devices.
func (s *Systemd) lingering(ctx context.Context) bool {
	if s.User == "" {
		return false
	}
	output, _ := s.Run(ctx, "loginctl", "show-user", s.User, "--property=Linger", "--value")
	return strings.TrimSpace(string(output)) == "yes"
}

func (s *Systemd) Restart(ctx context.Context) (bool, error) {
	status, err := s.Status(ctx)
	if err != nil || !status.Running {
		return false, err
	}
	if err := s.systemctl(ctx, "restart", UnitName); err != nil {
		return false, err
	}
	return true, nil
}

// quoteArgument renders one path as systemd reads it. A bare path is left
// bare — that is what every unit on the device looks like, and a reader
// checking what the service runs should not have to see past quoting to do
// it — and anything systemd's parser would split is quoted.
func quoteArgument(path string) string {
	if !strings.ContainsAny(path, " \t\"'\\") {
		return path
	}
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(path) + `"`
}

var _ Manager = (*Systemd)(nil)
