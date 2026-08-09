//go:build darwin

package cronctl

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

const launchLabel = "dev.cronctl.daemon"

func serviceDir(path string) string { return filepath.Dir(path) }

func servicePlanFor(p paths, executable string) (ServicePlan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ServicePlan{}, err
	}
	configPath := filepath.Join(home, "Library", "LaunchAgents", launchLabel+".plist")
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string><string>daemon</string><string>run</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, launchLabel, html.EscapeString(executable), html.EscapeString(filepath.Join(p.state, "daemon.stdout.log")), html.EscapeString(filepath.Join(p.state, "daemon.stderr.log")))
	return ServicePlan{Platform: "darwin-launchd", ConfigPath: configPath, Content: content, Command: "launchctl bootstrap gui/" + strconv.Itoa(os.Getuid()) + " " + configPath}, nil
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
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+launchLabel).Run()
	if output, err := exec.Command("launchctl", "bootstrap", domain, plan.ConfigPath).CombinedOutput(); err != nil {
		return plan, fmt.Errorf("%w: %s", err, output)
	}
	return plan, nil
}

func platformServiceStatus(_ paths) (ServiceState, error) {
	domain := "gui/" + strconv.Itoa(os.Getuid()) + "/" + launchLabel
	output, err := exec.Command("launchctl", "print", domain).CombinedOutput()
	if err != nil {
		return ServiceState{Installed: false, Running: false, Detail: string(output)}, nil
	}
	return ServiceState{Installed: true, Running: true, Detail: "launchd agent loaded"}, nil
}

func platformServiceUninstall(p paths) error {
	plan, err := servicePlanFor(p, "")
	if err != nil {
		return err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+launchLabel).Run()
	if err := os.Remove(plan.ConfigPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
