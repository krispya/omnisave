package service_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/service"
)

// recorder stands in for the commands a real systemd would answer. Every test
// here is about what the manager says and asks for, which is the whole of what
// it does: the file it writes and the commands it runs.
type recorder struct {
	// commands is every invocation, joined, in the order they were made.
	commands []string
	// answers maps a command to what it replies with; anything unanswered
	// succeeds silently, which is what the commands that only act do.
	answers map[string]string
	// failures maps a command to an error it fails with.
	failures map[string]string
}

func newRecorder() *recorder {
	return &recorder{answers: make(map[string]string), failures: make(map[string]string)}
}

func (r *recorder) run(_ context.Context, name string, arguments ...string) ([]byte, error) {
	command := strings.TrimSpace(name + " " + strings.Join(arguments, " "))
	r.commands = append(r.commands, command)
	if detail, failed := r.failures[command]; failed {
		return []byte(detail), errors.New("exit status 1")
	}
	if answer, known := r.answers[command]; known {
		return []byte(answer + "\n"), nil
	}
	return nil, nil
}

func (r *recorder) ran(command string) bool {
	for _, made := range r.commands {
		if made == command {
			return true
		}
	}
	return false
}

func (r *recorder) index(command string) int {
	for position, made := range r.commands {
		if made == command {
			return position
		}
	}
	return -1
}

func manager(t *testing.T) (*service.Systemd, *recorder) {
	t.Helper()
	commands := newRecorder()
	return &service.Systemd{
		UnitPath: filepath.Join(t.TempDir(), "systemd", "user", service.UnitName),
		User:     "deck",
		Run:      commands.run,
	}, commands
}

// running answers the questions a status asks about a live, enabled,
// lingering service.
func running(commands *recorder) {
	commands.answers["systemctl --user is-enabled "+service.UnitName] = "enabled"
	commands.answers["systemctl --user is-active "+service.UnitName] = "active"
	commands.answers["systemctl --user show "+service.UnitName+" --property=ActiveState --value"] = "active"
	commands.answers["loginctl show-user deck --property=Linger --value"] = "yes"
}

func TestInstallWritesTheUnitAndStartsIt(t *testing.T) {
	systemd, commands := manager(t)
	running(commands)

	status, err := systemd.Install(context.Background(), service.Config{Executable: "/home/deck/.local/bin/omnisave"})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	unit, err := os.ReadFile(systemd.UnitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	written := string(unit)
	if !strings.Contains(written, "ExecStart=/home/deck/.local/bin/omnisave watch") {
		t.Errorf("unit does not run the client that was asked for:\n%s", written)
	}
	// The restart policy is the difference between a service and a process
	// that ran once, so it is worth asserting rather than trusting.
	for _, directive := range []string{"Restart=always", "StartLimitIntervalSec=0", "WantedBy=default.target"} {
		if !strings.Contains(written, directive) {
			t.Errorf("unit is missing %s:\n%s", directive, written)
		}
	}

	for _, command := range []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable " + service.UnitName,
		"systemctl --user restart " + service.UnitName,
		"loginctl enable-linger deck",
	} {
		if !commands.ran(command) {
			t.Errorf("install did not run %q; ran %v", command, commands.commands)
		}
	}
	// A unit systemd has not read yet is a unit enable cannot find.
	if commands.index("systemctl --user daemon-reload") > commands.index("systemctl --user enable "+service.UnitName) {
		t.Errorf("enable ran before daemon-reload: %v", commands.commands)
	}
	if !status.Installed || !status.Enabled || !status.Running || status.Start != service.StartAtBoot {
		t.Errorf("install reported %+v; want a service that is on in every sense", status)
	}
}

// Re-installing is how the unit changes, so it has to overwrite what is there
// and restart onto it rather than leaving the old definition running.
func TestInstallRewritesAnExistingUnit(t *testing.T) {
	systemd, commands := manager(t)
	running(commands)
	if _, err := systemd.Install(context.Background(), service.Config{Executable: "/old/omnisave"}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if _, err := systemd.Install(context.Background(), service.Config{Executable: "/new/omnisave"}); err != nil {
		t.Fatalf("second install: %v", err)
	}

	unit, err := os.ReadFile(systemd.UnitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	if strings.Contains(string(unit), "/old/omnisave") {
		t.Errorf("re-install left the previous client in the unit:\n%s", unit)
	}
	if !strings.Contains(string(unit), "ExecStart=/new/omnisave watch") {
		t.Errorf("re-install did not write the new client:\n%s", unit)
	}
}

// A path systemd's parser would split has to survive being written into it.
func TestInstallQuotesAPathWithSpaces(t *testing.T) {
	systemd, commands := manager(t)
	running(commands)
	if _, err := systemd.Install(context.Background(), service.Config{Executable: "/home/a b/omnisave"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	unit, err := os.ReadFile(systemd.UnitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	if !strings.Contains(string(unit), `ExecStart="/home/a b/omnisave" watch`) {
		t.Errorf("unit did not quote a path with a space:\n%s", unit)
	}
}

// ExecStart expands % specifiers and $ variables even inside quotes, so a
// path carrying either would come out changed — or fail the unit outright on
// an unknown specifier. Both are escaped so the service runs the client that
// was asked for, wherever it was installed.
func TestInstallEscapesSpecifiersAndVariablesInThePath(t *testing.T) {
	systemd, commands := manager(t)
	running(commands)
	if _, err := systemd.Install(context.Background(), service.Config{Executable: "/home/deck/100% games/om$ave"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	unit, err := os.ReadFile(systemd.UnitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	if !strings.Contains(string(unit), `ExecStart="/home/deck/100%% games/om$$ave" watch`) {
		t.Errorf("unit did not escape the path for systemd:\n%s", unit)
	}
}

// Linger can be refused by policy with nobody there to authorize it. The
// service still starts with the session, so the install stands and only the
// status says the gap is there.
func TestInstallSurvivesRefusedLinger(t *testing.T) {
	systemd, commands := manager(t)
	running(commands)
	commands.failures["loginctl enable-linger deck"] = "Interactive authentication required."
	commands.answers["loginctl show-user deck --property=Linger --value"] = "no"

	status, err := systemd.Install(context.Background(), service.Config{Executable: "/bin/omnisave"})
	if err != nil {
		t.Fatalf("install failed over linger: %v", err)
	}
	if !status.Running || !status.Enabled {
		t.Errorf("install reported %+v; want a running, enabled service", status)
	}
	if status.Start == service.StartAtBoot {
		t.Error("install claimed linger after it was refused")
	}
	if status.Start != service.StartAtLogin {
		t.Errorf("install reported start mode %v; want login after linger was refused", status.Start)
	}
}

// A failing systemctl has to say what systemctl said: on a machine with no
// user manager that message is the entire diagnosis.
func TestInstallReportsWhatSystemctlSaid(t *testing.T) {
	systemd, commands := manager(t)
	commands.failures["systemctl --user daemon-reload"] = "Failed to connect to bus: No such file or directory"

	if _, err := systemd.Install(context.Background(), service.Config{Executable: "/bin/omnisave"}); err == nil {
		t.Fatal("install succeeded with no user manager")
	} else if !strings.Contains(err.Error(), "Failed to connect to bus") {
		t.Errorf("install reported %q; want systemctl's own words", err)
	}
}

func TestInstallNeedsAClientToRun(t *testing.T) {
	systemd, _ := manager(t)
	if _, err := systemd.Install(context.Background(), service.Config{}); err == nil {
		t.Fatal("install accepted an empty executable")
	}
	if _, err := os.Stat(systemd.UnitPath); !os.IsNotExist(err) {
		t.Error("a refused install still wrote a unit")
	}
}

func TestStatusOnADeviceWithNoService(t *testing.T) {
	systemd, commands := manager(t)
	running(commands)

	status, err := systemd.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Installed || status.Running || status.Enabled {
		t.Errorf("status reported %+v for a device with no unit", status)
	}
	// Nothing is asked of systemd about a service that is not defined.
	if len(commands.commands) != 0 {
		t.Errorf("status questioned systemd about a missing unit: %v", commands.commands)
	}
	if status.Definition != systemd.UnitPath {
		t.Errorf("status named %q; want the path a unit would live at", status.Definition)
	}
}

// Installed and stopped is the state a status exists to distinguish, because
// it is the one a device with no terminal cannot tell from working.
func TestStatusSeparatesStoppedFromMissing(t *testing.T) {
	systemd, commands := manager(t)
	running(commands)
	if _, err := systemd.Install(context.Background(), service.Config{Executable: "/bin/omnisave"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	commands.answers["systemctl --user is-active "+service.UnitName] = "failed"

	status, err := systemd.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Installed || !status.Enabled {
		t.Errorf("status reported %+v; want an installed, enabled service", status)
	}
	if status.Running {
		t.Error("status called a failed service running")
	}
}

func TestUninstallStopsBeforeRemoving(t *testing.T) {
	systemd, commands := manager(t)
	running(commands)
	if _, err := systemd.Install(context.Background(), service.Config{Executable: "/bin/omnisave"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	commands.commands = nil

	if err := systemd.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(systemd.UnitPath); !os.IsNotExist(err) {
		t.Error("uninstall left the unit behind")
	}
	disable := "systemctl --user disable --now " + service.UnitName
	if !commands.ran(disable) {
		t.Errorf("uninstall did not disable the service: %v", commands.commands)
	}
	if !commands.ran("systemctl --user daemon-reload") {
		t.Errorf("uninstall did not reload: %v", commands.commands)
	}
	// Disabling needs the unit to find what starts it.
	if commands.index(disable) > commands.index("systemctl --user daemon-reload") {
		t.Errorf("uninstall removed the unit before disabling it: %v", commands.commands)
	}
}

// The end state is the same either way, so removing nothing is not a failure.
func TestUninstallWithNoServiceIsNotAFailure(t *testing.T) {
	systemd, commands := manager(t)
	if err := systemd.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if commands.ran("systemctl --user disable --now " + service.UnitName) {
		t.Errorf("uninstall disabled a service that was never installed: %v", commands.commands)
	}
}

func TestRestartOnlyRestartsARunningService(t *testing.T) {
	systemd, commands := manager(t)
	running(commands)
	if _, err := systemd.Install(context.Background(), service.Config{Executable: "/bin/omnisave"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	commands.commands = nil

	restarted, err := systemd.Restart(context.Background())
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if !restarted || !commands.ran("systemctl --user restart "+service.UnitName) {
		t.Errorf("restart did not restart a running service: %v", commands.commands)
	}

	// A service the player stopped stays stopped: an update replacing a
	// binary is no reason to start something that was turned off.
	commands.answers["systemctl --user show "+service.UnitName+" --property=ActiveState --value"] = "inactive"
	commands.commands = nil
	restarted, err = systemd.Restart(context.Background())
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if restarted || commands.ran("systemctl --user restart "+service.UnitName) {
		t.Errorf("restart started a stopped service: %v", commands.commands)
	}
}

func TestRestartWithNoServiceDoesNothing(t *testing.T) {
	systemd, commands := manager(t)
	restarted, err := systemd.Restart(context.Background())
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if restarted || len(commands.commands) != 0 {
		t.Errorf("restart acted on a device with no service: %v", commands.commands)
	}
}

// A manager that cannot be reached is not a service that is stopped: telling
// an update "nothing was running" would leave a lingering service executing
// the binary it just replaced, silently, on the devices with nobody watching.
func TestRestartReportsAManagerItCannotReach(t *testing.T) {
	systemd, commands := manager(t)
	running(commands)
	if _, err := systemd.Install(context.Background(), service.Config{Executable: "/bin/omnisave"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	commands.failures["systemctl --user show "+service.UnitName+" --property=ActiveState --value"] = "Failed to connect to bus: No such file or directory"

	if _, err := systemd.Restart(context.Background()); err == nil {
		t.Fatal("restart called an unreachable manager a stopped service")
	} else if !strings.Contains(err.Error(), "Failed to connect to bus") {
		t.Errorf("restart reported %q; want systemctl's own words", err)
	}
}

// Every platform answers the question; only some can act on it. A caller
// deciding whether to mention the service should not need a platform check.
func TestUnsupportedPlatformsAnswerRatherThanFail(t *testing.T) {
	if service.Supported() {
		t.Skip("this platform has a service manager")
	}
	manager := service.PlatformManager()
	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Installed {
		t.Error("a platform with no manager claimed an installed service")
	}
	restarted, err := manager.Restart(context.Background())
	if err != nil || restarted {
		t.Errorf("restart reported (%t, %v); want a silent no", restarted, err)
	}
	if _, err := manager.Install(context.Background(), service.Config{Executable: "/bin/omnisave"}); !errors.Is(err, service.ErrUnsupported) {
		t.Errorf("install reported %v; want ErrUnsupported", err)
	}
}

func TestSupportedMatchesTheManager(t *testing.T) {
	// The two have to agree: every command branches on Supported and then
	// acts through the manager.
	manager := service.PlatformManager()
	_, isSystemd := manager.(*service.Systemd)
	_, isLaunchd := manager.(*service.Launchd)
	_, isScheduledTask := manager.(*service.ScheduledTask)
	implemented := isSystemd || isLaunchd || isScheduledTask
	if implemented != service.Supported() {
		t.Errorf("Supported() is %t but the manager is %s",
			service.Supported(), fmt.Sprintf("%T", manager))
	}
}
