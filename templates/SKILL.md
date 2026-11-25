---
name: winmoi-maintenance
description: Maintain this Windows dotfiles and Scoop manifest repository using winmoi. Use when adding Scoop packages, editing config links, managing ~/.agents/skills, or troubleshooting environment drift.
---

# WinMoi Repository Maintenance

## Repository Layout

- `manifest.toml`: Declarative source of truth for Scoop root/buckets, user environment variables, config links, packages, skills, and cleanup policy.
- `configs/<pkg>/`: Per-package config directories linked bidirectionally to each package's `config_to` (existing target is adopted on first `apply`).
- `agent/skills/`: Local private Agent Skills linked individually as directory junctions into `~/.agents/skills/<name>` (kept isolated from upstream `npx skills` installs).

## Standard Workflow

1. Edit `manifest.toml` or files under this config repository.
2. Preview changes first:
   ```powershell
   winmoi apply --dry-run
   ```
3. Apply declaratively:
   ```powershell
   winmoi apply
   ```
4. Verify state and health checks (SCOOP env, Developer Mode, broken links, SKILL.md frontmatter, external skills tool):
   ```powershell
   winmoi status
   ```

## Guardrails

- Prefer `${SCOOP_PERSIST}` for Scoop apps that maintain persisted config directories so `scoop update` does not break links.
- Use `type = "junction"` for directories (no admin or Developer Mode required) and `type = "symlink"` for files.
- Never manually delete `~/.agents/skills/*` directories managed by `winmoi`; use `winmoi apply --prune`.
- Check `winmoi status` before running `--prune` to review unmanaged packages or removed links.
