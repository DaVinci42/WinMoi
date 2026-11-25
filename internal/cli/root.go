package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/davinci42/WinMoi/internal/config"
	"github.com/davinci42/WinMoi/internal/exitcode"
	"github.com/davinci42/WinMoi/internal/runner"
	"github.com/davinci42/WinMoi/internal/state"
	"github.com/davinci42/WinMoi/internal/syswin"
	"github.com/spf13/cobra"
)

var Version = "dev"

type GlobalOptions struct {
	ConfigPath string
	Profile    string
	Verbose    bool
	DryRun     bool
	StatePath  string
	LockPath   string
}

func NewRootCmd() *cobra.Command {
	opts := &GlobalOptions{}

	cmd := &cobra.Command{
		Use:           "winmoi",
		Short:         "Windows user-level declarative Scoop, config links, and Agent Skills CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}

	pf := cmd.PersistentFlags()
	pf.StringVarP(&opts.ConfigPath, "config", "c", "manifest.toml", "Path or URL to manifest.toml")
	pf.StringVar(&opts.Profile, "profile", "", "Filter packages, buckets, and links by profile tag")
	pf.BoolVarP(&opts.Verbose, "verbose", "v", false, "Stream child process output in real time")
	pf.BoolVar(&opts.DryRun, "dry-run", false, "Preview actions without making changes")
	pf.StringVar(&opts.StatePath, "state", "", "Override state.json path")
	pf.StringVar(&opts.LockPath, "lock", "", "Override lock file path")

	cmd.AddCommand(
		newInitCmd(opts),
		newApplyCmd(opts),
		newUpdateCmd(opts),
		newStatusCmd(opts),
	)

	return cmd
}

func (g *GlobalOptions) loadConfig() (*config.LoadedConfig, func(), error) {
	path := g.ConfigPath
	cleanup := func() {}

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		tmpFile, err := downloadRemoteManifest(path)
		if err != nil {
			return nil, cleanup, exitcode.Config(err)
		}
		path = tmpFile
		cleanup = func() { _ = os.Remove(tmpFile) }
	}

	cfg, err := config.Load(config.LoadOptions{
		ManifestPath: path,
		Profile:      g.Profile,
	})
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return cfg, cleanup, nil
}

func downloadRemoteManifest(rawURL string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("fetch remote manifest %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch remote manifest %s: HTTP %d", rawURL, resp.StatusCode)
	}

	tmpDir, err := os.MkdirTemp("", "winmoi-manifest-*")
	if err != nil {
		return "", err
	}
	dest := filepath.Join(tmpDir, "manifest.toml")
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 4<<20)); err != nil {
		return "", err
	}
	return dest, nil
}

func (g *GlobalOptions) statePath() string {
	if g.StatePath != "" {
		return g.StatePath
	}
	return state.DefaultStatePath()
}

func (g *GlobalOptions) lockPath() string {
	if g.LockPath != "" {
		return g.LockPath
	}
	return state.DefaultLockPath()
}

func (g *GlobalOptions) withLock(fn func() error) error {
	if g.DryRun {
		return fn()
	}
	lock, err := syswin.AcquireLock(g.lockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	return fn()
}

func (g *GlobalOptions) newRunner(cmd *cobra.Command) *runner.ExecRunner {
	return runner.New(g.DryRun, g.Verbose, cmd.OutOrStdout(), cmd.ErrOrStderr())
}
