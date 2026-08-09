package cronctl

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type paths struct {
	config    string
	state     string
	jobs      string
	history   string
	runs      string
	locks     string
	jobState  string
	heartbeat string
}

func resolvePaths() (paths, error) {
	if home := os.Getenv("CRONCTL_HOME"); home != "" {
		return makePaths(filepath.Join(home, "config"), filepath.Join(home, "state"))
	}

	configBase, err := os.UserConfigDir()
	if err != nil {
		return paths{}, fmt.Errorf("find config directory: %w", err)
	}
	config := filepath.Join(configBase, "cronctl")

	var state string
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return paths{}, fmt.Errorf("find home directory: %w", err)
		}
		state = filepath.Join(home, "Library", "Application Support", "cronctl")
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = configBase
		}
		state = filepath.Join(base, "cronctl")
	default:
		base := os.Getenv("XDG_STATE_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return paths{}, fmt.Errorf("find home directory: %w", err)
			}
			base = filepath.Join(home, ".local", "state")
		}
		state = filepath.Join(base, "cronctl")
	}
	return makePaths(config, state)
}

func makePaths(config, state string) (paths, error) {
	p := paths{
		config:    config,
		state:     state,
		jobs:      filepath.Join(config, "jobs"),
		history:   filepath.Join(state, "history"),
		runs:      filepath.Join(state, "runs"),
		locks:     filepath.Join(state, "locks"),
		jobState:  filepath.Join(state, "jobstate"),
		heartbeat: filepath.Join(state, "daemon.json"),
	}
	for _, dir := range []string{p.config, p.state, p.jobs, p.history, p.runs, p.locks, p.jobState} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return paths{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return p, nil
}
