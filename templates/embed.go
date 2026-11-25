package templates

import _ "embed"

//go:embed manifest.toml
var ManifestTemplate string

//go:embed SKILL.md
var MaintenanceSkillTemplate string
