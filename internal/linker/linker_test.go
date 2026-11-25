package linker

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/davinci42/WinMoi/internal/config"
	"github.com/davinci42/WinMoi/internal/state"
)

func TestApplyLinksBackupIdempotentPruneAndRestore(t *testing.T) {
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "repo", ".gitconfig")
	srcDir := filepath.Join(dir, "repo", "nvim")
	_ = os.MkdirAll(filepath.Dir(srcFile), 0o755)
	_ = os.MkdirAll(srcDir, 0o755)
	_ = os.WriteFile(srcFile, []byte("[user]\nname=new\n"), 0o644)
	_ = os.WriteFile(filepath.Join(srcDir, "init.lua"), []byte("-- nvim"), 0o644)

	dstFile := filepath.Join(dir, "home", ".gitconfig")
	dstDir := filepath.Join(dir, "home", "nvim")
	_ = os.MkdirAll(filepath.Dir(dstFile), 0o755)
	_ = os.WriteFile(dstFile, []byte("[user]\nname=old\n"), 0o644)

	st := state.New()
	fixedNow := func() time.Time { return time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC) }
	links := []config.ResolvedLink{
		{From: srcFile, To: dstFile, Type: config.LinkTypeSymlink, Category: "package", Owner: "git"},
		{From: srcDir, To: dstDir, Type: config.LinkTypeJunction, Category: "package", Owner: "neovim"},
	}

	rep := ApplyLinks(links, st, ApplyOptions{
		OnConflict: ConflictBackup,
		Out:        &bytes.Buffer{},
		Now:        fixedNow,
	})
	if len(rep.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", rep.Failed)
	}
	if len(rep.Created) != 2 || len(rep.BackedUp) != 1 {
		t.Fatalf("expected 2 created and 1 backup, got %+v", rep)
	}

	// Second apply should be idempotent.
	rep2 := ApplyLinks(links, st, ApplyOptions{
		OnConflict: ConflictBackup,
		Out:        &bytes.Buffer{},
		Now:        fixedNow,
	})
	if len(rep2.Created) != 0 || len(rep2.Skipped) != 2 {
		t.Fatalf("expected idempotent skip, got %+v", rep2)
	}

	// Prune when nvim link is removed.
	rep3 := ApplyLinks(links[:1], st, ApplyOptions{
		OnConflict: ConflictBackup,
		Prune:      true,
		Out:        &bytes.Buffer{},
		Now:        fixedNow,
	})
	if len(rep3.Pruned) != 1 || rep3.Pruned[0] != dstDir {
		t.Fatalf("expected %s pruned, got %+v", dstDir, rep3.Pruned)
	}

	// Unlink with --restore should bring back original .gitconfig.
	uRep := Unlink(st, UnlinkOptions{Restore: true, Out: &bytes.Buffer{}})
	if len(uRep.Restored) != 1 {
		t.Fatalf("expected 1 restored, got %+v", uRep)
	}
	restoredBytes, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(restoredBytes) != "[user]\nname=old\n" {
		t.Fatalf("unexpected restored content: %q", string(restoredBytes))
	}
}

func TestApplySingleLinkAdoptAndBidirectionalSync(t *testing.T) {
	dir := t.TempDir()
	repoPkgCfg := filepath.Join(dir, "repo", "configs", "neovim")
	targetDir := filepath.Join(dir, "home", "AppData", "Local", "nvim")
	_ = os.MkdirAll(targetDir, 0o755)
	_ = os.WriteFile(filepath.Join(targetDir, "init.lua"), []byte("-- existing"), 0o644)

	st := state.New()
	spec := config.ResolvedLink{
		From:     repoPkgCfg,
		To:       targetDir,
		Category: "package",
		Owner:    "neovim",
		Adopt:    true,
	}

	created, _, err := ApplySingleLink(spec, st, ApplyOptions{Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("ApplySingleLink adopt failed: %v", err)
	}
	if !created {
		t.Fatal("expected link to be created")
	}

	// Existing file should now live in configs/neovim.
	got, err := os.ReadFile(filepath.Join(repoPkgCfg, "init.lua"))
	if err != nil || string(got) != "-- existing" {
		t.Fatalf("expected adopted file in repo, got %q (err=%v)", string(got), err)
	}

	// Editing via target reflects immediately in repo (bidirectional link).
	if err := os.WriteFile(filepath.Join(targetDir, "init.lua"), []byte("-- edited-at-target"), 0o644); err != nil {
		t.Fatal(err)
	}
	got2, err := os.ReadFile(filepath.Join(repoPkgCfg, "init.lua"))
	if err != nil || string(got2) != "-- edited-at-target" {
		t.Fatalf("expected bidirectional write visibility, got %q (err=%v)", string(got2), err)
	}
}
