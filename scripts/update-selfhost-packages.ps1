[CmdletBinding(SupportsShouldProcess)]
param()

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$ManifestPath = Join-Path $ProjectRoot "mise.toml"
$BucketPath = Join-Path $ProjectRoot "bucket"
. (Join-Path $PSScriptRoot "common.ps1")

$manifest = Read-WinMoiManifest $ManifestPath
$scoopRoot = $manifest.ScoopRoot
$engineDir = Get-ScoopEngineDir $scoopRoot
$checkver = Join-Path $engineDir "apps/scoop/current/bin/checkver.ps1"
if (-not (Test-Path $checkver)) {
    throw "Scoop checkver is not available at '$checkver'."
}

Get-ChildItem $BucketPath -Filter "*.json" -File | ForEach-Object {
    $manifest = Get-Content -Raw $_.FullName | ConvertFrom-Json
    if (-not $manifest.checkver -or -not $manifest.autoupdate) {
        throw "Self-hosted manifest '$($_.Name)' must define checkver and autoupdate."
    }
}

if ($PSCmdlet.ShouldProcess($BucketPath, "Update self-hosted Scoop manifests from livecheck metadata and commit changes")) {
    if (@(git -C $ProjectRoot status --porcelain -- bucket/).Count -gt 0 -or $LASTEXITCODE -ne 0) {
        throw "Bucket directory has uncommitted changes in '$ProjectRoot'. Commit or revert them before updating self-hosted manifests."
    }

    $global:LASTEXITCODE = 0
    & $checkver -Dir $BucketPath -Update -ThrowError
    if ($LASTEXITCODE -ne 0) {
        throw "Scoop failed to update self-hosted manifests."
    }

    foreach ($path in @(git -C $ProjectRoot diff --name-only -- bucket/ | Sort-Object)) {
        $updatedManifest = Get-Content -Raw -LiteralPath (Join-Path $ProjectRoot $path) | ConvertFrom-Json
        $package = [IO.Path]::GetFileNameWithoutExtension($path)
        git -C $ProjectRoot commit --only -m "${package}: Update to version $($updatedManifest.version)" -- $path
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to commit '$path'."
        }
    }
}
