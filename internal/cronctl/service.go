package cronctl

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type ServicePlan struct {
	Platform   string `json:"platform"`
	ConfigPath string `json:"config_path"`
	Content    string `json:"content,omitempty"`
	Command    string `json:"command,omitempty"`
}

type ServiceState struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Detail    string `json:"detail,omitempty"`
}

func daemonStatus(s *store) (Heartbeat, bool, error) {
	data, err := os.ReadFile(s.paths.heartbeat)
	if errors.Is(err, os.ErrNotExist) {
		return Heartbeat{}, false, nil
	}
	if err != nil {
		return Heartbeat{}, false, err
	}
	var heartbeat Heartbeat
	if err := json.Unmarshal(data, &heartbeat); err != nil {
		return Heartbeat{}, false, err
	}
	return heartbeat, time.Since(heartbeat.LastTick) < 5*time.Second, nil
}

func writePrivateFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(serviceDir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceFile(tmpName, path)
}

func serviceError(action string, err error) error {
	return &cliError{Code: "SERVICE_FAILED", Message: fmt.Sprintf("service %s failed: %v", action, err), Exit: 6}
}
