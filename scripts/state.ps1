[CmdletBinding()]
param(
    [ValidateSet("Plan", "Check", "Clean")]
    [string]$Mode = "Plan",
    [switch]$Core,
    [ValidateSet("core", "full")]
    [string]$Profile = "full"
)

if ($Core -and $PSBoundParameters.ContainsKey("Profile") -and $Profile -ne "core") {
    throw "-Core cannot be combined with -Profile full."
}
$Core = $Core -or $Profile -eq "core"

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$ManifestPath = Join-Path $ProjectRoot "mise.toml"
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
$allDeclaredPackages = @(($manifest.CorePackages + $manifest.FullPackages) | ForEach-Object { $_.Split('/', 2)[-1] })
$extraPackages = @($installedPackages | Where-Object { $_ -notin $allDeclaredPackages } | Sort-Object)

$configDrift = @()
foreach ($dotfile in $manifest.Dotfiles) {
    if (-not (Test-Path $dotfile.Target)) {
        $configDrift += "$($dotfile.Target) (missing)"
        continue
    }
    $item = Get-Item $dotfile.Target -Force
    $resolvedTarget = if ($item.Target) { @($item.Target)[0] } else { $null }
    if ($item.LinkType -ne "SymbolicLink" -or -not $resolvedTarget -or
        [IO.Path]::GetFullPath($resolvedTarget).TrimEnd('\', '/') -ne $dotfile.Source.TrimEnd('\', '/')) {
        $configDrift += "$($dotfile.Target) (not a SymbolicLink to '$($dotfile.SourceRelative)')"
    }
}

$skillLinks = @(
    [pscustomobject]@{ Source = Join-Path $ProjectRoot ".agents/skills"; Target = Join-Path $HOME ".agents/skills"; Type = "Junction" },
    [pscustomobject]@{ Source = Join-Path $ProjectRoot ".agents/.skill-lock.json"; Target = Join-Path $HOME ".agents/.skill-lock.json"; Type = "SymbolicLink" }
)
foreach ($link in $skillLinks) {
    if (-not (Test-Path $link.Source)) {
        continue
    }
    if (-not (Test-Path $link.Target)) {
        $configDrift += "$($link.Target) (missing $($link.Type))"
        continue
    }
    $item = Get-Item $link.Target -Force
    if ($item.LinkType -ne $link.Type -or $item.Target -notcontains $link.Source) {
        $configDrift += "$($link.Target) (not a $($link.Type) to '$($link.Source)')"
    }
}

$legacyPathEntries = @()
foreach ($scope in @("User")) {
    $entries = @([Environment]::GetEnvironmentVariable("Path", $scope) -split ";" | Where-Object { $_ })
    foreach ($entry in $entries) {
        if ($entry -match '(?i)WinGet|Komac') {
            $legacyPathEntries += [pscustomobject]@{ Scope = $scope; Path = $entry }
        }
    }
}

$residualFiles = @(
    (Join-Path $HOME ".config/windows-terminal")
    (Join-Path $HOME "AppData/Roaming/nushell")
) | Where-Object { Test-Path $_ }

$driftCount = $missingPackages.Count + $extraPackages.Count + $configDrift.Count + $legacyPathEntries.Count + $residualFiles.Count

Write-Host "Missing declared Scoop packages: $($missingPackages.Count)"
$missingPackages | ForEach-Object { Write-Host "  + $_" }
Write-Host "Undeclared Scoop packages: $($extraPackages.Count)"
$extraPackages | ForEach-Object { Write-Host "  - $_" }
Write-Host "Configuration (dotfile symlink) drift: $($configDrift.Count)"
$configDrift | ForEach-Object { Write-Host "  ~ $_" }
Write-Host "Legacy PATH entries: $($legacyPathEntries.Count)"
$legacyPathEntries | ForEach-Object { Write-Host "  - [$($_.Scope)] $($_.Path)" }
Write-Host "Residual files/directories: $($residualFiles.Count)"
$residualFiles | ForEach-Object { Write-Host "  - $_" }

if ($Mode -eq "Clean") {
    $engineDir = Get-ScoopEngineDir $scoopRoot
    $scoop = Join-Path $engineDir "shims/scoop.ps1"
    Assert-ScoopRoot $scoopRoot
    $env:SCOOP = $scoopRoot
    foreach ($package in $extraPackages) {
        $answer = Read-Host "Uninstall undeclared Scoop package '$package'? [y/N]"
        if ($answer -notin @("y", "Y")) {
            continue
        }
        & $scoop uninstall $package
        if ($LASTEXITCODE -ne 0) {
            throw "Scoop failed to uninstall '$package'."
        }
    }

    foreach ($scope in @("User")) {
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
        $answer = Read-Host "Remove residual path '$file'? [y/N]"
        if ($answer -in @("y", "Y")) {
            Remove-Item $file -Recurse -Force
        }
    }

    Write-Host "Cleanup finished. Run 'mise run check' to verify the remaining drift."
    exit 0
}

if ($Mode -eq "Check" -and $driftCount -gt 0) {
    Write-Error "Found $driftCount state differences. Run 'mise run plan' for details or 'mise run apply' to restore declared state."
    exit 1
}

if ($driftCount -eq 0) {
    Write-Host "Actual state matches the mise.toml declaration."
}
