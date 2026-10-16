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
- `scoop.local.txt` next to `mise.toml` is Git-ignored and adds machine-specific Scoop package IDs to both Core and Full. Accept one `bucket/package` or bare package ID per line, optional `scoop:` prefixes, blank lines, and `#` comments. Merge without duplicates, reject conflicting buckets, and include local packages in plan/check/clean declarations. Core must install explicitly listed Full-only packages too.
- Group newly declared packages by bucket and sort packages within each bucket.
- Scoop and all managed Scoop applications live at user scope below `<scoop_root>` (`<scoop_root>/apps`, `<scoop_root>/buckets`, `<scoop_root>/shims`, `<scoop_root>/persist`).
- Installations and updates must use the configured `SCOOP` root. Refuse package changes when Scoop resolves another root.
- Packages use Scoop's versioned `<scoop_root>/apps/<package>/<version>` layout and stable `current` junction.
- `<scoop_root>/shims` must be first in User `PATH`, ahead of legacy WinGet locations.
- Prefer official Scoop buckets. Charmbracelet's official bucket provides Crush.
- Package-specific custom manifests belong in this repository's `bucket` directory. The repository is registered as the `winmoi` Scoop bucket, and packages are declared as `scoop:winmoi/<package>`.
- Every self-hosted manifest must define Scoop `checkver` and `autoupdate` metadata so versions, URLs, and hashes can be refreshed consistently.
- FluentCleaner, AdGuard, NVIDIA App, SFW, Figtree, and the Skills CLI (`winmoi/skills`) use the common self-hosted package mechanism.
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
- `bucket`: self-hosted Scoop manifests (`adguard.json`, `figtree.json`, `fluentcleaner.json`, `nvidia-app.json`, `sfw.json`, `skills.json`) with livecheck and autoupdate metadata.
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
- NVIDIA App, AdGuard, and SFW are accepted exceptions: Scoop controls package lifecycle operations, while their vendor installers control physical paths under `C:/Program Files`.
- Remove only confirmed WinGet duplicates by exact package ID. Never clear `D:/WinGet` while non-migrated software remains.
- Prefer preview and diff operations before making system changes.
- Distribute the project under the MIT License.

## Maintenance Diagnostics

- AdGuard's official stable changelog is `https://adguard.com/versions/windows/release.html`. Read the dotted product version from the `versions__item-title` heading, not the `version-*` anchor: anchors concatenate variable-width build components, can omit trailing zeros, and cannot be reliably split. Regression-check consecutive releases (currently 8.0.2, 8.0.1, 8.0.0).
- AdGuard product versions differ from installer/file build versions. Compare the changelog with `(Get-Item 'C:/Program Files/AdGuard/Adguard.exe').VersionInfo.ProductVersion`; do not infer the installed application version from Scoop's stored manifest or setup.exe. The vendor application may have updated independently.
- After changing checkver, run `& "$env:SCOOP/apps/scoop/current/bin/checkver.ps1" -App adguard -Dir ./bucket -ThrowError`. For a one-time correction from inflated metadata such as 8.05.560.0 to 8.0.2, use `-ForceUpdate` to refresh the repository manifest and recompute the official setup.exe SHA256; this does not update installed software. Verify Authenticode and never bypass hash checks. The setup URL is mutable, so a hash mismatch requires investigation, not an unconditional retry without verification.
- `mise run apply` installs missing packages and restores configuration; it does not update installed packages. `mise run up` refreshes checkout manifests, but self-hosted installation/update still reads the separately registered `<SCOOP>/buckets/winmoi` checkout. Local manifest edits are not automatically deployed there. Inspect both copies and their Git state; do not overwrite dirty buckets, rewrite their history, or push without authorization.
- Ordinary Scoop updates skip numerically lower versions. Correcting AdGuard's inflated installed metadata may require a one-time `scoop update adguard --force` only after the registered bucket contains the reviewed correction. Scoop executes the OLD installed manifest's uninstaller during updates; AdGuard's old uninstaller invokes setup.exe with `-uninstall -quiet` and elevation. Obtain explicit uninstall confirmation and preserve application settings before proceeding. Do not edit installed manifests or version directories merely to make validation pass.
- Newer Scoop writes `scoop-manifest.json` and `scoop-install.json`; older installs use `manifest.json` and `install.json`. Prefer the new names with legacy fallback when checking installed versions. Inspect install metadata's `url` and `bucket`: an existing local-manifest `url` is reused by `scoop update` even when the declaration names a bucket. On this machine AdGuard updates from the repository's `bucket/adguard.json`. Never assume the registered bucket is the effective source without checking install metadata.
- Keep configuration backups outside Git under `%LOCALAPPDATA%/WinMoi`. AdGuard's `%ProgramData%/AdGuard/Core` requires elevation; stop its service temporarily for a consistent backup, verify copied configuration hashes, and restart it in `finally`. The confirmed 8.0.2 migration preserved all six `config-*.xml` files byte-for-byte. A manually patched registered bucket remains dirty until explicitly reconciled with the published fix; the safety check in `mise run up` will intentionally refuse it.
- Verification commands: `mise run apply`, `mise run check`, and `mise run validate` (or `mise run validate -- -SkipLivecheck` for offline checks). `check` checks package presence and configuration, while `validate` checks self-hosted installed manifest versions and livechecks. Neither proves the vendor-installed AdGuard binary matches its metadata; inspect ProductVersion separately. The repository has no dedicated unit-test suite; use isolated PowerShell assertions for parser changes and PowerShell AST parsing for syntax checks, including Windows PowerShell 5.1 bootstrap compatibility.
- `jq` is a declared Core package. Tool wrappers may require a real executable even when a tool advertises built-in jq; verify `jq --version` before use. If mise reports a missing shim file, inspect `<SCOOP>/shims/mise*` and `<SCOOP>/apps/mise/current/bin/mise.exe`, then use `scoop reset mise` to rebuild its shim. Run the real binary when updating mise itself to avoid locking its shim.

## Commit History

- Keep `feat: init project` as the initial commit and amend project-level changes into it.
- Give each supported package one dedicated `feat: support <package>` commit.
- Amend manual package changes into its existing support commit. Automated manifest updates are separate commits using `<package>: Update to version <version>`.
- Avoid creating incremental commits for project-level or package-specific changes that belong to an existing commit.
- Bucket updates must preserve local changes and only fast-forward. After rewritten remote history, stop and require explicit reconciliation; never automatically hard-reset a bucket.
- Do not install Git hooks. Run `mise run apply` explicitly to sync atomically saved dotfiles before committing.
