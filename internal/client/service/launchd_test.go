package service_test

import (
	"context"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/service"
)

type launchdRecorder struct {
	commands   []string
	registered bool
	running    bool
	disabled   bool
}

func (r *launchdRecorder) run(_ context.Context, name string, arguments ...string) ([]byte, error) {
	command := strings.TrimSpace(name + " " + strings.Join(arguments, " "))
	r.commands = append(r.commands, command)
	if len(arguments) == 0 {
		return nil, nil
	}
	switch arguments[0] {
	case "print":
		if !r.registered {
			return []byte("Could not find service"), errors.New("exit status 113")
		}
		state := "waiting"
		if r.running {
			state = "running"
		}
		return []byte("state = " + state), nil
	case "print-disabled":
		if r.disabled {
			return []byte(`"` + service.LaunchdLabel + `" => true`), nil
		}
		return []byte("disabled services = {}"), nil
	case "bootout":
		r.registered = false
		r.running = false
	case "bootstrap":
		r.registered = true
		r.running = true
	case "enable":
		r.disabled = false
	case "kickstart":
		r.registered = true
		r.running = true
	}
	return nil, nil
}

func (r *launchdRecorder) index(prefix string) int {
	for index, command := range r.commands {
		if strings.HasPrefix(command, prefix) {
			return index
		}
	}
	return -1
}

func launchdManager(t *testing.T) (*service.Launchd, *launchdRecorder) {
	t.Helper()
	commands := &launchdRecorder{}
	return &service.Launchd{
		PlistPath: filepath.Join(t.TempDir(), "Library", "LaunchAgents", service.LaunchdLabel+".plist"),
		Domain:    "gui/501",
		Run:       commands.run,
	}, commands
}

func TestLaunchdInstallDefinesAndStartsTheUserAgent(t *testing.T) {
	launchd, commands := launchdManager(t)
	executable := "/Users/Player/Omnisave & Games/omnisave"

	status, err := launchd.Install(context.Background(), service.Config{Executable: executable})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	plist, err := os.ReadFile(launchd.PlistPath)
	if err != nil {
		t.Fatalf("read launch agent: %v", err)
	}
	definition := string(plist)
	var document struct{ XMLName xml.Name }
	if err := xml.Unmarshal(plist, &document); err != nil || document.XMLName.Local != "plist" {
		t.Fatalf("launch agent is not a plist document: %v", err)
	}
	for _, wanted := range []string{
		"<string>/Users/Player/Omnisave &amp; Games/omnisave</string>",
		"<string>watch</string>",
		"<key>KeepAlive</key>",
		"<key>ThrottleInterval</key>",
	} {
		if !strings.Contains(definition, wanted) {
			t.Errorf("launch agent is missing %q:\n%s", wanted, definition)
		}
	}
	for _, command := range []string{
		"launchctl bootstrap gui/501 " + launchd.PlistPath,
		"launchctl enable gui/501/" + service.LaunchdLabel,
		"launchctl kickstart -k gui/501/" + service.LaunchdLabel,
	} {
		if commands.index(command) < 0 {
			t.Errorf("install did not run %q; ran %v", command, commands.commands)
		}
	}
	if !status.Installed || !status.Enabled || !status.Running || status.Start != service.StartAtLogin {
		t.Errorf("install reported %+v; want a running login agent", status)
	}
}

func TestLaunchdReinstallReplacesTheLiveAgent(t *testing.T) {
	launchd, commands := launchdManager(t)
	if _, err := launchd.Install(context.Background(), service.Config{Executable: "/old/omnisave"}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	commands.commands = nil
	if _, err := launchd.Install(context.Background(), service.Config{Executable: "/new/omnisave"}); err != nil {
		t.Fatalf("second install: %v", err)
	}

	bootout := commands.index("launchctl bootout gui/501/" + service.LaunchdLabel)
	bootstrap := commands.index("launchctl bootstrap gui/501 " + launchd.PlistPath)
	if bootout < 0 || bootstrap < 0 || bootout > bootstrap {
		t.Errorf("reinstall did not replace the loaded agent in order: %v", commands.commands)
	}
	plist, err := os.ReadFile(launchd.PlistPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plist), "/old/omnisave") || !strings.Contains(string(plist), "/new/omnisave") {
		t.Errorf("reinstall left the wrong executable:\n%s", plist)
	}
}

func TestLaunchdStatusAndUninstallSeparateStoppedFromMissing(t *testing.T) {
	launchd, commands := launchdManager(t)
	if _, err := launchd.Install(context.Background(), service.Config{Executable: "/bin/omnisave"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	commands.running = false
	status, err := launchd.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Installed || status.Running || !status.Enabled {
		t.Errorf("status reported %+v; want an installed, stopped agent", status)
	}

	commands.commands = nil
	if err := launchd.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if commands.index("launchctl bootout gui/501/"+service.LaunchdLabel) < 0 {
		t.Errorf("uninstall did not unload the agent: %v", commands.commands)
	}
	if _, err := os.Stat(launchd.PlistPath); !os.IsNotExist(err) {
		t.Error("uninstall left the launch agent definition behind")
	}
	status, err = launchd.Status(context.Background())
	if err != nil || status.Installed {
		t.Errorf("removed launch agent reported %+v, %v", status, err)
	}
}

func TestLaunchdRestartOnlyRestartsARunningAgent(t *testing.T) {
	launchd, commands := launchdManager(t)
	if _, err := launchd.Install(context.Background(), service.Config{Executable: "/bin/omnisave"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	commands.commands = nil

	restarted, err := launchd.Restart(context.Background())
	if err != nil || !restarted {
		t.Fatalf("restart returned (%t, %v)", restarted, err)
	}
	if commands.index("launchctl kickstart -k gui/501/"+service.LaunchdLabel) < 0 {
		t.Errorf("restart did not kickstart the agent: %v", commands.commands)
	}

	commands.running = false
	commands.commands = nil
	restarted, err = launchd.Restart(context.Background())
	if err != nil || restarted {
		t.Errorf("restart of a stopped agent returned (%t, %v)", restarted, err)
	}
	if commands.index("launchctl kickstart") >= 0 {
		t.Errorf("restart started a stopped agent: %v", commands.commands)
	}
}
