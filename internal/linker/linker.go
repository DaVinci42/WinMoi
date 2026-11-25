package linker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/davinci42/WinMoi/internal/config"
	"github.com/davinci42/WinMoi/internal/runner"
	"github.com/davinci42/WinMoi/internal/state"
	"github.com/davinci42/WinMoi/internal/syswin"
)

const (
	ConflictBackup    = "backup"
	ConflictSkip      = "skip"
	ConflictOverwrite = "overwrite"
)

type RepoSyncResult struct {
	Cloned  bool
	Pulled  bool
	Dirty   bool
	Skipped bool
}

func SyncConfigRepo(ctx context.Context, cfg *config.LoadedConfig, r runner.Runner, pull bool, out io.Writer) (RepoSyncResult, error) {
	if out == nil {
		out = io.Discard
	}
	var res RepoSyncResult
	repoDir := cfg.ConfigRepoDir
	repoURL := cfg.Manifest.ConfigRepo.URL
	branch := cfg.Manifest.ConfigRepo.Branch
	if branch == "" {
		branch = "main"
	}

	_, statErr := os.Stat(repoDir)
	if os.IsNotExist(statErr) {
		if repoURL == "" {
			return res, fmt.Errorf("config_repo local_path %s does not exist and no url is configured", repoDir)
		}
		if _, err := r.Run(ctx, runner.Command{
			Name: "git",
			Args: []string{"clone", "--branch", branch, repoURL, repoDir},
		}); err != nil {
			return res, fmt.Errorf("clone config_repo: %w", err)
		}
		res.Cloned = true
		return res, nil
	}
	if statErr != nil {
		return res, statErr
	}

	if !pull {
		res.Skipped = true
		return res, nil
	}

	if _, err := os.Stat(filepath.Join(repoDir, ".git")); os.IsNotExist(err) {
		res.Skipped = true
		return res, nil
	}

	if r.DryRun() {
		fmt.Fprintf(out, "[dry-run] git -C %s pull --ff-only\n", repoDir)
		res.Pulled = true
		return res, nil
	}

	statusRes, err := r.Run(ctx, runner.Command{
		Name: "git",
		Args: []string{"status", "--porcelain"},
		Dir:  repoDir,
	})
	if err != nil {
		return res, fmt.Errorf("check config_repo git status: %w", err)
	}
	if strings.TrimSpace(statusRes.Stdout) != "" {
		res.Dirty = true
		fmt.Fprintf(out, "warning: config_repo %s has uncommitted changes; skipping git pull\n", repoDir)
		return res, nil
	}

	if _, err := r.Run(ctx, runner.Command{
		Name: "git",
		Args: []string{"pull", "--ff-only"},
		Dir:  repoDir,
	}); err != nil {
		return res, fmt.Errorf("git pull --ff-only in %s: %w", repoDir, err)
	}
	res.Pulled = true
	return res, nil
}

type ApplyOptions struct {
	OnConflict string
	DryRun     bool
	Prune      bool
	Out        io.Writer
	Now        func() time.Time
}

type ApplyReport struct {
	Created  []string
	Skipped  []string
	BackedUp map[string]string
	Pruned   []string
	Failed   map[string]error
}

func ApplyLinks(links []config.ResolvedLink, st *state.State, opts ApplyOptions) ApplyReport {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.OnConflict == "" {
		opts.OnConflict = ConflictBackup
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	report := ApplyReport{
		BackedUp: make(map[string]string),
		Failed:   make(map[string]error),
	}

	activeKeys := make(map[string]struct{}, len(links))
	for _, spec := range links {
		key := config.NormalizePathKey(spec.To)
		activeKeys[key] = struct{}{}

		created, backupPath, err := ApplySingleLink(spec, st, opts)
		if err != nil {
			report.Failed[spec.To] = err
			continue
		}
		if backupPath != "" {
			report.BackedUp[spec.To] = backupPath
		}
		if created {
			report.Created = append(report.Created, spec.To)
		} else {
			report.Skipped = append(report.Skipped, spec.To)
		}
	}

	if opts.Prune {
		for key, rec := range st.Links {
			if rec.Category != "config" && rec.Category != "package" {
				continue
			}
			if _, ok := activeKeys[key]; ok {
				continue
			}
			if opts.DryRun {
				fmt.Fprintf(opts.Out, "[dry-run] prune link %s\n", rec.Target)
				report.Pruned = append(report.Pruned, rec.Target)
				continue
			}
			if err := removeManagedTarget(rec.Target, rec.Type); err != nil && !os.IsNotExist(err) {
				report.Failed[rec.Target] = err
				continue
			}
			delete(st.Links, key)
			report.Pruned = append(report.Pruned, rec.Target)
		}
	}

	return report
}

func ApplySingleLink(spec config.ResolvedLink, st *state.State, opts ApplyOptions) (bool, string, error) {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.OnConflict == "" {
		opts.OnConflict = ConflictBackup
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	srcAbs, err := filepath.Abs(spec.From)
	if err != nil {
		return false, "", fmt.Errorf("resolve source %s: %w", spec.From, err)
	}
	dstAbs, err := filepath.Abs(spec.To)
	if err != nil {
		return false, "", fmt.Errorf("resolve target %s: %w", spec.To, err)
	}

	srcInfo, err := os.Stat(srcAbs)
	if err != nil {
		if !os.IsNotExist(err) || !spec.Adopt {
			return false, "", fmt.Errorf("source %s not found: %w", srcAbs, err)
		}
		if dstInfo, derr := os.Lstat(dstAbs); derr == nil {
			isLnk, _, _ := syswin.IsLink(dstAbs)
			if isLnk {
				return false, "", fmt.Errorf("cannot adopt %s because target is already a link", dstAbs)
			}
			if opts.DryRun {
				fmt.Fprintf(opts.Out, "[dry-run] adopt %s -> %s and link back\n", dstAbs, srcAbs)
				return true, "", nil
			}
			if err := os.MkdirAll(filepath.Dir(srcAbs), 0o755); err != nil {
				return false, "", err
			}
			if err := movePath(dstAbs, srcAbs); err != nil {
				return false, "", fmt.Errorf("adopt %s -> %s: %w", dstAbs, srcAbs, err)
			}
			srcInfo = dstInfo
		} else if os.IsNotExist(derr) {
			if opts.DryRun {
				fmt.Fprintf(opts.Out, "[dry-run] create %s and link -> %s\n", srcAbs, dstAbs)
				return true, "", nil
			}
			if err := os.MkdirAll(srcAbs, 0o755); err != nil {
				return false, "", fmt.Errorf("create config dir %s: %w", srcAbs, err)
			}
			srcInfo, err = os.Stat(srcAbs)
			if err != nil {
				return false, "", err
			}
		} else {
			return false, "", derr
		}
	}

	linkType := spec.Type
	if linkType == config.LinkTypeAuto {
		if srcInfo.IsDir() {
			linkType = config.LinkTypeJunction
		} else {
			linkType = config.LinkTypeSymlink
		}
	}

	key := config.NormalizePathKey(dstAbs)

	if isLink, _, lerr := syswin.IsLink(dstAbs); lerr == nil && isLink {
		if target, rerr := syswin.ReadLinkTarget(dstAbs); rerr == nil {
			targetAbs := target
			if !filepath.IsAbs(targetAbs) {
				targetAbs = filepath.Join(filepath.Dir(dstAbs), targetAbs)
			}
			if config.NormalizePathKey(targetAbs) == config.NormalizePathKey(srcAbs) {
				if _, exists := st.Links[key]; !exists && !opts.DryRun {
					st.Links[key] = state.LinkRecord{
						Target:    dstAbs,
						Source:    srcAbs,
						Type:      linkType,
						Category:  spec.Category,
						Owner:     spec.Owner,
						UpdatedAt: opts.Now(),
					}
				}
				return false, "", nil
			}
		}
	}

	var backupPath string
	if _, statErr := os.Lstat(dstAbs); statErr == nil {
		switch opts.OnConflict {
		case ConflictSkip:
			fmt.Fprintf(opts.Out, "skipping conflict at %s\n", dstAbs)
			return false, "", nil
		case ConflictBackup:
			backupPath = fmt.Sprintf("%s.bak.%s", dstAbs, opts.Now().Format("20060102-150405"))
			if opts.DryRun {
				fmt.Fprintf(opts.Out, "[dry-run] backup %s -> %s\n", dstAbs, backupPath)
			} else {
				if err := os.Rename(dstAbs, backupPath); err != nil {
					return false, "", fmt.Errorf("backup %s: %w", dstAbs, err)
				}
			}
		case ConflictOverwrite:
			if opts.DryRun {
				fmt.Fprintf(opts.Out, "[dry-run] remove existing %s\n", dstAbs)
			} else {
				if err := removePathSafe(dstAbs); err != nil {
					return false, "", fmt.Errorf("remove existing %s: %w", dstAbs, err)
				}
			}
		default:
			return false, "", fmt.Errorf("unknown conflict policy %q", opts.OnConflict)
		}
	}

	if opts.DryRun {
		fmt.Fprintf(opts.Out, "[dry-run] link (%s) %s -> %s\n", linkType, dstAbs, srcAbs)
		return true, backupPath, nil
	}

	actualType, err := createLinkWithFallback(linkType, spec.Fallback, dstAbs, srcAbs, srcInfo.IsDir())
	if err != nil {
		return false, backupPath, err
	}

	prevBackup := backupPath
	if prevBackup == "" {
		if prev, ok := st.Links[key]; ok {
			prevBackup = prev.Backup
		}
	}
	st.Links[key] = state.LinkRecord{
		Target:    dstAbs,
		Source:    srcAbs,
		Type:      actualType,
		Backup:    prevBackup,
		Category:  spec.Category,
		Owner:     spec.Owner,
		UpdatedAt: opts.Now(),
	}
	return true, backupPath, nil
}

func createLinkWithFallback(linkType, fallback, dst, src string, isDir bool) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	switch linkType {
	case config.LinkTypeJunction:
		if !isDir {
			return "", fmt.Errorf("junction requires directory source: %s", src)
		}
		return config.LinkTypeJunction, syswin.CreateJunction(dst, src)
	case config.LinkTypeSymlink:
		err := syswin.CreateSymlink(dst, src)
		if err == nil {
			return config.LinkTypeSymlink, nil
		}
		if errors.Is(err, syswin.ErrSymlinkPrivilege) {
			switch fallback {
			case config.FallbackHardlink:
				if !isDir && syswin.IsSameVolume(dst, src) {
					if herr := syswin.CreateHardlink(dst, src); herr == nil {
						return config.LinkTypeHardlink, nil
					}
				}
			case config.FallbackCopy:
				if cerr := syswin.CopyPath(src, dst); cerr == nil {
					return config.LinkTypeCopy, nil
				}
			}
		}
		return "", err
	case config.LinkTypeHardlink:
		if isDir {
			return "", fmt.Errorf("hardlink cannot target directory: %s", src)
		}
		if !syswin.IsSameVolume(dst, src) {
			return "", fmt.Errorf("hardlink requires same volume: %s and %s", dst, src)
		}
		return config.LinkTypeHardlink, syswin.CreateHardlink(dst, src)
	case config.LinkTypeCopy:
		return config.LinkTypeCopy, syswin.CopyPath(src, dst)
	default:
		return "", fmt.Errorf("unsupported link type %q", linkType)
	}
}

func removePathSafe(path string) error {
	if isLink, _, err := syswin.IsLink(path); err == nil && isLink {
		return syswin.RemoveLink(path)
	}
	return os.RemoveAll(path)
}

func movePath(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Fallback for cross-volume moves (e.g. C:\Users\... -> D:\dotfiles\configs\...).
	if err := syswin.CopyPath(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func removeManagedTarget(path, linkType string) error {
	if linkType == config.LinkTypeCopy {
		return os.RemoveAll(path)
	}
	if isLink, _, err := syswin.IsLink(path); err == nil && isLink {
		return syswin.RemoveLink(path)
	}
	return os.Remove(path)
}

type UnlinkOptions struct {
	Restore bool
	Only    string
	DryRun  bool
	Out     io.Writer
}

type UnlinkReport struct {
	Removed  []string
	Restored []string
	Failed   map[string]error
}

func Unlink(st *state.State, opts UnlinkOptions) UnlinkReport {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	report := UnlinkReport{
		Failed: make(map[string]error),
	}

	for key, rec := range st.Links {
		switch opts.Only {
		case "link":
			if rec.Category != "config" && rec.Category != "package" {
				continue
			}
		case "skills":
			if rec.Category != "skill-local" {
				continue
			}
		}

		if opts.DryRun {
			fmt.Fprintf(opts.Out, "[dry-run] unlink %s\n", rec.Target)
			report.Removed = append(report.Removed, rec.Target)
			if opts.Restore && rec.Backup != "" {
				fmt.Fprintf(opts.Out, "[dry-run] restore %s -> %s\n", rec.Backup, rec.Target)
				report.Restored = append(report.Restored, rec.Target)
			}
			continue
		}

		if err := removeManagedTarget(rec.Target, rec.Type); err != nil && !os.IsNotExist(err) {
			report.Failed[rec.Target] = err
			continue
		}
		report.Removed = append(report.Removed, rec.Target)

		if opts.Restore && rec.Backup != "" {
			if _, err := os.Stat(rec.Backup); err == nil {
				if err := os.Rename(rec.Backup, rec.Target); err != nil {
					report.Failed["restore:"+rec.Target] = err
				} else {
					report.Restored = append(report.Restored, rec.Target)
				}
			}
		}
		delete(st.Links, key)
	}
	return report
}
