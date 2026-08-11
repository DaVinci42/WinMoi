$ErrorActionPreference = "Stop"

function Show-WinMoiHelp {
    @"
Usage: winmoi <command> [options]

Commands:
  apply   Restore the declared packages and configuration
  plan    Compare declared state with the current machine
  check   Exit unsuccessfully when declared and actual state differ
  clean   Interactively remove undeclared Scoop packages and legacy state
  init    Alias for apply

Apply options:
  -WhatIf                Preview changes
  -UpdatePackages        Update installed packages in the same Scoop root
  -SkipPackages          Skip package installation and updates
  -SkipUserConfig        Skip user configuration
  -ApplyMachineConfig    Apply machine configuration (requires elevation)
  -Core                  Use only the core package profile
"@
}

$command = $args | Select-Object -First 1
$commandArguments = @($args | Select-Object -Skip 1)

if (-not $command -or $command -in @("help", "--help", "-h")) {
    Show-WinMoiHelp
    exit 0
}

switch ($command.ToLowerInvariant()) {
    { $_ -in @("apply", "init") } {
        if ($commandArguments.Count -eq 1 -and $commandArguments[0] -in @("help", "--help", "-h", "-Help")) {
            Show-WinMoiHelp
            exit 0
        }

        & (Join-Path $PSScriptRoot "bootstrap.ps1") @commandArguments
        exit $LASTEXITCODE
    }
    "plan" {
        & (Join-Path $PSScriptRoot "state.ps1") -Mode Plan @commandArguments
        exit $LASTEXITCODE
    }
    "check" {
        & (Join-Path $PSScriptRoot "state.ps1") -Mode Check @commandArguments
        exit $LASTEXITCODE
    }
    "clean" {
        & (Join-Path $PSScriptRoot "state.ps1") -Mode Clean @commandArguments
        exit $LASTEXITCODE
    }
    default {
        Write-Error "Unknown command '$command'.`n`n$(Show-WinMoiHelp)"
    }
}
