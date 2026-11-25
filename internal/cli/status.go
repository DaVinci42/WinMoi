package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/davinci42/WinMoi/internal/config"
	"github.com/davinci42/WinMoi/internal/runner"
	"github.com/davinci42/WinMoi/internal/scoop"
	"github.com/davinci42/WinMoi/internal/skills"
	"github.com/davinci42/WinMoi/internal/state"
	"github.com/davinci42/WinMoi/internal/syswin"
	"github.com/spf13/cobra"
)

type StatusReport struct {
	Scoop struct {
		Root             string   `json:"root"`
		Installed        bool     `json:"installed"`
		BucketsInstalled []string `json:"buckets_installed"`
		BucketsMissing   []string `json:"buckets_missing"`
	} `json:"scoop"`
	Packages struct {
		Installed []string `json:"installed"`
		Missing   []string `json:"missing"`
		Unmanaged []string `json:"unmanaged"`
	} `json:"packages"`
	Links struct {
		OK           []string `json:"ok"`
		Missing      []string `json:"missing"`
		Drifted      []string `json:"drifted"`
		StaleManaged []string `json:"stale_managed"`
	} `json:"links"`
	Skills struct {
		TargetDir         string   `json:"target_dir"`
		LocalOK           []string `json:"local_ok"`
		LocalMissing      []string `json:"local_missing"`
		LocalDrifted      []string `json:"local_drifted"`
		UpstreamInstalled []string `json:"upstream_installed"`
		UpstreamMissing   []string `json:"upstream_missing"`
		UpstreamStale     []string `json:"upstream_stale"`
	} `json:"skills"`
	Issues []string `json:"issues,omitempty"`
}

func newStatusCmd(g *GlobalOptions) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show diff between local machine state and manifest.toml, including health checks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, cleanup, err := g.loadConfig()
			if err != nil {
				return err
			}
			defer cleanup()

			st, err := state.Load(g.statePath())
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			mgr := scoop.NewManager(cfg.ScoopRoot, g.newRunner(cmd), out)

			var rep StatusReport
			rep.Scoop.Root = cfg.ScoopRoot
			rep.Scoop.Installed = mgr.IsInstalled()

			installedBuckets, _ := mgr.InstalledBuckets()
			for _, b := range cfg.Manifest.Scoop.Buckets {
				if _, ok := installedBuckets[strings.ToLower(b.Name)]; ok {
					rep.Scoop.BucketsInstalled = append(rep.Scoop.BucketsInstalled, b.Name)
				} else {
					rep.Scoop.BucketsMissing = append(rep.Scoop.BucketsMissing, b.Name)
				}
			}

			installedApps, _ := mgr.InstalledApps()
			declaredApps := make(map[string]struct{}, len(cfg.Manifest.Packages))
			for _, p := range cfg.Manifest.Packages {
				key := strings.ToLower(p.AppName())
				declaredApps[key] = struct{}{}
				if _, ok := installedApps[key]; ok {
					rep.Packages.Installed = append(rep.Packages.Installed, p.Name)
				} else {
					rep.Packages.Missing = append(rep.Packages.Missing, p.Name)
				}
			}
			for app := range installedApps {
				if _, ok := declaredApps[app]; !ok {
					rep.Packages.Unmanaged = append(rep.Packages.Unmanaged, app)
				}
			}
			sort.Strings(rep.Packages.Unmanaged)

			activeLinks := make(map[string]struct{})
			for _, l := range cfg.AllLinks() {
				key := config.NormalizePathKey(l.To)
				activeLinks[key] = struct{}{}
				switch classifyLink(l.From, l.To) {
				case "ok":
					rep.Links.OK = append(rep.Links.OK, l.To)
				case "missing":
					rep.Links.Missing = append(rep.Links.Missing, l.To)
				default:
					rep.Links.Drifted = append(rep.Links.Drifted, l.To)
				}
			}
			for key, rec := range st.Links {
				if rec.Category != "config" && rec.Category != "package" {
					continue
				}
				if _, ok := activeLinks[key]; !ok {
					rep.Links.StaleManaged = append(rep.Links.StaleManaged, rec.Target)
				}
			}
			sort.Strings(rep.Links.StaleManaged)

			skillsTarget := skills.TargetDir(cfg.Vars["USERPROFILE"])
			rep.Skills.TargetDir = skillsTarget
			if cfg.Manifest.Skills.LocalDir != "" {
				localRoot := cfg.ResolveSource(cfg.Manifest.Skills.LocalDir)
				if entries, err := os.ReadDir(localRoot); err == nil {
					for _, e := range entries {
						if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
							continue
						}
						src := filepath.Join(localRoot, e.Name())
						dst := filepath.Join(skillsTarget, e.Name())
						switch classifyLink(src, dst) {
						case "ok":
							rep.Skills.LocalOK = append(rep.Skills.LocalOK, e.Name())
						case "missing":
							rep.Skills.LocalMissing = append(rep.Skills.LocalMissing, e.Name())
						default:
							rep.Skills.LocalDrifted = append(rep.Skills.LocalDrifted, e.Name())
						}
					}
				}
			}

			declaredUpstream := make(map[string]struct{}, len(cfg.Manifest.Skills.Upstream))
			for _, spec := range cfg.Manifest.Skills.Upstream {
				declaredUpstream[spec] = struct{}{}
				if rec, ok := st.UpstreamSkills[spec]; ok && len(rec.Dirs) > 0 {
					allOK := true
					for _, d := range rec.Dirs {
						if _, serr := os.Stat(filepath.Join(skillsTarget, d)); serr != nil {
							allOK = false
							break
						}
					}
					if allOK {
						rep.Skills.UpstreamInstalled = append(rep.Skills.UpstreamInstalled, spec)
					} else {
						rep.Skills.UpstreamMissing = append(rep.Skills.UpstreamMissing, spec)
					}
				} else {
					rep.Skills.UpstreamMissing = append(rep.Skills.UpstreamMissing, spec)
				}
			}
			for spec := range st.UpstreamSkills {
				if _, ok := declaredUpstream[spec]; !ok {
					rep.Skills.UpstreamStale = append(rep.Skills.UpstreamStale, spec)
				}
			}
			sort.Strings(rep.Skills.UpstreamStale)

			rep.Issues = collectHealthIssues(cfg, st, rep.Scoop.Installed)

			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(rep)
			}

			fmt.Fprintf(out, "Scoop (%s): installed=%v\n", rep.Scoop.Root, rep.Scoop.Installed)
			fmt.Fprintf(out, "  buckets: %d installed, %d missing\n", len(rep.Scoop.BucketsInstalled), len(rep.Scoop.BucketsMissing))
			if len(rep.Scoop.BucketsMissing) > 0 {
				fmt.Fprintf(out, "    missing: %s\n", strings.Join(rep.Scoop.BucketsMissing, ", "))
			}
			fmt.Fprintf(out, "Packages: %d installed, %d missing, %d unmanaged\n",
				len(rep.Packages.Installed), len(rep.Packages.Missing), len(rep.Packages.Unmanaged))
			if len(rep.Packages.Missing) > 0 {
				fmt.Fprintf(out, "  missing:   %s\n", strings.Join(rep.Packages.Missing, ", "))
			}
			if len(rep.Packages.Unmanaged) > 0 {
				fmt.Fprintf(out, "  unmanaged: %s\n", strings.Join(rep.Packages.Unmanaged, ", "))
			}
			fmt.Fprintf(out, "Links: %d ok, %d missing, %d drifted, %d stale\n",
				len(rep.Links.OK), len(rep.Links.Missing), len(rep.Links.Drifted), len(rep.Links.StaleManaged))
			fmt.Fprintf(out, "Skills (%s):\n", rep.Skills.TargetDir)
			fmt.Fprintf(out, "  local:    %d ok, %d missing, %d drifted\n",
				len(rep.Skills.LocalOK), len(rep.Skills.LocalMissing), len(rep.Skills.LocalDrifted))
			fmt.Fprintf(out, "  upstream: %d installed, %d missing, %d stale\n",
				len(rep.Skills.UpstreamInstalled), len(rep.Skills.UpstreamMissing), len(rep.Skills.UpstreamStale))
			if len(rep.Issues) > 0 {
				fmt.Fprintf(out, "Issues (%d):\n", len(rep.Issues))
				for _, issue := range rep.Issues {
					fmt.Fprintf(out, "  - %s\n", issue)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Output status as JSON")
	return cmd
}

func collectHealthIssues(cfg *config.LoadedConfig, st *state.State, scoopInstalled bool) []string {
	var issues []string

	if !scoopInstalled {
		issues = append(issues, fmt.Sprintf("Scoop not found at %s", cfg.ScoopRoot))
	}

	if envScoop, ok, err := syswin.GetUserEnv("SCOOP"); err == nil && ok && envScoop != "" {
		if config.NormalizePathKey(envScoop) != config.NormalizePathKey(cfg.ScoopRoot) {
			issues = append(issues, fmt.Sprintf("SCOOP env mismatch: %s vs manifest %s", envScoop, cfg.ScoopRoot))
		}
	}

	if devMode, err := syswin.IsDeveloperModeEnabled(); err == nil && !devMode {
		issues = append(issues, "Windows Developer Mode is disabled (file symlinks require Developer Mode or fallback=hardlink/copy)")
	}

	for _, rec := range st.Links {
		if _, err := os.Stat(rec.Target); err != nil {
			issues = append(issues, fmt.Sprintf("broken managed link: %s", rec.Target))
		}
	}

	if cfg.Manifest.Skills.LocalDir != "" && cfg.Manifest.Skills.ShouldValidate() {
		localRoot := cfg.ResolveSource(cfg.Manifest.Skills.LocalDir)
		if entries, err := os.ReadDir(localRoot); err == nil {
			for _, e := range entries {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
					continue
				}
				if _, verr := skills.ValidateSkillMD(filepath.Join(localRoot, e.Name())); verr != nil {
					issues = append(issues, fmt.Sprintf("invalid SKILL.md (%s): %v", e.Name(), verr))
				}
			}
		}
	}

	if cfg.Manifest.Skills.Tool != "" && len(cfg.Manifest.Skills.Upstream) > 0 {
		bin, _ := runner.SplitToolCommand(cfg.Manifest.Skills.Tool)
		if _, err := exec.LookPath(bin); err != nil {
			issues = append(issues, fmt.Sprintf("skills tool %q not found in PATH", bin))
		}
	}

	return issues
}

func classifyLink(from, to string) string {
	srcAbs, err := filepath.Abs(from)
	if err != nil {
		return "drifted"
	}
	if _, err := os.Lstat(to); os.IsNotExist(err) {
		return "missing"
	}
	isLink, _, err := syswin.IsLink(to)
	if err != nil || !isLink {
		return "drifted"
	}
	target, err := syswin.ReadLinkTarget(to)
	if err != nil {
		return "drifted"
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(to), target)
	}
	if config.NormalizePathKey(target) == config.NormalizePathKey(srcAbs) {
		return "ok"
	}
	return "drifted"
}
