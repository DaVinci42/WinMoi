package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/davinci42/WinMoi/internal/exitcode"
)

func TestLoadExpandAndProfile(t *testing.T) {
	dir := t.TempDir()

	baseManifest := `
[scoop]
root = "D:\\Scoop"

[[scoop.buckets]]
name = "main"

[[scoop.buckets]]
name = "extras"
profiles = ["work"]

[env]
XDG_CONFIG_HOME = "${USERPROFILE}/.config"
CUSTOM_VAR = "hello"

[config_repo]
local_path = "."
branch = "main"

[[links]]
from = "pwsh/profile.ps1"
to = "${USERPROFILE}/Documents/PowerShell/profile.ps1"
type = "symlink"

[[packages]]
name = "git"
links = [
  { from = "git/.gitconfig", to = "~/.gitconfig", type = "symlink" },
]

[[packages]]
name = "neovim"
profiles = ["dev"]
links = [
  { from = "nvim", to = "${SCOOP_PERSIST}/nvim", type = "junction" },
]

[clean]
keep_versions = 3
`
	manifestPath := filepath.Join(dir, "manifest.toml")
	if err := os.WriteFile(manifestPath, []byte(baseManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeHome := filepath.Join(dir, "home")
	loaded, err := Load(LoadOptions{
		ManifestPath: manifestPath,
		Profile:      "dev",
		EnvLookup: func(k string) string {
			if k == "USERPROFILE" || k == "HOME" {
				return fakeHome
			}
			return ""
		},
	})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.ScoopRoot != filepath.Clean(`D:\Scoop`) {
		t.Fatalf("expected overridden scoop root, got %q", loaded.ScoopRoot)
	}
	if loaded.Manifest.Env["CUSTOM_VAR"] != "hello" {
		t.Fatalf("expected local env merge, got %+v", loaded.Manifest.Env)
	}
	if loaded.Manifest.Env["XDG_CONFIG_HOME"] != fakeHome+"/.config" {
		t.Fatalf("unexpected XDG_CONFIG_HOME: %q", loaded.Manifest.Env["XDG_CONFIG_HOME"])
	}
	if len(loaded.Manifest.Scoop.Buckets) != 1 || loaded.Manifest.Scoop.Buckets[0].Name != "main" {
		t.Fatalf("expected profile 'dev' to filter out 'work' bucket, got %+v", loaded.Manifest.Scoop.Buckets)
	}
	if len(loaded.Manifest.Packages) != 2 {
		t.Fatalf("expected 2 packages for profile 'dev', got %d", len(loaded.Manifest.Packages))
	}
	if loaded.Manifest.Packages[0].Links[0].To != filepath.Join(fakeHome, ".gitconfig") {
		t.Fatalf("expected ~ expansion, got %q", loaded.Manifest.Packages[0].Links[0].To)
	}
	if loaded.Manifest.Clean.KeepVersions != 3 {
		t.Fatalf("expected keep_versions=3, got %d", loaded.Manifest.Clean.KeepVersions)
	}
}

func TestValidateRejectsDuplicateTargetAndBadType(t *testing.T) {
	dir := t.TempDir()
	bad := `
[[links]]
from = "a"
to = "~/same"
type = "invalid"
`
	p := filepath.Join(dir, "manifest.toml")
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(LoadOptions{ManifestPath: p})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if exitcode.FromError(err) != exitcode.CodeConfig {
		t.Fatalf("expected exit code %d, got %d", exitcode.CodeConfig, exitcode.FromError(err))
	}
}
