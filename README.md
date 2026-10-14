# WinMoi

Restore and maintain a lean Windows machine from a single `mise.toml` declaration: user-level Scoop packages, PowerShell & app dotfiles, and global agent skills.

## Quick Bootstrap (New Machine)

1. Adjust `SCOOP` in `mise.toml` if you want a directory other than `D:/Scoop` (or set `$env:WINMOI_SCOOP_ROOT`).
2. Bootstrap from a new machine using a temporary WinGet `mise` seed:

```powershell
winget install jdx.mise
mise bootstrap --from https://github.com/DaVinci42/WinMoi.git --only task
winget uninstall --exact --id jdx.mise
```

Or directly from a local clone:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File ./scripts/bootstrap.ps1 -Profile full
```

## Ongoing Maintenance

Once bootstrapped (`~/.config/mise/config.toml` is symlinked to `mise.toml`), run `mise` tasks from any directory:

```console
mise run plan
mise run apply
mise run up
mise run check
mise run clean
mise run validate
```

- `mise run plan`: show declared vs. actual differences (Scoop packages, dotfile symlinks, legacy PATH).
- `mise run apply`: restore Core + Full Scoop packages and dotfile symlinks.
- `mise run up`: update self-hosted manifests and installed Scoop packages.
- `mise run check`: fail when the machine has drifted from `mise.toml`.
- `mise run clean`: confirm each removal interactively.
- `mise run validate`: verify manifests, dotfile sources, Scoop root, PATH precedence, and livechecks.

Select bootstrap packages with `-Profile core` or `-Profile full` (the default, includes Core). Both include Zed, Crush, and rtk. Use `mise run bootstrap` for profile selection; native `mise bootstrap --only task` applies Full through the same safe script. Do not use bare `mise bootstrap` or `mise dot apply`: their native phases bypass profile selection and our safe file replacement, and relative sources in the global symlink resolve differently. The same profile parameter works with `apply`, `up`, `plan`, `check`, and `clean`; `-Core` remains a shorthand. Core checks and cleanup ignore declared Full packages.

```console
mise run bootstrap -- -Profile core
mise run bootstrap -- -Profile full
mise run bootstrap -- -Profile core -WhatIf
mise run apply -- -Core
mise run apply -- -WhatIf
mise run plan -- -Core
```

## Declaration (`mise.toml`)

- `[env]`: user-configured `SCOOP` root (default `D:/Scoop`). All packages are strictly installed by Scoop under `<SCOOP>/apps`.
- `[bootstrap.packages]`: user-level Scoop packages (`pwsh`, `mise`, `git`, `gh`, `nodejs`, `starship`, `zoxide`, `windows-terminal`, `crush`, `winmoi/skills`, fonts, and Full desktop apps).
- `[dotfiles]`: explicit file-level `SymbolicLink` mappings pointing directly to `dotfiles/user` and `dotfiles/full`. Editing any target config updates the repo immediately; apps that overwrite files on save (such as Windows Terminal) are synced back when you explicitly run `mise run apply`, provided a prior apply recorded their baseline. First adoption preserves the old target as a `.winmoi-backup-*` file and keeps the repository authoritative. Concurrent source and target edits stop with a conflict. Link creation is tested before replacing a target; metadata and source backups stay under `%LOCALAPPDATA%/WinMoi/dotfiles`.

Zed's built-in updater should remain disabled. NVIDIA App is vendor-installed under `C:/Program Files`, while Scoop controls its package lifecycle.

Bootstrap, apply, and up persist Scoop `ignore_running_processes=true` for the current user. Direct `scoop update` commands also continue when an app is running; this is not deferred activation and does not bypass file locks. The setting also affects uninstall/reset, while WinMoi cleanup still asks for confirmation.

## Automated Bucket Updates

The Excavator workflow runs Tuesdays and Fridays at 03:37 UTC. You can also select **Actions > Excavator > Run workflow** on GitHub, or run `gh workflow run excavator.yml` after the workflow is available on the default branch.

Scoop `checkver` updates manifests before Git publishes one commit per changed package (`<package>: Update to version <version>`). Failed checks stop publication; unchanged manifests produce no commits. Local testing with `act workflow_dispatch -P windows-latest=-self-hosted` checks updates without committing or pushing.

## License

MIT
