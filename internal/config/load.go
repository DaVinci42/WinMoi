package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/davinci42/WinMoi/internal/exitcode"
	toml "github.com/pelletier/go-toml/v2"
)

var varPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

type LoadOptions struct {
	ManifestPath string
	Profile      string
	EnvLookup    func(string) string
}

func Load(opts LoadOptions) (*LoadedConfig, error) {
	if opts.ManifestPath == "" {
		opts.ManifestPath = "manifest.toml"
	}
	absPath, err := filepath.Abs(opts.ManifestPath)
	if err != nil {
		return nil, exitcode.Configf("resolve manifest path: %w", err)
	}

	raw, err := os.ReadFile(absPath)
	if err != nil {
		return nil, exitcode.Configf("read manifest %s: %w", absPath, err)
	}

	var manifest Manifest
	if err := toml.Unmarshal(raw, &manifest); err != nil {
		return nil, exitcode.Configf("parse manifest %s: %w", absPath, err)
	}

	manifestDir := filepath.Dir(absPath)

	lookup := opts.EnvLookup
	if lookup == nil {
		lookup = os.Getenv
	}

	vars := buildBaseVars(lookup)

	if manifest.Scoop.Root == "" {
		if existing := lookup("SCOOP"); existing != "" {
			manifest.Scoop.Root = existing
		} else if home := vars["USERPROFILE"]; home != "" {
			manifest.Scoop.Root = filepath.Join(home, "scoop")
		}
	}
	scoopRoot := ExpandPath(manifest.Scoop.Root, vars)
	manifest.Scoop.Root = scoopRoot
	scoopPersist := ""
	if scoopRoot != "" {
		scoopPersist = filepath.Join(scoopRoot, "persist")
	}
	vars["SCOOP_ROOT"] = scoopRoot
	vars["SCOOP_PERSIST"] = scoopPersist
	vars["SCOOP"] = scoopRoot

	if len(manifest.Env) > 0 {
		expandedEnv := make(map[string]string, len(manifest.Env))
		for key, val := range manifest.Env {
			expanded := ExpandVars(val, vars)
			expandedEnv[key] = expanded
			vars[key] = expanded
		}
		manifest.Env = expandedEnv
	}

	manifest = filterProfile(manifest, opts.Profile)

	if manifest.Clean.KeepVersions == 0 {
		manifest.Clean.KeepVersions = 2
	}
	if manifest.ConfigRepo.Branch == "" {
		manifest.ConfigRepo.Branch = "main"
	}
	if manifest.ConfigRepo.ConfigsDir == "" {
		manifest.ConfigRepo.ConfigsDir = "configs"
	}

	configRepoDir := manifestDir
	if manifest.ConfigRepo.LocalPath != "" {
		lp := ExpandPath(manifest.ConfigRepo.LocalPath, vars)
		if filepath.IsAbs(lp) {
			configRepoDir = filepath.Clean(lp)
		} else {
			configRepoDir = filepath.Clean(filepath.Join(manifestDir, lp))
		}
	}

	for i := range manifest.Links {
		manifest.Links[i].To = ExpandPath(manifest.Links[i].To, vars)
	}
	for i := range manifest.Packages {
		if manifest.Packages[i].ConfigTo != "" {
			manifest.Packages[i].ConfigTo = ExpandPath(manifest.Packages[i].ConfigTo, vars)
		}
		for j := range manifest.Packages[i].Links {
			manifest.Packages[i].Links[j].To = ExpandPath(manifest.Packages[i].Links[j].To, vars)
		}
	}

	loaded := &LoadedConfig{
		ManifestPath:  absPath,
		ManifestDir:   manifestDir,
		ConfigRepoDir: configRepoDir,
		ScoopRoot:     scoopRoot,
		ScoopPersist:  scoopPersist,
		Manifest:      manifest,
		Vars:          vars,
	}

	if err := loaded.Validate(); err != nil {
		return nil, exitcode.Config(err)
	}
	return loaded, nil
}

func (c *LoadedConfig) AllLinks() []ResolvedLink {
	out := make([]ResolvedLink, 0, len(c.Manifest.Links))
	for _, l := range c.Manifest.Links {
		out = append(out, ResolvedLink{
			From:     c.ResolveSource(l.From),
			To:       l.To,
			Type:     l.Type,
			Fallback: l.Fallback,
			Category: "config",
			Owner:    "links",
		})
	}
	configsDir := c.Manifest.ConfigRepo.ConfigsDir
	if configsDir == "" {
		configsDir = "configs"
	}
	for _, p := range c.Manifest.Packages {
		if p.ConfigTo != "" {
			out = append(out, ResolvedLink{
				From:     c.ResolveSource(filepath.Join(configsDir, p.AppName())),
				To:       p.ConfigTo,
				Type:     p.ConfigType,
				Category: "package",
				Owner:    p.Name,
				Adopt:    true,
			})
		}
		for _, l := range p.Links {
			out = append(out, ResolvedLink{
				From:     c.ResolveSource(l.From),
				To:       l.To,
				Type:     l.Type,
				Fallback: l.Fallback,
				Category: "package",
				Owner:    p.Name,
			})
		}
	}
	return out
}

func (c *LoadedConfig) ResolveSource(from string) string {
	expanded := ExpandPath(from, c.Vars)
	if filepath.IsAbs(expanded) {
		return filepath.Clean(expanded)
	}
	return filepath.Clean(filepath.Join(c.ConfigRepoDir, expanded))
}

func (c *LoadedConfig) Validate() error {
	if c.Manifest.Clean.KeepVersions < 0 {
		return fmt.Errorf("clean.keep_versions must be >= 0, got %d", c.Manifest.Clean.KeepVersions)
	}

	seenBuckets := make(map[string]struct{}, len(c.Manifest.Scoop.Buckets))
	for _, b := range c.Manifest.Scoop.Buckets {
		if strings.TrimSpace(b.Name) == "" {
			return errors.New("scoop.buckets[].name must not be empty")
		}
		key := strings.ToLower(b.Name)
		if _, dup := seenBuckets[key]; dup {
			return fmt.Errorf("duplicate scoop bucket %q", b.Name)
		}
		seenBuckets[key] = struct{}{}
	}

	seenPkgs := make(map[string]struct{}, len(c.Manifest.Packages))
	for _, p := range c.Manifest.Packages {
		if strings.TrimSpace(p.Name) == "" {
			return errors.New("packages[].name must not be empty")
		}
		key := strings.ToLower(p.AppName())
		if _, dup := seenPkgs[key]; dup {
			return fmt.Errorf("duplicate package %q", p.Name)
		}
		seenPkgs[key] = struct{}{}
	}

	seenTargets := make(map[string]string)
	for _, l := range c.AllLinks() {
		if strings.TrimSpace(l.From) == "" {
			return fmt.Errorf("link in %s has empty 'from'", l.Owner)
		}
		if strings.TrimSpace(l.To) == "" {
			return fmt.Errorf("link in %s has empty 'to'", l.Owner)
		}
		switch l.Type {
		case LinkTypeAuto, LinkTypeSymlink, LinkTypeJunction, LinkTypeHardlink, LinkTypeCopy:
		default:
			return fmt.Errorf("invalid link type %q for target %s", l.Type, l.To)
		}
		switch l.Fallback {
		case "", FallbackError, FallbackHardlink, FallbackCopy:
		default:
			return fmt.Errorf("invalid link fallback %q for target %s", l.Fallback, l.To)
		}
		norm := NormalizePathKey(l.To)
		if prev, dup := seenTargets[norm]; dup {
			return fmt.Errorf("duplicate link target %s (declared by %s and %s)", l.To, prev, l.Owner)
		}
		seenTargets[norm] = l.Owner
	}

	return nil
}

func NormalizePathKey(p string) string {
	clean := filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}

func buildBaseVars(lookup func(string) string) map[string]string {
	vars := make(map[string]string)
	home := lookup("USERPROFILE")
	if home == "" {
		home = lookup("HOME")
	}
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	vars["USERPROFILE"] = home
	vars["HOME"] = home

	appData := lookup("APPDATA")
	if appData == "" && home != "" {
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	vars["APPDATA"] = appData

	localAppData := lookup("LOCALAPPDATA")
	if localAppData == "" && home != "" {
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	vars["LOCALAPPDATA"] = localAppData

	return vars
}

func ExpandVars(input string, vars map[string]string) string {
	if input == "" {
		return ""
	}
	return varPattern.ReplaceAllStringFunc(input, func(match string) string {
		sub := varPattern.FindStringSubmatch(match)
		if len(sub) != 2 {
			return match
		}
		name := sub[1]
		if v, ok := vars[name]; ok {
			return v
		}
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return match
	})
}

func ExpandPath(input string, vars map[string]string) string {
	if input == "" {
		return ""
	}
	expanded := ExpandVars(input, vars)
	if expanded == "~" || strings.HasPrefix(expanded, "~/") || strings.HasPrefix(expanded, `~\`) {
		home := vars["USERPROFILE"]
		if home == "" {
			home = vars["HOME"]
		}
		if home != "" {
			if expanded == "~" {
				expanded = home
			} else {
				expanded = filepath.Join(home, expanded[2:])
			}
		}
	}
	return filepath.Clean(expanded)
}

func filterProfile(m Manifest, profile string) Manifest {
	if profile == "" {
		return m
	}
	match := func(profiles []string) bool {
		if len(profiles) == 0 {
			return true
		}
		for _, p := range profiles {
			if strings.EqualFold(p, profile) {
				return true
			}
		}
		return false
	}

	var buckets []BucketConfig
	for _, b := range m.Scoop.Buckets {
		if match(b.Profiles) {
			buckets = append(buckets, b)
		}
	}
	m.Scoop.Buckets = buckets

	var links []LinkConfig
	for _, l := range m.Links {
		if match(l.Profiles) {
			links = append(links, l)
		}
	}
	m.Links = links

	var pkgs []PackageConfig
	for _, p := range m.Packages {
		if !match(p.Profiles) {
			continue
		}
		var plinks []LinkConfig
		for _, l := range p.Links {
			if match(l.Profiles) {
				plinks = append(plinks, l)
			}
		}
		p.Links = plinks
		pkgs = append(pkgs, p)
	}
	m.Packages = pkgs
	return m
}
