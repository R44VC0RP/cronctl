//go:build windows

package cronctl

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

func serviceDir(path string) string { return filepath.Dir(path) }

func servicePlanFor(_ paths, executable string) (ServicePlan, error) {
	daemon := filepath.Join(filepath.Dir(executable), "cronctl-daemon.exe")
	return ServicePlan{Platform: "windows-hkcu-run", ConfigPath: runKey + `\cronctl`, Command: fmt.Sprintf(`reg.exe ADD %s /v cronctl /t REG_SZ /d "\"%s\"" /f`, runKey, daemon)}, nil
}

func platformServiceInstall(p paths, executable string, dryRun bool) (ServicePlan, error) {
	plan, err := servicePlanFor(p, executable)
	if err != nil || dryRun {
		return plan, err
	}
	daemon := filepath.Join(filepath.Dir(executable), "cronctl-daemon.exe")
	if _, err := os.Stat(daemon); err != nil {
		return plan, fmt.Errorf("cronctl-daemon.exe must be next to cronctl.exe: %w", err)
	}
	if output, err := exec.Command("reg.exe", "ADD", runKey, "/v", "cronctl", "/t", "REG_SZ", "/d", `"`+daemon+`"`, "/f").CombinedOutput(); err != nil {
		return plan, fmt.Errorf("%w: %s", err, output)
	}
	if err := exec.Command(daemon).Start(); err != nil {
		return plan, err
	}
	return plan, nil
}

func platformServiceStatus(_ paths) (ServiceState, error) {
	err := exec.Command("reg.exe", "QUERY", runKey, "/v", "cronctl").Run()
	return ServiceState{Installed: err == nil, Running: false, Detail: "running state is determined by daemon heartbeat"}, nil
}

func platformServiceUninstall(_ paths) error {
	if exec.Command("reg.exe", "QUERY", runKey, "/v", "cronctl").Run() != nil {
		return nil
	}
	output, err := exec.Command("reg.exe", "DELETE", runKey, "/v", "cronctl", "/f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}
	return nil
}
