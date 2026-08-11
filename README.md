# WinMoi

Restore a lean Windows machine from one declaration: Scoop packages, Nushell configuration, and global agent skills.

![A friend's honest review of WinMoi](assets/friend-review.png)

Fair. First make my own machine reproducible, then worry about building the NixOS of Windows.

## Usage

Set an absolute Scoop root on an existing volume in `winmoi.toml`, then open an elevated terminal:

```console
winmoi plan
winmoi apply
winmoi check
winmoi clean
```

- `plan`: show declared/actual differences.
- `apply`: restore Core + Full packages and configuration.
- `check`: fail when the machine has drifted.
- `clean`: confirm each removal interactively.

Use `-Core` with `plan`, `apply`, `check`, or `clean` for essentials only. `winmoi init` is an alias for `apply`.

```console
winmoi apply -Core
winmoi apply -UpdatePackages
winmoi apply -WhatIf
```

## Declaration

`winmoi.toml` contains:

- `scoop_root`: user-selected global Scoop root.
- `buckets`: exact Scoop source names and URLs.
- `core_packages`: Git, chezmoi, Nushell, Terminal, skills, Crush, and shell essentials.
- `full_packages`: optional desktop applications.

Packages are installed globally below `<scoop_root>/apps`. WinMoi refuses package changes if Scoop resolves a different global root. Self-hosted manifests live in `bucket` and include `checkver` and `autoupdate` metadata.

Core configuration lives in `dotfiles/user`; Full-only configuration lives in `dotfiles/full`. Chezmoi applies only explicitly tracked files. Starship and zoxide generate their Nushell integrations during restore. Machine configuration is optional via `-ApplyMachineConfig`.

## Maintenance

```console
winmoi apply -UpdatePackages
./scripts/update-selfhost-packages.ps1
./scripts/validate.ps1
```

Zed's built-in updater should remain disabled. NVIDIA App is vendor-installed under `C:/Program Files`, while Scoop controls its package lifecycle.

## License

MIT
