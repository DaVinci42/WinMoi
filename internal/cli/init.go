package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/davinci42/WinMoi/internal/config"
	"github.com/davinci42/WinMoi/internal/exitcode"
	"github.com/davinci42/WinMoi/internal/scoop"
	"github.com/davinci42/WinMoi/templates"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

func newInitCmd(g *GlobalOptions) *cobra.Command {
	var (
		fromCurrent bool
		withSkill   bool
		dir         string
		force       bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate a manifest.toml template (optionally from current Scoop or with maintenance skill)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if dir == "" {
				dir = "."
			}
			absDir, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			manifestPath := filepath.Join(absDir, "manifest.toml")
			if !force {
				if _, err := os.Stat(manifestPath); err == nil {
					return exitcode.Configf("%s already exists (use --force to overwrite)", manifestPath)
				}
			}

			var content string
			if fromCurrent {
				scoopRoot := os.Getenv("SCOOP")
				if scoopRoot == "" {
					if home, herr := os.UserHomeDir(); herr == nil {
						scoopRoot = filepath.Join(home, "scoop")
					}
				}
				mgr := scoop.NewManager(scoopRoot, g.newRunner(cmd), out)
				buckets, pkgs, err := mgr.ExportCurrent()
				if err != nil {
					return fmt.Errorf("export current scoop state: %w", err)
				}
				validate := true
				m := config.Manifest{
					Scoop: config.ScoopConfig{
						Root:    scoopRoot,
						Buckets: buckets,
					},
					Env: map[string]string{
						"XDG_CONFIG_HOME": "${USERPROFILE}/.config",
					},
					ConfigRepo: config.ConfigRepoConfig{
						LocalPath: ".",
						Branch:    "main",
					},
					Packages: pkgs,
					Skills: config.SkillsConfig{
						LocalDir:  "agent/skills",
						Validate:  &validate,
						Tool:      "npx skills",
						ExtraArgs: []string{"-g", "-y"},
					},
					Clean: config.CleanConfig{
						KeepVersions: 2,
					},
				}
				raw, err := toml.Marshal(m)
				if err != nil {
					return err
				}
				content = string(raw)
			} else {
				content = templates.ManifestTemplate
			}

			if g.DryRun {
				fmt.Fprintf(out, "[dry-run] write %s\n", manifestPath)
			} else {
				if err := os.MkdirAll(absDir, 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
					return err
				}
				fmt.Fprintf(out, "created %s\n", manifestPath)
			}

			if withSkill {
				skillPath := filepath.Join(absDir, "agent", "skills", "winmoi-maintenance", "SKILL.md")
				if g.DryRun {
					fmt.Fprintf(out, "[dry-run] write %s\n", skillPath)
				} else {
					if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
						return err
					}
					if err := os.WriteFile(skillPath, []byte(templates.MaintenanceSkillTemplate), 0o644); err != nil {
						return err
					}
					fmt.Fprintf(out, "created %s\n", skillPath)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&fromCurrent, "from-current", false, "Export currently installed Scoop buckets and packages")
	cmd.Flags().BoolVar(&withSkill, "with-skill", false, "Generate built-in repository maintenance skill under skills/")
	cmd.Flags().StringVar(&dir, "dir", ".", "Target directory for generated files")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing manifest.toml")

	return cmd
}
