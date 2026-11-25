package scoop

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/davinci42/WinMoi/internal/config"
	"github.com/davinci42/WinMoi/internal/runner"
	"github.com/davinci42/WinMoi/internal/syswin"
)

type Manager struct {
	ScoopRoot string
	Runner    runner.Runner
	Out       io.Writer
}

func NewManager(scoopRoot string, r runner.Runner, out io.Writer) *Manager {
	if out == nil {
		out = io.Discard
	}
	return &Manager{
		ScoopRoot: scoopRoot,
		Runner:    r,
		Out:       out,
	}
}

func (m *Manager) EnsureEnv(envVars map[string]string) error {
	changed := false
	if m.ScoopRoot != "" {
		existing, ok, err := syswin.GetUserEnv("SCOOP")
		if err != nil {
			return fmt.Errorf("read user env SCOOP: %w", err)
		}
		if ok && existing != "" && config.NormalizePathKey(existing) != config.NormalizePathKey(m.ScoopRoot) {
			return fmt.Errorf("existing SCOOP=%q conflicts with manifest scoop.root=%q; update manifest.toml or unset SCOOP first", existing, m.ScoopRoot)
		}
		if !ok || config.NormalizePathKey(existing) != config.NormalizePathKey(m.ScoopRoot) {
			if m.Runner.DryRun() {
				fmt.Fprintf(m.Out, "[dry-run] set HKCU\\Environment SCOOP=%s\n", m.ScoopRoot)
			} else {
				if err := syswin.SetUserEnv("SCOOP", m.ScoopRoot); err != nil {
					return fmt.Errorf("set user env SCOOP: %w", err)
				}
				changed = true
			}
		}
	}

	keys := make([]string, 0, len(envVars))
	for k := range envVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		val := envVars[k]
		existing, ok, err := syswin.GetUserEnv(k)
		if err != nil {
			return fmt.Errorf("read user env %s: %w", k, err)
		}
		if ok && existing == val {
			continue
		}
		if m.Runner.DryRun() {
			fmt.Fprintf(m.Out, "[dry-run] set HKCU\\Environment %s=%s\n", k, val)
			continue
		}
		if err := syswin.SetUserEnv(k, val); err != nil {
			return fmt.Errorf("set user env %s: %w", k, err)
		}
		changed = true
	}

	if changed {
		if err := syswin.BroadcastEnvChange(); err != nil {
			fmt.Fprintf(m.Out, "warning: broadcast WM_SETTINGCHANGE failed: %v\n", err)
		}
	}
	return nil
}

func (m *Manager) IsInstalled() bool {
	if m.ScoopRoot == "" {
		return false
	}
	candidates := []string{
		filepath.Join(m.ScoopRoot, "shims", "scoop.ps1"),
		filepath.Join(m.ScoopRoot, "shims", "scoop.cmd"),
		filepath.Join(m.ScoopRoot, "apps", "scoop", "current"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func (m *Manager) RequireInstalled() error {
	if m.IsInstalled() {
		return nil
	}
	if m.Runner.DryRun() {
		fmt.Fprintf(m.Out, "[dry-run] warning: Scoop not found at %s\n", m.ScoopRoot)
		return nil
	}
	return fmt.Errorf("Scoop not found at %s; install and configure Scoop first", m.ScoopRoot)
}

func (m *Manager) scoopBin() string {
	if m.ScoopRoot != "" {
		cmdPath := filepath.Join(m.ScoopRoot, "shims", "scoop.cmd")
		if _, err := os.Stat(cmdPath); err == nil {
			return cmdPath
		}
	}
	return "scoop"
}

func (m *Manager) runScoop(ctx context.Context, args ...string) (runner.Result, error) {
	return m.Runner.Run(ctx, runner.Command{
		Name: m.scoopBin(),
		Args: args,
		Env:  []string{"SCOOP=" + m.ScoopRoot},
	})
}

func (m *Manager) InstalledBuckets() (map[string]struct{}, error) {
	out := make(map[string]struct{})
	dir := filepath.Join(m.ScoopRoot, "buckets")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			out[strings.ToLower(e.Name())] = struct{}{}
		}
	}
	return out, nil
}

func (m *Manager) InstalledApps() (map[string]struct{}, error) {
	out := make(map[string]struct{})
	dir := filepath.Join(m.ScoopRoot, "apps")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if name == "scoop" {
			continue
		}
		out[name] = struct{}{}
	}
	return out, nil
}

type BucketSyncReport struct {
	Added   []string
	Skipped []string
	Failed  map[string]error
}

func (m *Manager) SyncBuckets(ctx context.Context, buckets []config.BucketConfig) (BucketSyncReport, error) {
	report := BucketSyncReport{
		Failed: make(map[string]error),
	}
	installed, err := m.InstalledBuckets()
	if err != nil {
		return report, err
	}

	needsGit := false
	for _, b := range buckets {
		if _, ok := installed[strings.ToLower(b.Name)]; !ok && !strings.EqualFold(b.Name, "main") {
			needsGit = true
			break
		}
	}
	if needsGit {
		apps, _ := m.InstalledApps()
		if _, hasGitApp := apps["git"]; !hasGitApp {
			if _, err := exec.LookPath("git"); err != nil {
				fmt.Fprintf(m.Out, "installing git prerequisite for non-main buckets...\n")
				if _, ierr := m.runScoop(ctx, "install", "git"); ierr != nil {
					return report, fmt.Errorf("install git prerequisite: %w", ierr)
				}
			}
		}
	}

	for _, b := range buckets {
		if _, ok := installed[strings.ToLower(b.Name)]; ok {
			report.Skipped = append(report.Skipped, b.Name)
			continue
		}
		args := []string{"bucket", "add", b.Name}
		if b.URL != "" {
			args = append(args, b.URL)
		}
		if _, err := m.runScoop(ctx, args...); err != nil {
			report.Failed[b.Name] = err
			continue
		}
		report.Added = append(report.Added, b.Name)
	}
	return report, nil
}

type PkgSyncReport struct {
	Installed []string
	Skipped   []string
	Pruned    []string
	Unmanaged []string
	Failed    map[string]error
}

func (m *Manager) SyncPackages(ctx context.Context, pkgs []config.PackageConfig, prune bool) (PkgSyncReport, error) {
	report := PkgSyncReport{
		Failed: make(map[string]error),
	}
	installed, err := m.InstalledApps()
	if err != nil {
		return report, err
	}

	ordered := prioritizeBasePackages(pkgs)
	declared := make(map[string]struct{}, len(ordered))
	for _, p := range ordered {
		appKey := strings.ToLower(p.AppName())
		declared[appKey] = struct{}{}
		if _, ok := installed[appKey]; ok {
			report.Skipped = append(report.Skipped, p.Name)
			continue
		}
		if _, err := m.runScoop(ctx, "install", p.Name); err != nil {
			report.Failed[p.Name] = err
			continue
		}
		report.Installed = append(report.Installed, p.Name)
	}

	unmanaged := make([]string, 0)
	for app := range installed {
		if _, ok := declared[app]; !ok {
			unmanaged = append(unmanaged, app)
		}
	}
	sort.Strings(unmanaged)
	report.Unmanaged = unmanaged

	if prune {
		for _, app := range unmanaged {
			if _, err := m.runScoop(ctx, "uninstall", app); err != nil {
				report.Failed["uninstall:"+app] = err
				continue
			}
			report.Pruned = append(report.Pruned, app)
		}
	}
	return report, nil
}

func prioritizeBasePackages(pkgs []config.PackageConfig) []config.PackageConfig {
	priority := map[string]int{
		"git":   1,
		"7zip":  2,
		"aria2": 3,
	}
	out := make([]config.PackageConfig, len(pkgs))
	copy(out, pkgs)
	sort.SliceStable(out, func(i, j int) bool {
		pi := priority[strings.ToLower(out[i].AppName())]
		pj := priority[strings.ToLower(out[j].AppName())]
		if pi == 0 && pj == 0 {
			return false
		}
		if pi == 0 {
			return false
		}
		if pj == 0 {
			return true
		}
		return pi < pj
	})
	return out
}

type UpdateReport struct {
	Updated    []string
	LockedApps []string
	Failed     map[string]error
}

func (m *Manager) Update(ctx context.Context, pkgs []config.PackageConfig, updateAll bool) (UpdateReport, error) {
	report := UpdateReport{
		Failed: make(map[string]error),
	}
	if _, err := m.runScoop(ctx, "update"); err != nil {
		return report, fmt.Errorf("scoop update index: %w", err)
	}

	if updateAll {
		res, err := m.runScoop(ctx, "update", "*")
		if err != nil {
			if looksLikeFileLock(res.Stdout + "\n" + res.Stderr) {
				report.LockedApps = append(report.LockedApps, "*")
			}
			report.Failed["*"] = err
			return report, nil
		}
		report.Updated = append(report.Updated, "*")
		return report, nil
	}

	installed, err := m.InstalledApps()
	if err != nil {
		return report, err
	}
	for _, p := range pkgs {
		app := p.AppName()
		if _, ok := installed[strings.ToLower(app)]; !ok {
			continue
		}
		res, err := m.runScoop(ctx, "update", app)
		if err != nil {
			if looksLikeFileLock(res.Stdout + "\n" + res.Stderr) {
				report.LockedApps = append(report.LockedApps, app)
			}
			report.Failed[app] = err
			continue
		}
		report.Updated = append(report.Updated, app)
	}
	return report, nil
}

func looksLikeFileLock(output string) bool {
	lower := strings.ToLower(output)
	patterns := []string{
		"being used by another process",
		"used by another process",
		"cannot access the file",
		"access to the path",
		"is in use",
		"file is locked",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

type CleanReport struct {
	RemovedVersions []string
	VersionsBytes   int64
	CacheFiles      int
	CacheBytes      int64
	Failed          map[string]error
}

func (m *Manager) Clean(ctx context.Context, keepVersions int, cacheOnly bool) (CleanReport, error) {
	report := CleanReport{
		Failed: make(map[string]error),
	}
	if keepVersions < 1 {
		keepVersions = 1
	}

	cacheDir := filepath.Join(m.ScoopRoot, "cache")
	if entries, err := os.ReadDir(cacheDir); err == nil {
		for _, e := range entries {
			p := filepath.Join(cacheDir, e.Name())
			sz, _ := dirSize(p)
			report.CacheBytes += sz
			report.CacheFiles++
		}
		if report.CacheFiles > 0 {
			if _, err := m.runScoop(ctx, "cache", "rm", "*"); err != nil {
				report.Failed["cache"] = err
			}
		}
	}

	if cacheOnly {
		return report, nil
	}

	appsDir := filepath.Join(m.ScoopRoot, "apps")
	apps, err := os.ReadDir(appsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return report, nil
		}
		return report, err
	}

	type verEntry struct {
		name    string
		path    string
		modTime time.Time
	}

	for _, app := range apps {
		if !app.IsDir() {
			continue
		}
		appPath := filepath.Join(appsDir, app.Name())
		currentTarget := ""
		if target, err := syswin.ReadLinkTarget(filepath.Join(appPath, "current")); err == nil {
			currentTarget = config.NormalizePathKey(target)
		}

		subEntries, err := os.ReadDir(appPath)
		if err != nil {
			continue
		}
		var versions []verEntry
		for _, sub := range subEntries {
			if !sub.IsDir() || strings.EqualFold(sub.Name(), "current") {
				continue
			}
			full := filepath.Join(appPath, sub.Name())
			info, err := sub.Info()
			if err != nil {
				continue
			}
			versions = append(versions, verEntry{
				name:    sub.Name(),
				path:    full,
				modTime: info.ModTime(),
			})
		}
		if len(versions) <= keepVersions {
			continue
		}

		sort.Slice(versions, func(i, j int) bool {
			if !versions[i].modTime.Equal(versions[j].modTime) {
				return versions[i].modTime.After(versions[j].modTime)
			}
			return versions[i].name > versions[j].name
		})

		kept := 0
		for _, v := range versions {
			isCurrent := currentTarget != "" && config.NormalizePathKey(v.path) == currentTarget
			if isCurrent || kept < keepVersions {
				kept++
				continue
			}
			sz, _ := dirSize(v.path)
			report.VersionsBytes += sz
			label := app.Name() + "@" + v.name
			if m.Runner.DryRun() {
				fmt.Fprintf(m.Out, "[dry-run] remove old version %s (%s)\n", label, FormatBytes(sz))
				report.RemovedVersions = append(report.RemovedVersions, label)
				continue
			}
			if err := os.RemoveAll(v.path); err != nil {
				report.Failed[label] = err
				continue
			}
			report.RemovedVersions = append(report.RemovedVersions, label)
		}
	}
	return report, nil
}

func (m *Manager) ExportCurrent() ([]config.BucketConfig, []config.PackageConfig, error) {
	bucketSet, err := m.InstalledBuckets()
	if err != nil {
		return nil, nil, err
	}
	bucketNames := make([]string, 0, len(bucketSet))
	for b := range bucketSet {
		bucketNames = append(bucketNames, b)
	}
	sort.Strings(bucketNames)

	knownOfficial := map[string]bool{
		"main": true, "extras": true, "versions": true, "nirsoft": true,
		"sysinternals": true, "php": true, "nerd-fonts": true, "nonportable": true,
		"java": true, "games": true,
	}

	buckets := make([]config.BucketConfig, 0, len(bucketNames))
	for _, b := range bucketNames {
		bc := config.BucketConfig{Name: b}
		if !knownOfficial[b] {
			bc.URL = readGitOriginURL(filepath.Join(m.ScoopRoot, "buckets", b))
		}
		buckets = append(buckets, bc)
	}

	appSet, err := m.InstalledApps()
	if err != nil {
		return nil, nil, err
	}
	appNames := make([]string, 0, len(appSet))
	for a := range appSet {
		appNames = append(appNames, a)
	}
	sort.Strings(appNames)

	pkgs := make([]config.PackageConfig, 0, len(appNames))
	for _, a := range appNames {
		pkgs = append(pkgs, config.PackageConfig{Name: a})
	}
	return buckets, pkgs, nil
}

func readGitOriginURL(repoDir string) string {
	raw, err := os.ReadFile(filepath.Join(repoDir, ".git", "config"))
	if err != nil {
		return ""
	}
	inOrigin := false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inOrigin = strings.EqualFold(trimmed, `[remote "origin"]`)
			continue
		}
		if inOrigin && strings.HasPrefix(trimmed, "url") {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func dirSize(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, err
}

func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
