[CmdletBinding()]
param(
    [switch]$SkipLivecheck
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$ManifestPath = Join-Path $ProjectRoot "winmoi.toml"
$BucketPath = Join-Path $ProjectRoot "bucket"
. (Join-Path $PSScriptRoot "common.ps1")

$winMoiManifest = Read-WinMoiManifest $ManifestPath
$scoopRoot = $winMoiManifest.ScoopRoot
$selfHostedPackages = @($winMoiManifest.Packages | Where-Object { $_.StartsWith("winmoi/", [StringComparison]::OrdinalIgnoreCase) })
$manifests = @(Get-ChildItem $BucketPath -Filter "*.json" -File)

foreach ($manifestFile in $manifests) {
    $manifest = Get-Content -Raw $manifestFile.FullName | ConvertFrom-Json
    if (-not $manifest.checkver -or -not $manifest.autoupdate) {
        throw "Self-hosted manifest '$($manifestFile.Name)' must define checkver and autoupdate."
    }

    $package = "winmoi/$($manifestFile.BaseName)"
    if ($package -notin $selfHostedPackages) {
        throw "Self-hosted manifest '$($manifestFile.Name)' is not declared as '$package' in winmoi.toml."
    }

    $installedManifestPath = Join-Path $scoopRoot "apps/$($manifestFile.BaseName)/current/manifest.json"
    if (Test-Path $installedManifestPath) {
        $installedVersion = (Get-Content -Raw $installedManifestPath | ConvertFrom-Json).version
        if ($installedVersion -ne $manifest.version) {
            throw "Installed '$($manifestFile.BaseName)' version '$installedVersion' does not match manifest version '$($manifest.version)'."
        }
    }
}

foreach ($package in $selfHostedPackages) {
    $name = $package.Split('/', 2)[1]
    if (-not (Test-Path (Join-Path $BucketPath "$name.json"))) {
        throw "Declared self-hosted package '$package' has no bucket manifest."
    }
}

Assert-ScoopGlobalRoot $scoopRoot

$shimPath = (Join-Path $scoopRoot "shims").TrimEnd('\', '/')
foreach ($scope in @("Machine", "User")) {
    $pathEntries = @([Environment]::GetEnvironmentVariable("Path", $scope) -split ";" | Where-Object { $_ })
    $shimIndex = [array]::FindIndex($pathEntries, [Predicate[string]] { param($entry) $entry.TrimEnd('\', '/') -eq $shimPath })
    $winGetIndex = [array]::FindIndex($pathEntries, [Predicate[string]] { param($entry) $entry -match '(?i)WinGet' })
    if ($shimIndex -lt 0 -or ($winGetIndex -ge 0 -and $shimIndex -gt $winGetIndex)) {
        throw "Scoop shims do not precede WinGet in the $scope PATH."
    }
}

if (-not $SkipLivecheck) {
    $checkver = Join-Path $scoopRoot "Core/apps/scoop/current/bin/checkver.ps1"
    if (-not (Test-Path $checkver)) {
        throw "Scoop checkver is not available at '$checkver'."
    }
    $global:LASTEXITCODE = 0
    & $checkver -Dir $BucketPath -ThrowError
    if ($LASTEXITCODE -ne 0) {
        throw "Scoop livechecks failed."
    }
}

Write-Host "WinMoi validation passed."
