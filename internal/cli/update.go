package cli

import (
	"fmt"
	"strings"

	"github.com/davinci42/WinMoi/internal/exitcode"
	"github.com/davinci42/WinMoi/internal/linker"
	"github.com/davinci42/WinMoi/internal/scoop"
	"github.com/davinci42/WinMoi/internal/skills"
	"github.com/davinci42/WinMoi/internal/state"
	"github.com/spf13/cobra"
)

func newUpdateCmd(g *GlobalOptions) *cobra.Command {
	var (
		only       string
		updateAll  bool
		doClean    bool
		keep       int
		onConflict string
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update Scoop packages, config_repo, links, and upstream skills",
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch only {
			case "", "pkg", "skills":
			default:
				return exitcode.Configf("invalid --only value %q (expected pkg|skills)", only)
			}

			cfg, cleanup, err := g.loadConfig()
			if err != nil {
				return err
			}
			defer cleanup()

			return g.withLock(func() error {
				ctx := cmd.Context()
				out := cmd.OutOrStdout()
				r := g.newRunner(cmd)
				st, err := state.Load(g.statePath())
				if err != nil {
					return err
				}

				mgr := scoop.NewManager(cfg.ScoopRoot, r, out)
				var failures []string

				if only == "" || only == "pkg" {
					uRep, err := mgr.Update(ctx, cfg.Manifest.Packages, updateAll)
					if err != nil {
						failures = append(failures, fmt.Sprintf("scoop update: %v", err))
					}
					for k, ferr := range uRep.Failed {
						failures = append(failures, fmt.Sprintf("update %s: %v", k, ferr))
					}
					fmt.Fprintf(out, "packages updated: %d, failed: %d\n", len(uRep.Updated), len(uRep.Failed))
					if len(uRep.LockedApps) > 0 {
						fmt.Fprintf(cmd.ErrOrStderr(),
							"warning: the following apps appear locked by running processes: %s (close them and retry)\n",
							strings.Join(uRep.LockedApps, ", "))
					}
				}

				if only == "" {
					if _, err := linker.SyncConfigRepo(ctx, cfg, r, true, out); err != nil {
						failures = append(failures, fmt.Sprintf("config_repo: %v", err))
					}
					lRep := linker.ApplyLinks(cfg.AllLinks(), st, linker.ApplyOptions{
						OnConflict: onConflict,
						DryRun:     g.DryRun,
						Out:        out,
					})
					for k, ferr := range lRep.Failed {
						failures = append(failures, fmt.Sprintf("link %s: %v", k, ferr))
					}
					fmt.Fprintf(out, "links refreshed: %d created, %d skipped, %d failed\n",
						len(lRep.Created), len(lRep.Skipped), len(lRep.Failed))
				}

				if only == "" || only == "skills" {
					sRep := skills.Update(ctx, cfg, st, r, skills.ApplyOptions{
						OnConflict: onConflict,
						DryRun:     g.DryRun,
						Out:        out,
					})
					for k, ferr := range sRep.Failed {
						failures = append(failures, fmt.Sprintf("skills %s: %v", k, ferr))
					}
					if sRep.UpstreamOutput != "" && !g.Verbose {
						fmt.Fprint(out, sRep.UpstreamOutput)
					}
				}

				if doClean {
					keepN := keep
					if keepN <= 0 {
						keepN = cfg.Manifest.Clean.KeepVersions
					}
					cRep, err := mgr.Clean(ctx, keepN, false)
					if err != nil {
						failures = append(failures, fmt.Sprintf("clean: %v", err))
					}
					for k, ferr := range cRep.Failed {
						failures = append(failures, fmt.Sprintf("clean %s: %v", k, ferr))
					}
					fmt.Fprintf(out, "cleaned: %d old versions (%s), %d cache files (%s)\n",
						len(cRep.RemovedVersions), scoop.FormatBytes(cRep.VersionsBytes),
						cRep.CacheFiles, scoop.FormatBytes(cRep.CacheBytes))
				}

				if !g.DryRun {
					if err := state.Save(g.statePath(), st); err != nil {
						return err
					}
				}

				if len(failures) > 0 {
					for _, f := range failures {
						fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", f)
					}
					return exitcode.Partialf("%d operation(s) failed during update", len(failures))
				}
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&only, "only", "", "Limit scope: pkg|skills")
	cmd.Flags().BoolVar(&updateAll, "all", false, "Update all installed Scoop packages instead of manifest packages only")
	cmd.Flags().BoolVar(&doClean, "clean", false, "Run scoop clean after updating")
	cmd.Flags().IntVar(&keep, "keep", 0, "Override clean.keep_versions when --clean is set")
	cmd.Flags().StringVar(&onConflict, "on-conflict", linker.ConflictBackup, "Conflict strategy: backup|skip|overwrite")

	return cmd
}
