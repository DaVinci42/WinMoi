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

func newApplyCmd(g *GlobalOptions) *cobra.Command {
	var (
		only       string
		prune      bool
		onConflict string
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Idempotently converge Scoop, Env, Buckets, Packages, Links, and Skills",
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch only {
			case "", "scoop", "pkg", "link", "skills":
			default:
				return exitcode.Configf("invalid --only value %q (expected scoop|pkg|link|skills)", only)
			}
			switch onConflict {
			case linker.ConflictBackup, linker.ConflictSkip, linker.ConflictOverwrite:
			default:
				return exitcode.Configf("invalid --on-conflict value %q (expected backup|skip|overwrite)", onConflict)
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

				runScoop := only == "" || only == "scoop"
				runPkg := only == "" || only == "pkg"
				runLink := only == "" || only == "link"
				runSkills := only == "" || only == "skills"

				if runScoop {
					if err := mgr.EnsureEnv(cfg.Manifest.Env); err != nil {
						return err
					}
					if err := mgr.RequireInstalled(); err != nil {
						return err
					}
					bRep, err := mgr.SyncBuckets(ctx, cfg.Manifest.Scoop.Buckets)
					if err != nil {
						failures = append(failures, fmt.Sprintf("buckets: %v", err))
					}
					for name, ferr := range bRep.Failed {
						failures = append(failures, fmt.Sprintf("bucket %s: %v", name, ferr))
					}
					fmt.Fprintf(out, "buckets: %d added, %d skipped, %d failed\n", len(bRep.Added), len(bRep.Skipped), len(bRep.Failed))
				}

				if runPkg {
					pRep, err := mgr.SyncPackages(ctx, cfg.Manifest.Packages, prune)
					if err != nil {
						failures = append(failures, fmt.Sprintf("packages: %v", err))
					}
					for name, ferr := range pRep.Failed {
						failures = append(failures, fmt.Sprintf("package %s: %v", name, ferr))
					}
					fmt.Fprintf(out, "packages: %d installed, %d skipped, %d pruned, %d failed\n",
						len(pRep.Installed), len(pRep.Skipped), len(pRep.Pruned), len(pRep.Failed))
					if len(pRep.Unmanaged) > 0 && !prune {
						fmt.Fprintf(out, "unmanaged packages (%d): %s (pass --prune to uninstall)\n",
							len(pRep.Unmanaged), strings.Join(pRep.Unmanaged, ", "))
					}
				}

				if runLink || runSkills {
					if _, err := linker.SyncConfigRepo(ctx, cfg, r, false, out); err != nil {
						failures = append(failures, fmt.Sprintf("config_repo: %v", err))
					}
				}

				if runLink {
					lRep := linker.ApplyLinks(cfg.AllLinks(), st, linker.ApplyOptions{
						OnConflict: onConflict,
						DryRun:     g.DryRun,
						Prune:      prune,
						Out:        out,
					})
					for target, ferr := range lRep.Failed {
						failures = append(failures, fmt.Sprintf("link %s: %v", target, ferr))
					}
					fmt.Fprintf(out, "links: %d created, %d skipped, %d backed up, %d pruned, %d failed\n",
						len(lRep.Created), len(lRep.Skipped), len(lRep.BackedUp), len(lRep.Pruned), len(lRep.Failed))
				}

				if runSkills {
					sRep := skills.Apply(ctx, cfg, st, r, skills.ApplyOptions{
						OnConflict: onConflict,
						DryRun:     g.DryRun,
						Prune:      prune,
						Out:        out,
					})
					for k, ferr := range sRep.Failed {
						failures = append(failures, fmt.Sprintf("skills %s: %v", k, ferr))
					}
					fmt.Fprintf(out, "skills: local(%d linked, %d skipped, %d pruned) upstream(%d added, %d skipped, %d pruned) failed(%d)\n",
						len(sRep.LocalLinked), len(sRep.LocalSkipped), len(sRep.LocalPruned),
						len(sRep.UpstreamAdded), len(sRep.UpstreamSkipped), len(sRep.UpstreamPruned),
						len(sRep.Failed))
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
					return exitcode.Partialf("%d operation(s) failed during apply", len(failures))
				}
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&only, "only", "", "Limit scope: scoop|pkg|link|skills")
	cmd.Flags().BoolVar(&prune, "prune", false, "Remove unmanaged packages, links, or skills")
	cmd.Flags().StringVar(&onConflict, "on-conflict", linker.ConflictBackup, "Conflict strategy: backup|skip|overwrite")

	return cmd
}
