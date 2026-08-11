[CmdletBinding(SupportsShouldProcess)]
param()

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$ManifestPath = Join-Path $ProjectRoot "winmoi.toml"
$BucketPath = Join-Path $ProjectRoot "bucket"
. (Join-Path $PSScriptRoot "common.ps1")

$manifest = Read-WinMoiManifest $ManifestPath
$scoopRoot = $manifest.ScoopRoot
$checkver = Join-Path $scoopRoot "Core/apps/scoop/current/bin/checkver.ps1"
if (-not (Test-Path $checkver)) {
    throw "Scoop checkver is not available at '$checkver'."
}

Get-ChildItem $BucketPath -Filter "*.json" -File | ForEach-Object {
    $manifest = Get-Content -Raw $_.FullName | ConvertFrom-Json
    if (-not $manifest.checkver -or -not $manifest.autoupdate) {
        throw "Self-hosted manifest '$($_.Name)' must define checkver and autoupdate."
    }
}

if ($PSCmdlet.ShouldProcess($BucketPath, "Update self-hosted Scoop manifests from livecheck metadata")) {
    $global:LASTEXITCODE = 0
    & $checkver -Dir $BucketPath -Update -ThrowError
    if ($LASTEXITCODE -ne 0) {
        throw "Scoop failed to update self-hosted manifests."
    }
}
