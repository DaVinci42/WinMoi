package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInitStatusLinkAndUnlinkCLI(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	lockPath := filepath.Join(dir, "lock")

	// 1. init --with-skill
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"init", "--dir", dir, "--with-skill"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v\n%s", err, out.String())
	}
	manifestPath := filepath.Join(dir, "manifest.toml")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest.toml not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent", "skills", "winmoi-maintenance", "SKILL.md")); err != nil {
		t.Fatalf("maintenance SKILL.md not created: %v", err)
	}

	// 2. status --json
	out.Reset()
	cmd2 := NewRootCmd()
	cmd2.SetOut(&out)
	cmd2.SetErr(&out)
	cmd2.SetArgs([]string{"status", "--config", manifestPath, "--state", statePath, "--json"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("status failed: %v\n%s", err, out.String())
	}
	var statusRep StatusReport
	if err := json.Unmarshal(out.Bytes(), &statusRep); err != nil {
		t.Fatalf("invalid status json: %v\n%s", err, out.String())
	}
	if len(statusRep.Skills.LocalMissing) != 1 || statusRep.Skills.LocalMissing[0] != "winmoi-maintenance" {
		t.Fatalf("expected winmoi-maintenance missing in status, got %+v", statusRep.Skills)
	}

	// 3. apply --dry-run
	out.Reset()
	cmd3 := NewRootCmd()
	cmd3.SetOut(&out)
	cmd3.SetErr(&out)
	cmd3.SetArgs([]string{"apply", "--config", manifestPath, "--state", statePath, "--lock", lockPath, "--dry-run"})
	if err := cmd3.Execute(); err != nil {
		t.Fatalf("apply --dry-run failed: %v\n%s", err, out.String())
	}
}
