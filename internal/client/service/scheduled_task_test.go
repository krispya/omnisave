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

type taskRecorder struct {
	commands   []string
	definition string
	installed  bool
	running    bool
}

func (r *taskRecorder) run(_ context.Context, name string, arguments ...string) ([]byte, error) {
	command := strings.TrimSpace(name + " " + strings.Join(arguments, " "))
	r.commands = append(r.commands, command)
	if name == "powershell.exe" {
		if r.running {
			return []byte("running\r\n"), nil
		}
		if r.installed {
			return []byte("ready\r\n"), nil
		}
		return []byte("missing\r\n"), nil
	}
	if len(arguments) == 0 {
		return nil, nil
	}
	switch arguments[0] {
	case "/Create":
		for index, argument := range arguments {
			if argument == "/XML" && index+1 < len(arguments) {
				definition, err := os.ReadFile(arguments[index+1])
				if err != nil {
					return nil, err
				}
				r.definition = string(definition)
			}
		}
		r.installed = true
	case "/Run":
		if !r.installed {
			return []byte("The system cannot find the task specified."), errors.New("exit status 1")
		}
		r.running = true
	case "/End":
		r.running = false
	case "/Delete":
		r.installed = false
		r.running = false
	case "/Query":
		if !r.installed {
			return []byte("The system cannot find the task specified."), errors.New("exit status 1")
		}
		return []byte(r.definition), nil
	}
	return nil, nil
}

func (r *taskRecorder) index(prefix string) int {
	for index, command := range r.commands {
		if strings.HasPrefix(command, prefix) {
			return index
		}
	}
	return -1
}

func taskManager(t *testing.T) (*service.ScheduledTask, *taskRecorder) {
	t.Helper()
	commands := &taskRecorder{}
	return &service.ScheduledTask{
		DefinitionPath: filepath.Join(t.TempDir(), "Omnisave", "omnisave-task.xml"),
		User:           `DESKTOP\Player & One`,
		Run:            commands.run,
	}, commands
}

func TestScheduledTaskInstallDefinesAndStartsTheUserTask(t *testing.T) {
	task, commands := taskManager(t)
	executable := `C:\Program Files\Omnisave & Games\omnisave.exe`

	status, err := task.Install(context.Background(), service.Config{Executable: executable})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	definition, err := os.ReadFile(task.DefinitionPath)
	if err != nil {
		t.Fatalf("read scheduled task: %v", err)
	}
	written := string(definition)
	var document struct{ XMLName xml.Name }
	if err := xml.Unmarshal(definition, &document); err != nil || document.XMLName.Local != "Task" {
		t.Fatalf("scheduled task is not an XML task document: %v", err)
	}
	for _, wanted := range []string{
		`<UserId>DESKTOP\Player &amp; One</UserId>`,
		`<LogonType>InteractiveToken</LogonType>`,
		`<RunLevel>LeastPrivilege</RunLevel>`,
		`<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>`,
		`<RestartOnFailure>`,
		`<Command>C:\Program Files\Omnisave &amp; Games\omnisave.exe</Command>`,
		`<Arguments>watch</Arguments>`,
	} {
		if !strings.Contains(written, wanted) {
			t.Errorf("scheduled task is missing %q:\n%s", wanted, written)
		}
	}
	for _, command := range []string{
		"schtasks.exe /Create /TN " + service.TaskName + " /XML " + task.DefinitionPath + " /F",
		"schtasks.exe /Run /TN " + service.TaskName,
	} {
		if commands.index(command) < 0 {
			t.Errorf("install did not run %q; ran %v", command, commands.commands)
		}
	}
	if !status.Installed || !status.Enabled || !status.Running || status.Start != service.StartAtLogin {
		t.Errorf("install reported %+v; want a running logon task", status)
	}
}

func TestScheduledTaskReinstallRepairsADeletedRegistration(t *testing.T) {
	task, commands := taskManager(t)
	if _, err := task.Install(context.Background(), service.Config{Executable: `C:\Omnisave\omnisave.exe`}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	// A player can delete the task in Task Scheduler while its readable XML
	// remains. Re-running install is the repair path, not an error.
	commands.installed = false
	commands.running = false
	commands.commands = nil

	status, err := task.Install(context.Background(), service.Config{Executable: `C:\Omnisave\omnisave.exe`})
	if err != nil {
		t.Fatalf("repair install: %v", err)
	}
	if !status.Installed || !status.Running {
		t.Errorf("repair install reported %+v", status)
	}
	if commands.index("schtasks.exe /Create /TN "+service.TaskName) < 0 {
		t.Errorf("repair did not recreate the task: %v", commands.commands)
	}
}

func TestScheduledTaskReinstallStopsAndReplacesTheLiveTask(t *testing.T) {
	task, commands := taskManager(t)
	if _, err := task.Install(context.Background(), service.Config{Executable: `C:\old\omnisave.exe`}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	commands.commands = nil
	if _, err := task.Install(context.Background(), service.Config{Executable: `C:\new\omnisave.exe`}); err != nil {
		t.Fatalf("second install: %v", err)
	}

	end := commands.index("schtasks.exe /End /TN " + service.TaskName)
	create := commands.index("schtasks.exe /Create /TN " + service.TaskName)
	if end < 0 || create < 0 || end > create {
		t.Errorf("reinstall did not stop before replacing the task: %v", commands.commands)
	}
	if strings.Contains(commands.definition, `C:\old\omnisave.exe`) || !strings.Contains(commands.definition, `C:\new\omnisave.exe`) {
		t.Errorf("reinstall registered the wrong executable:\n%s", commands.definition)
	}
}

func TestScheduledTaskStatusAndUninstallSeparateStoppedFromMissing(t *testing.T) {
	task, commands := taskManager(t)
	if _, err := task.Install(context.Background(), service.Config{Executable: `C:\Omnisave\omnisave.exe`}); err != nil {
		t.Fatalf("install: %v", err)
	}
	commands.running = false
	status, err := task.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Installed || status.Running || !status.Enabled {
		t.Errorf("status reported %+v; want an installed, stopped task", status)
	}

	commands.commands = nil
	if err := task.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if commands.index("schtasks.exe /Delete /TN "+service.TaskName+" /F") < 0 {
		t.Errorf("uninstall did not delete the registered task: %v", commands.commands)
	}
	if _, err := os.Stat(task.DefinitionPath); !os.IsNotExist(err) {
		t.Error("uninstall left the task definition behind")
	}
	status, err = task.Status(context.Background())
	if err != nil || status.Installed {
		t.Errorf("removed task reported %+v, %v", status, err)
	}
}

func TestScheduledTaskRestartOnlyRestartsARunningTask(t *testing.T) {
	task, commands := taskManager(t)
	if _, err := task.Install(context.Background(), service.Config{Executable: `C:\Omnisave\omnisave.exe`}); err != nil {
		t.Fatalf("install: %v", err)
	}
	commands.commands = nil

	restarted, err := task.Restart(context.Background())
	if err != nil || !restarted {
		t.Fatalf("restart returned (%t, %v)", restarted, err)
	}
	end := commands.index("schtasks.exe /End /TN " + service.TaskName)
	run := commands.index("schtasks.exe /Run /TN " + service.TaskName)
	if end < 0 || run < 0 || end > run {
		t.Errorf("restart did not stop before starting the task: %v", commands.commands)
	}

	commands.running = false
	commands.commands = nil
	restarted, err = task.Restart(context.Background())
	if err != nil || restarted {
		t.Errorf("restart of a stopped task returned (%t, %v)", restarted, err)
	}
	if commands.index("schtasks.exe /Run") >= 0 {
		t.Errorf("restart started a stopped task: %v", commands.commands)
	}
}
