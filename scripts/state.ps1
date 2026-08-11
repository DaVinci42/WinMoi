[CmdletBinding()]
param(
    [ValidateSet("Plan", "Check", "Clean")]
    [string]$Mode = "Plan",
    [switch]$Core
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$ManifestPath = Join-Path $ProjectRoot "winmoi.toml"
. (Join-Path $PSScriptRoot "common.ps1")

$manifest = Read-WinMoiManifest $ManifestPath -Core:$Core
$scoopRoot = $manifest.ScoopRoot
$declaredPackages = @($manifest.Packages | ForEach-Object { $_.Split('/', 2)[-1] })
$appsPath = Join-Path $scoopRoot "apps"
$installedPackages = if (Test-Path $appsPath) {
    @(Get-ChildItem $appsPath -Directory | Where-Object { $_.Name -ne "scoop" } | ForEach-Object { $_.Name })
}
else {
    @()
}
$missingPackages = @($declaredPackages | Where-Object { $_ -notin $installedPackages } | Sort-Object)
$extraPackages = @($installedPackages | Where-Object { $_ -notin $declaredPackages } | Sort-Object)

$chezmoi = Join-Path $scoopRoot "apps/chezmoi/current/chezmoi.exe"
if (-not (Test-Path $chezmoi)) {
    throw "chezmoi is not available at the managed Scoop location '$chezmoi'."
}
$configSources = @(
    (Join-Path $ProjectRoot "dotfiles/user")
)
if (-not $Core) {
    $configSources += Join-Path $ProjectRoot "dotfiles/full"
}
$configDrift = @($configSources | ForEach-Object {
    $status = @(& $chezmoi --source $_ --destination $HOME --no-tty status --include files --path-style absolute)
    if ($LASTEXITCODE -ne 0) {
        throw "chezmoi failed to inspect configuration source '$_'."
    }
    $status | Where-Object { $_ } | ForEach-Object { $_.Substring(3) }
})
$terminalSource = Join-Path $HOME ".config/windows-terminal/settings.json"
$terminalTarget = Join-Path $scoopRoot "persist/windows-terminal/settings/settings.json"
if (-not (Test-Path $terminalSource) -or -not (Test-Path $terminalTarget) -or
    (Get-FileHash $terminalSource).Hash -ne (Get-FileHash $terminalTarget).Hash) {
    $configDrift += $terminalTarget
}

$legacyPathEntries = @()
foreach ($scope in @("Machine", "User")) {
    $entries = @([Environment]::GetEnvironmentVariable("Path", $scope) -split ";" | Where-Object { $_ })
    foreach ($entry in $entries) {
        if ($entry -match '(?i)WinGet|Komac') {
            $legacyPathEntries += [pscustomobject]@{ Scope = $scope; Path = $entry }
        }
    }
}

$residualFiles = @(
    (Join-Path $HOME "Documents/PowerShell/Microsoft.PowerShell_profile.ps1")
) | Where-Object { Test-Path $_ }
$driftCount = $missingPackages.Count + $extraPackages.Count + $configDrift.Count + $legacyPathEntries.Count + $residualFiles.Count

Write-Host "Missing declared packages: $($missingPackages.Count)"
$missingPackages | ForEach-Object { Write-Host "  + $_" }
Write-Host "Undeclared Scoop packages: $($extraPackages.Count)"
$extraPackages | ForEach-Object { Write-Host "  - $_" }
Write-Host "Configuration drift: $($configDrift.Count)"
$configDrift | ForEach-Object { Write-Host "  ~ $_" }
Write-Host "Legacy PATH entries: $($legacyPathEntries.Count)"
$legacyPathEntries | ForEach-Object { Write-Host "  - [$($_.Scope)] $($_.Path)" }
Write-Host "Residual files: $($residualFiles.Count)"
$residualFiles | ForEach-Object { Write-Host "  - $_" }

if ($Mode -eq "Clean") {
    $scoop = Join-Path $scoopRoot "Core/shims/scoop.ps1"
    foreach ($package in $extraPackages) {
        $answer = Read-Host "Uninstall undeclared Scoop package '$package'? [y/N]"
        if ($answer -notin @("y", "Y")) {
            continue
        }
        & $scoop uninstall --global $package
        if ($LASTEXITCODE -ne 0) {
            throw "Scoop failed to uninstall '$package'."
        }
    }

    foreach ($scope in @("Machine", "User")) {
        $scopeEntries = @($legacyPathEntries | Where-Object { $_.Scope -eq $scope })
        if ($scopeEntries.Count -eq 0) {
            continue
        }
        $answer = Read-Host "Remove $($scopeEntries.Count) legacy WinGet/Komac PATH entries from $scope PATH? [y/N]"
        if ($answer -notin @("y", "Y")) {
            continue
        }
        $legacyPaths = @($scopeEntries | ForEach-Object { $_.Path })
        $keptEntries = @([Environment]::GetEnvironmentVariable("Path", $scope) -split ";" |
            Where-Object { $_ -and $_ -notin $legacyPaths })
        [Environment]::SetEnvironmentVariable("Path", ($keptEntries -join ";"), $scope)
    }

    foreach ($file in $residualFiles) {
        $answer = Read-Host "Remove residual file '$file'? [y/N]"
        if ($answer -in @("y", "Y")) {
            Remove-Item $file -Force
        }
    }

    Write-Host "Cleanup finished. Run 'winmoi check' to verify the remaining drift."
    exit 0
}

if ($Mode -eq "Check" -and $driftCount -gt 0) {
    Write-Error "WinMoi found $driftCount state differences. Run 'winmoi plan' for details or 'winmoi apply' to restore declared state."
    exit 1
}

if ($driftCount -eq 0) {
    Write-Host "Actual state matches the WinMoi declaration."
}
