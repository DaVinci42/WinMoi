[CmdletBinding(SupportsShouldProcess)]
param(
    [switch]$SkipPackages,
    [switch]$SkipUserConfig,
    [switch]$ApplyMachineConfig,
    [switch]$UpdatePackages,
    [switch]$Core
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$ManifestPath = Join-Path $ProjectRoot "winmoi.toml"
. (Join-Path $PSScriptRoot "common.ps1")
. (Join-Path $PSScriptRoot "bootstrap-scoop.ps1")
. (Join-Path $PSScriptRoot "bootstrap-config.ps1")

$manifest = Read-WinMoiManifest $ManifestPath -Core:$Core

if (-not $SkipPackages) {
    if (-not $WhatIfPreference -and -not (Test-IsAdministrator)) {
        throw "Global Scoop package installation and updates require an elevated Windows session."
    }
    $scoop = Resolve-Scoop $manifest
    Assert-ScoopLayout $manifest $scoop
    if ($PSCmdlet.ShouldProcess((Join-Path $manifest.ScoopRoot "shims"), "Prepend Scoop shims to machine and user PATH")) {
        Set-ScoopPathPrecedence $manifest
    }
    Add-WinMoiBuckets $manifest $scoop
    if ($UpdatePackages) {
        & (Join-Path $PSScriptRoot "update-selfhost-packages.ps1") -WhatIf:$WhatIfPreference
    }
    Install-WinMoiPackages $manifest $scoop
}

Link-WinMoiSkills

if (-not $SkipUserConfig -or $ApplyMachineConfig) {
    $chezmoi = Resolve-Chezmoi $manifest
}

if (-not $SkipUserConfig) {
    Apply-ChezmoiConfig $chezmoi (Join-Path $ProjectRoot "dotfiles/user") $HOME
    Initialize-NushellIntegrations $manifest
    if (-not $Core) {
        Apply-ChezmoiConfig $chezmoi (Join-Path $ProjectRoot "dotfiles/full") $HOME
    }
    Link-WindowsTerminalSettings $manifest
}

if ($ApplyMachineConfig) {
    if (-not (Test-IsAdministrator)) {
        throw "Machine configuration requires an elevated Windows session."
    }
    Apply-ChezmoiConfig $chezmoi (Join-Path $ProjectRoot "dotfiles/machine") "C:/"
}
