package state

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/davinci42/WinMoi/internal/syswin"
)

func TestStateSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "winmoi", "state.json")
	st := New()
	st.Links["k1"] = LinkRecord{
		Target:    "/tmp/a",
		Source:    "/tmp/b",
		Type:      "symlink",
		Backup:    "/tmp/a.bak.1",
		Category:  "config",
		Owner:     "links",
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}
	st.UpstreamSkills["vercel-labs/agent-skills"] = UpstreamSkillRecord{
		Spec: "vercel-labs/agent-skills",
		Dirs: []string{"react-best-practices"},
	}

	if err := Save(p, st); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Links["k1"].Backup != "/tmp/a.bak.1" {
		t.Fatalf("unexpected link record: %+v", loaded.Links["k1"])
	}
	if len(loaded.UpstreamSkills["vercel-labs/agent-skills"].Dirs) != 1 {
		t.Fatalf("unexpected upstream skill record: %+v", loaded.UpstreamSkills)
	}
}

func TestAcquireLockExclusivity(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "lock")
	l1, err := syswin.AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("first AcquireLock failed: %v", err)
	}
	defer func() { _ = l1.Release() }()

	if _, err := syswin.AcquireLock(lockPath); !errors.Is(err, syswin.ErrLockHeld) {
		t.Fatalf("expected ErrLockHeld on second acquire, got %v", err)
	}

	if err := l1.Release(); err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	l2, err := syswin.AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock after release failed: %v", err)
	}
	_ = l2.Release()
}
