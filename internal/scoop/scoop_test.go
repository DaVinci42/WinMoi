package scoop

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davinci42/WinMoi/internal/config"
	"github.com/davinci42/WinMoi/internal/runner"
	"github.com/davinci42/WinMoi/internal/syswin"
)

type fakeRunner struct {
	dryRun bool
	calls  []runner.Command
	onRun  func(cmd runner.Command) (runner.Result, error)
}

func (f *fakeRunner) Run(_ context.Context, cmd runner.Command) (runner.Result, error) {
	f.calls = append(f.calls, cmd)
	if f.onRun != nil {
		return f.onRun(cmd)
	}
	return runner.Result{}, nil
}

func (f *fakeRunner) RunPowerShell(_ context.Context, script string, dir string) (runner.Result, error) {
	cmd := runner.Command{Name: "powershell.exe", Args: []string{script}, Dir: dir}
	f.calls = append(f.calls, cmd)
	if f.onRun != nil {
		return f.onRun(cmd)
	}
	return runner.Result{}, nil
}

func (f *fakeRunner) DryRun() bool  { return f.dryRun }
func (f *fakeRunner) Verbose() bool { return false }

func TestSyncPackagesPriorityAndPrune(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "apps", "scoop", "current"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "apps", "nodejs-lts", "current"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "apps", "stale-app", "current"), 0o755)

	fr := &fakeRunner{}
	mgr := NewManager(root, fr, &bytes.Buffer{})

	pkgs := []config.PackageConfig{
		{Name: "neovim"},
		{Name: "aria2"},
		{Name: "nodejs-lts"},
		{Name: "git"},
		{Name: "7zip"},
	}

	rep, err := mgr.SyncPackages(context.Background(), pkgs, true)
	if err != nil {
		t.Fatalf("SyncPackages failed: %v", err)
	}

	wantOrder := []string{"git", "7zip", "aria2", "neovim"}
	if strings.Join(rep.Installed, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("expected install order %v, got %v", wantOrder, rep.Installed)
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0] != "nodejs-lts" {
		t.Fatalf("expected nodejs-lts skipped, got %v", rep.Skipped)
	}
	if len(rep.Pruned) != 1 || rep.Pruned[0] != "stale-app" {
		t.Fatalf("expected stale-app pruned, got %v", rep.Pruned)
	}
}

func TestUpdateDetectsFileLock(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "apps", "neovim", "current"), 0o755)

	fr := &fakeRunner{
		onRun: func(cmd runner.Command) (runner.Result, error) {
			if len(cmd.Args) == 2 && cmd.Args[0] == "update" && cmd.Args[1] == "neovim" {
				return runner.Result{Stderr: "The process cannot access the file because it is being used by another process."}, errors.New("exit 1")
			}
			return runner.Result{}, nil
		},
	}
	mgr := NewManager(root, fr, &bytes.Buffer{})
	rep, err := mgr.Update(context.Background(), []config.PackageConfig{{Name: "neovim"}}, false)
	if err != nil {
		t.Fatalf("Update returned unexpected top-level err: %v", err)
	}
	if len(rep.LockedApps) != 1 || rep.LockedApps[0] != "neovim" {
		t.Fatalf("expected neovim in LockedApps, got %v", rep.LockedApps)
	}
}

func TestCleanKeepVersionsAndCache(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "apps", "neovim")
	v1 := filepath.Join(appDir, "0.9.0")
	v2 := filepath.Join(appDir, "0.10.0")
	v3 := filepath.Join(appDir, "0.11.0")
	for _, d := range []string{v1, v2, v3} {
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, "nvim.bin"), make([]byte, 1024), 0o644)
	}
	now := time.Now()
	_ = os.Chtimes(v1, now.Add(-3*time.Hour), now.Add(-3*time.Hour))
	_ = os.Chtimes(v2, now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	_ = os.Chtimes(v3, now.Add(-1*time.Hour), now.Add(-1*time.Hour))
	_ = syswin.CreateJunction(filepath.Join(appDir, "current"), v3)

	cacheDir := filepath.Join(root, "cache")
	_ = os.MkdirAll(cacheDir, 0o755)
	_ = os.WriteFile(filepath.Join(cacheDir, "pkg.zip"), make([]byte, 2048), 0o644)

	fr := &fakeRunner{}
	mgr := NewManager(root, fr, &bytes.Buffer{})

	rep, err := mgr.Clean(context.Background(), 2, false)
	if err != nil {
		t.Fatalf("Clean failed: %v", err)
	}
	if len(rep.RemovedVersions) != 1 || rep.RemovedVersions[0] != "neovim@0.9.0" {
		t.Fatalf("expected only neovim@0.9.0 removed, got %v", rep.RemovedVersions)
	}
	if rep.VersionsBytes != 1024 || rep.CacheBytes != 2048 {
		t.Fatalf("unexpected reclaimed bytes: versions=%d cache=%d", rep.VersionsBytes, rep.CacheBytes)
	}
	if _, err := os.Stat(v1); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be deleted", v1)
	}
	if _, err := os.Stat(v2); err != nil {
		t.Fatalf("expected %s to be kept", v2)
	}
}
