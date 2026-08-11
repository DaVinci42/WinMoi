# WinMoi Development Plan

## Goal

WinMoi restores a lean Windows machine from a small declaration of packages, selected configuration, and global agent skills. It uses Scoop for deterministic global package locations and chezmoi for explicitly selected configuration files.

## User Preferences

- Install Scoop packages globally by default, not per-user.
- Always ask for confirmation before uninstalling anything.

## Package Management

- `winmoi.toml` declares one Scoop root, bucket sources, essential `core_packages`, and optional `full_packages`.
- Group newly declared packages by bucket and sort packages within each bucket.
- Run `winmoi apply` directly whenever practical. Use a UAC elevation prompt when administrator access is required instead of asking the user to run it manually.
- Scoop itself belongs in `<scoop_root>/Core`.
- All managed applications are installed globally below `<scoop_root>/apps`.
- Installations and updates must use the same configured global root. WinMoi must refuse package changes when Scoop resolves another `global_path`.
- Packages use Scoop's versioned `<scoop_root>/apps/<package>/<version>` layout and stable `current` junction.
- `<scoop_root>/shims` must be first in both machine and user `PATH`, ahead of legacy WinGet locations.
- Prefer official Scoop buckets. Charmbracelet's official bucket provides Crush.
- Package-specific custom manifests belong in this repository's `bucket` directory. The repository is registered as the `winmoi` Scoop bucket, and packages are declared as `winmoi/<package>`.
- Every self-hosted manifest must define Scoop `checkver` and `autoupdate` metadata so versions, URLs, and hashes can be refreshed consistently.
- FluentCleaner, NVIDIA App, and the Skills CLI use the common self-hosted package mechanism.
- The Skills CLI is installed as `winmoi/skills` and manages the repository-backed global catalog under `.agents`.
- `main/chezmoi` is the initial package because configuration management depends on it.
- Do not use WinGet or Komac.

## Configuration Management

- Users explicitly add every managed configuration file.
- chezmoi performs configuration tracking, diffing, and application.
- Core user configuration belongs in `dotfiles/user`; Full-only application configuration belongs in `dotfiles/full`.
- Machine-level configuration belongs in `dotfiles/machine` and may require elevation when applied.
- Package installation remains global, while configuration scope stays separated because destination paths and permissions differ.
- Nushell, Starship, and Windows Terminal settings remain user-level because their standard destinations are user-specific.
- chezmoi manages Windows Terminal settings at `~/.config/windows-terminal/settings.json`; bootstrap copies that file to Scoop's persisted portable settings destination on each restore.

## State Workflow

- `winmoi plan` compares declared packages and configuration with the current machine without changing it.
- `winmoi apply` restores Core + Full; `winmoi apply -Core` restores only essentials; `winmoi init` is an alias.
- `winmoi check` fails when actual state differs from the declaration.
- `winmoi clean` offers undeclared packages and known legacy state for removal, confirming every removal.

## Bootstrap Workflow

1. `winmoi apply` dispatches to the restore implementation, which reads and validates `winmoi.toml`.
2. It requires an elevated Windows session for global package operations.
3. It installs Scoop in `<scoop_root>/Core` with global applications rooted at `<scoop_root>`.
4. It verifies Scoop's configured global root and prepends its shims to machine and user `PATH` before installing or updating packages.
5. It registers declared remote buckets, including this repository as `winmoi`.
6. It installs missing packages globally and verifies each stable `current` path.
7. With `-UpdatePackages`, it refreshes local self-hosted manifests for review, updates remote buckets, and updates installed packages globally in the same root.
8. It links only `.agents/skills` and `.agents/.skill-lock.json` into `~/.agents` so unrelated agent state remains local.
9. It resolves chezmoi from the managed Scoop location, applies `dotfiles/user`, and copies the managed Windows Terminal settings into Scoop's persisted destination.
10. With `-ApplyMachineConfig`, it applies `dotfiles/machine` from an elevated Windows session.
11. `-Core`, `-WhatIf`, `-SkipPackages`, and `-SkipUserConfig` support minimal, preview, and partial restores.

## Current Layout

- `winmoi.toml`: Scoop root, bucket sources, and declarative package list.
- `bucket`: self-hosted Scoop manifests with livecheck and autoupdate metadata.
- `scripts/update-selfhost-packages.ps1`: refreshes every self-hosted manifest through Scoop checkver.
- `scripts/validate.ps1`: validates manifests, installed versions, Scoop root, livechecks, and PATH precedence.
- `scripts/state.ps1`: plans, checks, and interactively cleans differences between declared and actual state.
- `.agents`: versioned global skills and Skills CLI lock state.
- `dotfiles/user`: Core user-level chezmoi source state.
- `dotfiles/full`: Full-only user-level chezmoi source state.
- `dotfiles/machine`: machine-level chezmoi source state.
- `winmoi.cmd`: user-facing `winmoi` command entry point.
- `scripts/winmoi.ps1`: command dispatcher.
- `scripts/bootstrap.ps1`: Scoop installation, global package management, skills links, and chezmoi restore implementation.
- `.githooks/pre-commit`: applies user configuration before each commit.
- `scripts/install-git-hooks.ps1`: enables the repository-managed Git hooks.

## Design Constraints

- Keep the manifest human-readable and minimal.
- Never silently fall back to another package manager or installation root.
- Built-in application updaters must not relocate Scoop-managed software. Prefer `winmoi apply -UpdatePackages`.
- NVIDIA App is an accepted exception: Scoop controls package lifecycle operations, while NVIDIA's installer controls its physical path under `C:/Program Files`.
- Remove only confirmed WinGet duplicates by exact package ID. Never clear `D:/WinGet` while non-migrated software remains.
- Prefer preview and diff operations before making system changes.
- Distribute the project under the MIT License.

## Commit History

- Keep `feat: init project` as the initial commit and amend project-level changes into it.
- Give each supported package one dedicated `feat: support <package>` commit.
- Amend all later changes for a package into its existing support commit.
- Avoid creating incremental commits for project-level or package-specific changes that belong to an existing commit.
- After every force push, fetch and hard-reset the local `winmoi` Scoop bucket clone to `origin/main` so rewritten history does not cause `scoop update` to fail with divergent branches.
- Enable `.githooks/pre-commit`; it must apply `dotfiles/user` before every commit so the working configuration matches the committed source.
