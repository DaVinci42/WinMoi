package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const CurrentVersion = 1

type LinkRecord struct {
	Target    string    `json:"target"`
	Source    string    `json:"source"`
	Type      string    `json:"type"`
	Backup    string    `json:"backup,omitempty"`
	Category  string    `json:"category"`
	Owner     string    `json:"owner,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpstreamSkillRecord struct {
	Spec      string    `json:"spec"`
	Dirs      []string  `json:"dirs"`
	UpdatedAt time.Time `json:"updated_at"`
}

type State struct {
	Version        int                            `json:"version"`
	Links          map[string]LinkRecord          `json:"links"`
	UpstreamSkills map[string]UpstreamSkillRecord `json:"upstream_skills"`
}

func New() *State {
	return &State{
		Version:        CurrentVersion,
		Links:          make(map[string]LinkRecord),
		UpstreamSkills: make(map[string]UpstreamSkillRecord),
	}
}

func DefaultDir() string {
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "winmoi")
		}
	}
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "winmoi")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "state", "winmoi")
	}
	return ".winmoi"
}

func DefaultStatePath() string {
	return filepath.Join(DefaultDir(), "state.json")
}

func DefaultLockPath() string {
	return filepath.Join(DefaultDir(), "lock")
}

func Load(path string) (*State, error) {
	if path == "" {
		path = DefaultStatePath()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return New(), nil
		}
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}
	st := New()
	if err := json.Unmarshal(raw, st); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", path, err)
	}
	if st.Version == 0 {
		st.Version = CurrentVersion
	}
	if st.Links == nil {
		st.Links = make(map[string]LinkRecord)
	}
	if st.UpstreamSkills == nil {
		st.UpstreamSkills = make(map[string]UpstreamSkillRecord)
	}
	return st, nil
}

func Save(path string, st *State) error {
	if path == "" {
		path = DefaultStatePath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	if st.Version == 0 {
		st.Version = CurrentVersion
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit state %s: %w", path, err)
	}
	return nil
}
