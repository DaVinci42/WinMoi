package skills

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/davinci42/WinMoi/internal/config"
	"github.com/davinci42/WinMoi/internal/linker"
	"github.com/davinci42/WinMoi/internal/runner"
	"github.com/davinci42/WinMoi/internal/state"
)

func TargetDir(home string) string {
	return filepath.Join(home, ".agents", "skills")
}

type SkillMeta struct {
	Name        string
	Description string
	Dir         string
}

func ValidateSkillMD(skillDir string) (SkillMeta, error) {
	skillFile := filepath.Join(skillDir, "SKILL.md")
	raw, err := os.ReadFile(skillFile)
	if err != nil {
		return SkillMeta{}, fmt.Errorf("missing SKILL.md in %s: %w", skillDir, err)
	}
	content := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return SkillMeta{}, fmt.Errorf("%s: missing YAML frontmatter", skillFile)
	}
	rest := content[4:]
	endIdx := strings.Index(rest, "\n---")
	if endIdx < 0 {
		return SkillMeta{}, fmt.Errorf("%s: unterminated YAML frontmatter", skillFile)
	}
	fm := rest[:endIdx]

	var meta SkillMeta
	meta.Dir = skillDir
	for _, line := range strings.Split(fm, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		switch k {
		case "name":
			meta.Name = v
		case "description":
			meta.Description = v
		}
	}
	if meta.Name == "" {
		return meta, fmt.Errorf("%s: frontmatter missing 'name'", skillFile)
	}
	if meta.Description == "" {
		return meta, fmt.Errorf("%s: frontmatter missing 'description'", skillFile)
	}
	return meta, nil
}

type ApplyOptions struct {
	TargetDir  string
	OnConflict string
	DryRun     bool
	Prune      bool
	LocalOnly  bool
	Out        io.Writer
	Now        func() time.Time
}

type ApplyReport struct {
	LocalLinked     []string
	LocalSkipped    []string
	LocalPruned     []string
	UpstreamAdded   []string
	UpstreamSkipped []string
	UpstreamPruned  []string
	Warnings        []string
	Failed          map[string]error
}

func Apply(ctx context.Context, cfg *config.LoadedConfig, st *state.State, r runner.Runner, opts ApplyOptions) ApplyReport {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.TargetDir == "" {
		opts.TargetDir = TargetDir(cfg.Vars["USERPROFILE"])
	}

	report := ApplyReport{
		Failed: make(map[string]error),
	}

	localNames := make(map[string]struct{})
	activeLocalKeys := make(map[string]struct{})

	if cfg.Manifest.Skills.LocalDir != "" {
		localRoot := cfg.ResolveSource(cfg.Manifest.Skills.LocalDir)
		entries, err := os.ReadDir(localRoot)
		if err != nil {
			if !os.IsNotExist(err) {
				report.Failed["local_dir"] = err
			}
		} else {
			if !opts.DryRun {
				_ = os.MkdirAll(opts.TargetDir, 0o755)
			}
			for _, e := range entries {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
					continue
				}
				srcDir := filepath.Join(localRoot, e.Name())
				if cfg.Manifest.Skills.ShouldValidate() {
					if _, verr := ValidateSkillMD(srcDir); verr != nil {
						report.Failed["local:"+e.Name()] = verr
						continue
					}
				}
				dstDir := filepath.Join(opts.TargetDir, e.Name())
				localNames[strings.ToLower(e.Name())] = struct{}{}
				activeLocalKeys[config.NormalizePathKey(dstDir)] = struct{}{}

				spec := config.ResolvedLink{
					From:     srcDir,
					To:       dstDir,
					Type:     config.LinkTypeJunction,
					Category: "skill-local",
					Owner:    e.Name(),
				}
				created, _, lerr := linker.ApplySingleLink(spec, st, linker.ApplyOptions{
					OnConflict: opts.OnConflict,
					DryRun:     opts.DryRun,
					Out:        opts.Out,
					Now:        opts.Now,
				})
				if lerr != nil {
					report.Failed["local:"+e.Name()] = lerr
					continue
				}
				if created {
					report.LocalLinked = append(report.LocalLinked, e.Name())
				} else {
					report.LocalSkipped = append(report.LocalSkipped, e.Name())
				}
			}
		}
	}

	if opts.Prune {
		for key, rec := range st.Links {
			if rec.Category != "skill-local" {
				continue
			}
			if _, ok := activeLocalKeys[key]; ok {
				continue
			}
			if opts.DryRun {
				fmt.Fprintf(opts.Out, "[dry-run] prune local skill %s\n", rec.Target)
				report.LocalPruned = append(report.LocalPruned, rec.Owner)
				continue
			}
			if err := os.Remove(rec.Target); err != nil && !os.IsNotExist(err) {
				report.Failed["prune:"+rec.Owner] = err
				continue
			}
			delete(st.Links, key)
			report.LocalPruned = append(report.LocalPruned, rec.Owner)
		}
	}

	if opts.LocalOnly {
		return report
	}

	declaredSpecs := make(map[string]struct{}, len(cfg.Manifest.Skills.Upstream))
	toolBin, toolBaseArgs := runner.SplitToolCommand(cfg.Manifest.Skills.Tool)
	if len(cfg.Manifest.Skills.Upstream) > 0 && toolBin == "" {
		report.Failed["skills.tool"] = fmt.Errorf("skills.upstream is configured but skills.tool is empty")
		return report
	}

	for _, spec := range cfg.Manifest.Skills.Upstream {
		declaredSpecs[spec] = struct{}{}

		if rec, exists := st.UpstreamSkills[spec]; exists && allDirsExist(opts.TargetDir, rec.Dirs) {
			report.UpstreamSkipped = append(report.UpstreamSkipped, spec)
			continue
		}

		before := listDirNames(opts.TargetDir)
		args := make([]string, 0, len(toolBaseArgs)+2+len(cfg.Manifest.Skills.ExtraArgs))
		args = append(args, toolBaseArgs...)
		args = append(args, "add", spec)
		args = append(args, cfg.Manifest.Skills.ExtraArgs...)

		if _, err := r.Run(ctx, runner.Command{
			Name: toolBin,
			Args: args,
		}); err != nil {
			report.Failed["upstream:"+spec] = err
			continue
		}

		if opts.DryRun {
			report.UpstreamAdded = append(report.UpstreamAdded, spec)
			continue
		}

		after := listDirNames(opts.TargetDir)
		added := diffDirNames(before, after)
		var kept []string
		for _, d := range added {
			if _, conflict := localNames[strings.ToLower(d)]; conflict {
				w := fmt.Sprintf("upstream skill %q conflicts with local private skill %q; keeping local skill", spec, d)
				report.Warnings = append(report.Warnings, w)
				fmt.Fprintf(opts.Out, "warning: %s\n", w)
				continue
			}
			kept = append(kept, d)
		}
		st.UpstreamSkills[spec] = state.UpstreamSkillRecord{
			Spec:      spec,
			Dirs:      kept,
			UpdatedAt: opts.Now(),
		}
		report.UpstreamAdded = append(report.UpstreamAdded, spec)
	}

	if opts.Prune {
		retainedDirs := make(map[string]struct{})
		for spec, rec := range st.UpstreamSkills {
			if _, ok := declaredSpecs[spec]; ok {
				for _, d := range rec.Dirs {
					retainedDirs[strings.ToLower(d)] = struct{}{}
				}
			}
		}
		for spec, rec := range st.UpstreamSkills {
			if _, ok := declaredSpecs[spec]; ok {
				continue
			}
			for _, d := range rec.Dirs {
				low := strings.ToLower(d)
				if _, isLocal := localNames[low]; isLocal {
					continue
				}
				if _, stillNeeded := retainedDirs[low]; stillNeeded {
					continue
				}
				full := filepath.Join(opts.TargetDir, d)
				if opts.DryRun {
					fmt.Fprintf(opts.Out, "[dry-run] prune upstream skill dir %s (from %s)\n", full, spec)
					continue
				}
				if err := os.RemoveAll(full); err != nil && !os.IsNotExist(err) {
					report.Failed["prune-upstream:"+d] = err
				}
			}
			if !opts.DryRun {
				delete(st.UpstreamSkills, spec)
			}
			report.UpstreamPruned = append(report.UpstreamPruned, spec)
		}
	}

	return report
}

type UpdateReport struct {
	RepoResult     linker.RepoSyncResult
	LocalReport    ApplyReport
	UpstreamOutput string
	Failed         map[string]error
}

func Update(ctx context.Context, cfg *config.LoadedConfig, st *state.State, r runner.Runner, opts ApplyOptions) UpdateReport {
	report := UpdateReport{
		Failed: make(map[string]error),
	}

	repoRes, err := linker.SyncConfigRepo(ctx, cfg, r, true, opts.Out)
	report.RepoResult = repoRes
	if err != nil {
		report.Failed["config_repo"] = err
	}

	localOpts := opts
	localOpts.LocalOnly = true
	report.LocalReport = Apply(ctx, cfg, st, r, localOpts)
	for k, v := range report.LocalReport.Failed {
		report.Failed[k] = v
	}

	if len(cfg.Manifest.Skills.Upstream) == 0 || cfg.Manifest.Skills.Tool == "" {
		return report
	}

	toolBin, toolBaseArgs := runner.SplitToolCommand(cfg.Manifest.Skills.Tool)
	subcmd := "update"
	if opts.DryRun {
		subcmd = "check"
	}
	args := append(append([]string{}, toolBaseArgs...), subcmd)
	res, err := r.Run(ctx, runner.Command{
		Name: toolBin,
		Args: args,
	})
	report.UpstreamOutput = res.Stdout
	if err != nil {
		report.Failed["upstream_update"] = err
	}
	return report
}

func listDirNames(dir string) map[string]struct{} {
	out := make(map[string]struct{})
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		out[e.Name()] = struct{}{}
	}
	return out
}

func diffDirNames(before, after map[string]struct{}) []string {
	var added []string
	for name := range after {
		if _, existed := before[name]; !existed {
			added = append(added, name)
		}
	}
	sort.Strings(added)
	return added
}

func allDirsExist(base string, dirs []string) bool {
	if len(dirs) == 0 {
		return false
	}
	for _, d := range dirs {
		if _, err := os.Stat(filepath.Join(base, d)); err != nil {
			return false
		}
	}
	return true
}
