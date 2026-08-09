//go:build linux

package cronctl

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func serviceDir(path string) string { return filepath.Dir(path) }

func servicePlanFor(_ paths, executable string) (ServicePlan, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return ServicePlan{}, err
	}
	path := filepath.Join(config, "systemd", "user", "cronctl.service")
	quoted := strconv.Quote(strings.ReplaceAll(executable, "%", "%%"))
	content := fmt.Sprintf(`[Unit]
Description=cronctl user scheduler

[Service]
Type=simple
ExecStart=%s daemon run
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
`, quoted)
	return ServicePlan{Platform: "linux-systemd-user", ConfigPath: path, Content: content, Command: "systemctl --user daemon-reload && systemctl --user enable --now cronctl.service"}, nil
}

func platformServiceInstall(p paths, executable string, dryRun bool) (ServicePlan, error) {
	plan, err := servicePlanFor(p, executable)
	if err != nil || dryRun {
		return plan, err
	}
	if err := os.MkdirAll(filepath.Dir(plan.ConfigPath), 0o700); err != nil {
		return plan, err
	}
	if err := writePrivateFile(plan.ConfigPath, []byte(plan.Content)); err != nil {
		return plan, err
	}
	if output, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return plan, fmt.Errorf("%w: %s", err, output)
	}
	if output, err := exec.Command("systemctl", "--user", "enable", "--now", "cronctl.service").CombinedOutput(); err != nil {
		return plan, fmt.Errorf("%w: %s", err, output)
	}
	return plan, nil
}

func platformServiceStatus(_ paths) (ServiceState, error) {
	err := exec.Command("systemctl", "--user", "is-active", "--quiet", "cronctl.service").Run()
	if err == nil {
		return ServiceState{Installed: true, Running: true, Detail: "systemd user unit active"}, nil
	}
	installed := exec.Command("systemctl", "--user", "is-enabled", "--quiet", "cronctl.service").Run() == nil
	return ServiceState{Installed: installed, Running: false, Detail: "systemd user unit inactive"}, nil
}

func platformServiceUninstall(_ paths) error {
	_ = exec.Command("systemctl", "--user", "disable", "--now", "cronctl.service").Run()
	plan, err := servicePlanFor(paths{}, "")
	if err != nil {
		return err
	}
	if err := os.Remove(plan.ConfigPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}
