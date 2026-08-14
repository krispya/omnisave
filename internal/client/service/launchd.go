package service

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// LaunchdLabel is the stable identity of the per-user launch agent.
const LaunchdLabel = "com.krispya.omnisave"

const launchdTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>watch</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>30</integer>
  <key>ProcessType</key>
  <string>Background</string>
  <key>LowPriorityIO</key>
  <true/>
</dict>
</plist>
`

// Launchd manages the client as a LaunchAgent in one macOS user's GUI
// domain. Its fields keep the native boundary deterministic in tests.
type Launchd struct {
	// PlistPath is the readable definition launchd loads at login.
	PlistPath string
	// Domain is the launchd domain for the signed-in user, such as gui/501.
	Domain string
	// Run executes one command and returns its combined output.
	Run func(ctx context.Context, name string, arguments ...string) ([]byte, error)
}

// NewLaunchd resolves the current user's LaunchAgents directory and launchd
// GUI domain.
func NewLaunchd() (*Launchd, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	current, err := user.Current()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(current.Uid) == "" {
		return nil, fmt.Errorf("the current user has no launchd identity")
	}
	return &Launchd{
		PlistPath: filepath.Join(home, "Library", "LaunchAgents", LaunchdLabel+".plist"),
		Domain:    "gui/" + current.Uid,
		Run:       runCommand,
	}, nil
}

func (l *Launchd) target() string { return l.Domain + "/" + LaunchdLabel }

// launchctl runs one command and preserves launchctl's own diagnostic, which
// usually names the domain or malformed definition more precisely than the
// caller could.
func (l *Launchd) launchctl(ctx context.Context, arguments ...string) ([]byte, error) {
	output, err := l.Run(ctx, "launchctl", arguments...)
	if err == nil {
		return output, nil
	}
	if detail := strings.TrimSpace(string(output)); detail != "" {
		return nil, fmt.Errorf("launchctl %s: %s", strings.Join(arguments, " "), detail)
	}
	return nil, fmt.Errorf("launchctl %s: %w", strings.Join(arguments, " "), err)
}

func (l *Launchd) registered(ctx context.Context) (string, bool) {
	output, err := l.Run(ctx, "launchctl", "print", l.target())
	return string(output), err == nil
}

func (l *Launchd) Install(ctx context.Context, config Config) (Status, error) {
	if strings.TrimSpace(config.Executable) == "" {
		return Status{}, fmt.Errorf("the service needs a client to run")
	}
	if err := os.MkdirAll(filepath.Dir(l.PlistPath), 0o755); err != nil {
		return Status{}, fmt.Errorf("cannot create %s: %w", filepath.Dir(l.PlistPath), err)
	}
	plist := fmt.Sprintf(launchdTemplate, LaunchdLabel, xmlText(config.Executable))
	if err := os.WriteFile(l.PlistPath, []byte(plist), 0o644); err != nil {
		return Status{}, fmt.Errorf("cannot write %s: %w", l.PlistPath, err)
	}

	// Bootstrap will not replace a loaded agent. Remove the old registration
	// first so reinstalling updates both the definition and the live process.
	if _, registered := l.registered(ctx); registered {
		if _, err := l.launchctl(ctx, "bootout", l.target()); err != nil {
			return Status{}, err
		}
	}
	if _, err := l.launchctl(ctx, "bootstrap", l.Domain, l.PlistPath); err != nil {
		return Status{}, err
	}
	if _, err := l.launchctl(ctx, "enable", l.target()); err != nil {
		return Status{}, err
	}
	if _, err := l.launchctl(ctx, "kickstart", "-k", l.target()); err != nil {
		return Status{}, err
	}
	return l.Status(ctx)
}

func (l *Launchd) Uninstall(ctx context.Context) error {
	if _, err := os.Stat(l.PlistPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, registered := l.registered(ctx); registered {
		if _, err := l.launchctl(ctx, "bootout", l.target()); err != nil {
			return err
		}
	}
	if err := os.Remove(l.PlistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot remove %s: %w", l.PlistPath, err)
	}
	return nil
}

func (l *Launchd) Status(ctx context.Context) (Status, error) {
	status := Status{Definition: l.PlistPath}
	if _, err := os.Stat(l.PlistPath); err != nil {
		if os.IsNotExist(err) {
			return status, nil
		}
		return status, err
	}
	status.Installed = true
	status.Enabled = true
	if disabled, _ := l.Run(ctx, "launchctl", "print-disabled", l.Domain); launchdDisabled(disabled) {
		status.Enabled = false
	}
	if status.Enabled {
		status.Start = StartAtLogin
	}
	if detail, registered := l.registered(ctx); registered {
		status.Running = strings.Contains(detail, "state = running")
	}
	return status, nil
}

func launchdDisabled(output []byte) bool {
	detail := string(output)
	return strings.Contains(detail, `"`+LaunchdLabel+`" => true`) ||
		strings.Contains(detail, LaunchdLabel+` => true`)
}

func (l *Launchd) Restart(ctx context.Context) (bool, error) {
	status, err := l.Status(ctx)
	if err != nil || !status.Running {
		return false, err
	}
	if _, err := l.launchctl(ctx, "kickstart", "-k", l.target()); err != nil {
		return false, err
	}
	return true, nil
}

var _ Manager = (*Launchd)(nil)
