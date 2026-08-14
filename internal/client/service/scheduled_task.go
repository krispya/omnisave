package service

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// TaskName is the stable identity shown in Windows Task Scheduler.
const TaskName = "Omnisave"

const scheduledTaskTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Omnisave save synchronization</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>%s</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>%s</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>255</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>
      <Arguments>watch</Arguments>
    </Exec>
  </Actions>
</Task>
`

const taskStateScript = `$task = Get-ScheduledTask -TaskPath '\' -TaskName 'Omnisave' -ErrorAction SilentlyContinue; if ($null -eq $task) { 'missing' } else { $task.State.ToString().ToLowerInvariant() }`

// ScheduledTask manages the client as a logon-triggered task owned by the
// current Windows user. The XML remains beside the installation state so the
// player can inspect the exact native definition that was registered.
type ScheduledTask struct {
	// DefinitionPath is the XML registered with Task Scheduler.
	DefinitionPath string
	// User is the Windows account whose interactive token runs the task.
	User string
	// Run executes one command and returns its combined output.
	Run func(ctx context.Context, name string, arguments ...string) ([]byte, error)
}

// NewScheduledTask resolves the current user's local application data and
// Windows account.
func NewScheduledTask() (*ScheduledTask, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if strings.TrimSpace(localAppData) == "" {
		return nil, fmt.Errorf("LOCALAPPDATA is not set")
	}
	current, err := user.Current()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(current.Username) == "" {
		return nil, fmt.Errorf("the current user has no Task Scheduler identity")
	}
	return &ScheduledTask{
		DefinitionPath: filepath.Join(localAppData, "omnisave", "omnisave-task.xml"),
		User:           current.Username,
		Run:            runCommand,
	}, nil
}

func (s *ScheduledTask) command(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	output, err := s.Run(ctx, name, arguments...)
	if err == nil {
		return output, nil
	}
	command := name + " " + strings.Join(arguments, " ")
	if detail := strings.TrimSpace(string(output)); detail != "" {
		return nil, fmt.Errorf("%s: %s", command, detail)
	}
	return nil, fmt.Errorf("%s: %w", command, err)
}

func (s *ScheduledTask) Install(ctx context.Context, config Config) (Status, error) {
	if strings.TrimSpace(config.Executable) == "" {
		return Status{}, fmt.Errorf("the service needs a client to run")
	}
	if previous, err := s.taskState(ctx); err != nil {
		return Status{}, err
	} else if previous == "running" {
		if _, err := s.command(ctx, "schtasks.exe", "/End", "/TN", TaskName); err != nil {
			return Status{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.DefinitionPath), 0o755); err != nil {
		return Status{}, fmt.Errorf("cannot create %s: %w", filepath.Dir(s.DefinitionPath), err)
	}
	identity := xmlText(s.User)
	definition := fmt.Sprintf(scheduledTaskTemplate, identity, identity, xmlText(config.Executable))
	if err := os.WriteFile(s.DefinitionPath, []byte(definition), 0o644); err != nil {
		return Status{}, fmt.Errorf("cannot write %s: %w", s.DefinitionPath, err)
	}
	if _, err := s.command(ctx, "schtasks.exe", "/Create", "/TN", TaskName, "/XML", s.DefinitionPath, "/F"); err != nil {
		return Status{}, err
	}
	if _, err := s.command(ctx, "schtasks.exe", "/Run", "/TN", TaskName); err != nil {
		return Status{}, err
	}
	return s.Status(ctx)
}

func (s *ScheduledTask) Uninstall(ctx context.Context) error {
	status, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if !status.Installed {
		if err := os.Remove(s.DefinitionPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot remove %s: %w", s.DefinitionPath, err)
		}
		return nil
	}
	if status.Running {
		if _, err := s.command(ctx, "schtasks.exe", "/End", "/TN", TaskName); err != nil {
			return err
		}
	}
	if _, err := s.command(ctx, "schtasks.exe", "/Delete", "/TN", TaskName, "/F"); err != nil {
		return err
	}
	if err := os.Remove(s.DefinitionPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot remove %s: %w", s.DefinitionPath, err)
	}
	return nil
}

func (s *ScheduledTask) Status(ctx context.Context) (Status, error) {
	status := Status{Definition: s.DefinitionPath}
	if _, err := os.Stat(s.DefinitionPath); err != nil {
		if os.IsNotExist(err) {
			return status, nil
		}
		return status, err
	}
	state, err := s.taskState(ctx)
	if err != nil {
		return status, err
	}
	if state == "missing" {
		return status, nil
	}
	status.Installed = true
	status.Enabled = state != "disabled"
	if status.Enabled {
		status.Start = StartAtLogin
	}
	status.Running = state == "running"
	return status, nil
}

func (s *ScheduledTask) taskState(ctx context.Context) (string, error) {
	output, err := s.command(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", taskStateScript)
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(string(output))), nil
}

func (s *ScheduledTask) Restart(ctx context.Context) (bool, error) {
	status, err := s.Status(ctx)
	if err != nil || !status.Running {
		return false, err
	}
	if _, err := s.command(ctx, "schtasks.exe", "/End", "/TN", TaskName); err != nil {
		return false, err
	}
	if _, err := s.command(ctx, "schtasks.exe", "/Run", "/TN", TaskName); err != nil {
		return false, err
	}
	return true, nil
}

var _ Manager = (*ScheduledTask)(nil)
