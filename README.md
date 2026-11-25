# WinMoi

User-level declarative CLI for Windows to manage Scoop buckets, packages, updates, cleanup, dotfiles config links, and `~/.agents/skills`.

Requires [Scoop](https://scoop.sh) to be already installed and configured.

## Install

This repository also acts as a Scoop bucket:

```powershell
scoop bucket add winmoi https://github.com/DaVinci42/WinMoi.git
scoop install winmoi/winmoi
```

## Quick Start

Create a separate config repository (can be private) from [winmoi-template](https://github.com/DaVinci42/winmoi-template) or via `winmoi init`:

```powershell
mkdir my-winmoi-config && cd my-winmoi-config
winmoi init --with-skill
```

Then preview, apply, and verify:

```powershell
winmoi apply --dry-run
winmoi apply
winmoi status
```

## Commands

| Command | Purpose | Key Flags |
| :--- | :--- | :--- |
| `init` | Generate `manifest.toml` template | `--from-current`, `--with-skill` |
| `apply` | Converge env, buckets, packages, links, and skills | `--config`, `--dry-run`, `--only`, `--prune`, `--profile`, `--on-conflict` |
| `update` | Update packages, `config_repo`, links, and skills (with optional cleanup) | `--only`, `--all`, `--clean`, `--keep`, `--dry-run` |
| `status` | Diff local state against `manifest.toml` and report health issues | `--json` |
