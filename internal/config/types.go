package config

import "strings"

const (
	LinkTypeAuto     = ""
	LinkTypeSymlink  = "symlink"
	LinkTypeJunction = "junction"
	LinkTypeHardlink = "hardlink"
	LinkTypeCopy     = "copy"

	FallbackError    = "error"
	FallbackHardlink = "hardlink"
	FallbackCopy     = "copy"
)

type Manifest struct {
	Scoop      ScoopConfig       `toml:"scoop"`
	Env        map[string]string `toml:"env"`
	ConfigRepo ConfigRepoConfig  `toml:"config_repo"`
	Links      []LinkConfig      `toml:"links"`
	Packages   []PackageConfig   `toml:"packages"`
	Skills     SkillsConfig      `toml:"skills"`
	Clean      CleanConfig       `toml:"clean"`
}

type ScoopConfig struct {
	Root    string         `toml:"root"`
	Buckets []BucketConfig `toml:"buckets"`
}

type BucketConfig struct {
	Name     string   `toml:"name"`
	URL      string   `toml:"url,omitempty"`
	Profiles []string `toml:"profiles,omitempty"`
}

type ConfigRepoConfig struct {
	URL        string `toml:"url,omitempty"`
	LocalPath  string `toml:"local_path,omitempty"`
	Branch     string `toml:"branch,omitempty"`
	ConfigsDir string `toml:"configs_dir,omitempty"`
}

type LinkConfig struct {
	From     string   `toml:"from"`
	To       string   `toml:"to"`
	Type     string   `toml:"type,omitempty"`
	Fallback string   `toml:"fallback,omitempty"`
	Profiles []string `toml:"profiles,omitempty"`
}

type PackageConfig struct {
	Name       string       `toml:"name"`
	ConfigTo   string       `toml:"config_to,omitempty"`
	ConfigType string       `toml:"config_type,omitempty"`
	Links      []LinkConfig `toml:"links,omitempty"`
	Profiles   []string     `toml:"profiles,omitempty"`
}

func (p PackageConfig) AppName() string {
	if idx := strings.LastIndex(p.Name, "/"); idx >= 0 {
		return p.Name[idx+1:]
	}
	return p.Name
}

func (p PackageConfig) BucketName() string {
	if idx := strings.LastIndex(p.Name, "/"); idx >= 0 {
		return p.Name[:idx]
	}
	return ""
}

type SkillsConfig struct {
	LocalDir  string   `toml:"local_dir,omitempty"`
	Validate  *bool    `toml:"validate,omitempty"`
	Tool      string   `toml:"tool,omitempty"`
	ExtraArgs []string `toml:"extra_args,omitempty"`
	Upstream  []string `toml:"upstream,omitempty"`
}

func (s SkillsConfig) ShouldValidate() bool {
	if s.Validate == nil {
		return true
	}
	return *s.Validate
}

type CleanConfig struct {
	KeepVersions int `toml:"keep_versions"`
}

type ResolvedLink struct {
	From     string
	To       string
	Type     string
	Fallback string
	Category string
	Owner    string
	Adopt    bool
}

type LoadedConfig struct {
	ManifestPath  string
	ManifestDir   string
	ConfigRepoDir string
	ScoopRoot     string
	ScoopPersist  string
	Manifest      Manifest
	Vars          map[string]string
}
