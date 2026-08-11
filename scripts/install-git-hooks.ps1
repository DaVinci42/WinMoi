$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot

git -C $projectRoot config core.hooksPath .githooks
if ($LASTEXITCODE -ne 0) {
    throw "Unable to configure Git hooks."
}

Write-Output "Git hooks installed."
