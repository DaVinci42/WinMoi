# WinMoi Development Plan

## Goal

WinMoi restores and maintains a lean Windows machine from a single `mise.toml` declaration of Scoop packages, selected configuration dotfiles, and global agent skills. It uses user-level Scoop for deterministic package locations (`D:/Scoop` by default, user-configurable) and `mise` for bootstrap/task orchestration and symlinked dotfiles.

## User Preferences

- Install ALL packages via Scoop at user level (`SCOOP` in `mise.toml`, defaulting to `D:/Scoop`) so every package has a strict, deterministic location under `<scoop_root>/apps`.
- Do not use `mise [tools]` for package installation.
- Default shell is PowerShell (`pwsh`); ignore Nushell (`nu`).
- Persist Scoop `ignore_running_processes=true` during bootstrap/apply/up so running apps do not block operations for this user, including direct Scoop commands. This also affects uninstall/reset; removal still requires confirmation.
- Drive bootstrap and ongoing maintenance directly via `mise` commands (`mise bootstrap --only task`, `mise run plan|apply|up|check|clean|validate`) without a custom `winmoi` wrapper.
- Always ask for confirmation before uninstalling anything.

## Package Management

- `mise.toml` declares the Scoop root (`[env]`), Scoop packages (`[bootstrap.packages]`), tracked dotfiles (`[dotfiles]`), and maintenance tasks (`[tasks.*]`).
- Group newly declared packages by bucket and sort packages within each bucket.
- Scoop and all managed Scoop applications live at user scope below `<scoop_root>` (`<scoop_root>/apps`, `<scoop_root>/buckets`, `<scoop_root>/shims`, `<scoop_root>/persist`).
- Installations and updates must use the configured `SCOOP` root. Refuse package changes when Scoop resolves another root.
- Packages use Scoop's versioned `<scoop_root>/apps/<package>/<version>` layout and stable `current` junction.
- `<scoop_root>/shims` must be first in User `PATH`, ahead of legacy WinGet locations.
- Prefer official Scoop buckets. Charmbracelet's official bucket provides Crush.
- Package-specific custom manifests belong in this repository's `bucket` directory. The repository is registered as the `winmoi` Scoop bucket, and packages are declared as `scoop:winmoi/<package>`.
- Every self-hosted manifest must define Scoop `checkver` and `autoupdate` metadata so versions, URLs, and hashes can be refreshed consistently.
- FluentCleaner, AdGuard, NVIDIA App, and the Skills CLI (`winmoi/skills`) use the common self-hosted package mechanism.
- The Skills CLI is installed as `scoop:winmoi/skills` and manages the repository-backed global catalog under `.agents`.

## Configuration Management

- Users explicitly add every managed configuration file to `[dotfiles]` in `mise.toml`.
- Core user configuration belongs in `dotfiles/user`; Full-only application configuration belongs in `dotfiles/full`.
- Every `[dotfiles]` entry is linked as a real file `SymbolicLink` pointing directly to the repository checkout (`dotfiles.default_mode = "symlink"`), including `mise.toml` (`~/.config/mise/config.toml`), PowerShell profile, Starship, Crush (`crush.json`), Windows Terminal, and Zed.
- Editing a target file (or an application saving settings in-place) directly updates the working tree in the repository. If an application performs atomic rename-on-save and replaces a symlink with a regular file, `mise run apply` uses local baseline metadata to sync edits back before re-linking. First adoption backs up the existing target without overwriting repository content. Conflicting edits stop. Create a temporary link before moving the old target; keep backups under the target directory and `%LOCALAPPDATA%/WinMoi/dotfiles`.

## State & Maintenance Workflow

- `mise run plan` compares declared Scoop packages and dotfile symlinks with the current machine without changing it.
- Bootstrap, apply, up, plan, check, and clean accept `-Profile core` or `-Profile full` (default). `-Core` remains shorthand for Core. Use `mise run bootstrap -- -Profile core` for essentials, including Zed, Crush, and rtk.
- `mise run up` updates self-hosted manifests and installed Scoop packages.
- `mise run check` fails when actual state differs from the declaration.
- `mise run clean` offers undeclared Scoop packages and known legacy state for removal, confirming every removal.
- `mise run validate` validates self-hosted manifests, dotfile sources, Scoop root, PATH precedence, and livechecks.

## Bootstrap Workflow

1. `mise run bootstrap -- -Profile core|full` reads and validates `mise.toml` using Windows PowerShell, which is available before pwsh is installed. Use native `mise bootstrap --only task` (also with `--from`) to run Full through the same script. Do not use native package/dotfile phases; they bypass our profile and safety logic.
2. It installs user-level Scoop in `<scoop_root>` if not already installed.
3. It verifies Scoop's configured root and prepends `<scoop_root>/shims` to User `PATH` before installing or updating packages.
4. It registers declared remote buckets, including this repository as `winmoi`.
5. It installs missing Scoop packages (including `main/mise` and `main/pwsh`) and verifies each stable `current` path.
6. With `-UpdatePackages` (`mise run up`), it refreshes local self-hosted manifests for review, updates remote buckets, and updates installed Scoop packages.
7. It links only `.agents/skills` and `.agents/.skill-lock.json` into `~/.agents` so unrelated agent state remains local.
8. It ensures all declared `[dotfiles]` targets are file-level `SymbolicLink`s pointing to the repository checkout (syncing back any atomically saved file modifications first).
9. `-Core`, `-WhatIf`, `-SkipPackages`, and `-SkipUserConfig` support minimal, preview, and partial restores.

## Current Layout

- `mise.toml`: Scoop root (`[env]`), Scoop packages (`[bootstrap.packages]`), symlinked dotfiles (`[dotfiles]`), and tasks (`[tasks.*]`).
- `bucket`: self-hosted Scoop manifests (`adguard.json`, `fluentcleaner.json`, `nvidia-app.json`, `skills.json`) with livecheck and autoupdate metadata.
- `scripts/update-selfhost-packages.ps1`: refreshes every self-hosted manifest through Scoop checkver.
- `scripts/validate.ps1`: validates manifests, dotfile sources, installed versions, Scoop root, livechecks, and PATH precedence.
- `scripts/state.ps1`: plans, checks, and interactively cleans differences between declared and actual state.
- `.agents`: versioned global skills and Skills CLI lock state.
- `dotfiles/user`: User-level dotfiles (`powershell`, `starship.toml`, `crush`, `windows-terminal`, `zed`, `gitignore_global`). Zed configuration and the Zed package are included in Core.
- `dotfiles/full`: Reserved for Full-only user-level dotfiles; currently no entries.
- `scripts/bootstrap.ps1`: Scoop installation, user-level package management, skills links, and dotfile symlink implementation.

## Design Constraints

- Keep the declaration human-readable and minimal.
- Never silently fall back to another package manager or installation root.
- Built-in application updaters must not relocate Scoop-managed software. Prefer `mise run up`.
- NVIDIA App is an accepted exception: Scoop controls package lifecycle operations, while NVIDIA's installer controls its physical path under `C:/Program Files`.
- Remove only confirmed WinGet duplicates by exact package ID. Never clear `D:/WinGet` while non-migrated software remains.
- Prefer preview and diff operations before making system changes.
- Distribute the project under the MIT License.

## Commit History

- Keep `feat: init project` as the initial commit and amend project-level changes into it.
- Give each supported package one dedicated `feat: support <package>` commit.
- Amend manual package changes into its existing support commit. Automated manifest updates are separate commits using `<package>: Update to version <version>`.
- Avoid creating incremental commits for project-level or package-specific changes that belong to an existing commit.
- Bucket updates must preserve local changes and only fast-forward. After rewritten remote history, stop and require explicit reconciliation; never automatically hard-reset a bucket.
- Do not install Git hooks. Run `mise run apply` explicitly to sync atomically saved dotfiles before committing.
