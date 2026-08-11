function Link-WinMoiSkills {
    $agentsDirectory = Join-Path $HOME ".agents"
    $links = @(
        [pscustomobject]@{
            Source = Join-Path $ProjectRoot ".agents/skills"
            Target = Join-Path $agentsDirectory "skills"
            Type = "Junction"
        },
        [pscustomobject]@{
            Source = Join-Path $ProjectRoot ".agents/.skill-lock.json"
            Target = Join-Path $agentsDirectory ".skill-lock.json"
            Type = "SymbolicLink"
        }
    )

    if (-not (Test-Path $agentsDirectory) -and $PSCmdlet.ShouldProcess($agentsDirectory, "Create global skills directory")) {
        New-Item -ItemType Directory -Path $agentsDirectory -Force | Out-Null
    }

    foreach ($link in $links) {
        if (-not (Test-Path $link.Source)) {
            continue
        }
        if (Test-Path $link.Target) {
            $targetItem = Get-Item $link.Target
            if ($targetItem.LinkType -eq $link.Type -and $targetItem.Target -contains $link.Source) {
                continue
            }
            throw "'$($link.Target)' already exists and is not linked to '$($link.Source)'."
        }

        if ($PSCmdlet.ShouldProcess($link.Target, "Create $($link.Type) to $($link.Source)")) {
            try {
                New-Item -ItemType $link.Type -Path $link.Target -Target $link.Source | Out-Null
            }
            catch [System.UnauthorizedAccessException] {
                throw "Creating '$($link.Target)' requires an elevated Windows session or Windows Developer Mode."
            }
        }
    }
}

function Link-WindowsTerminalSettings {
    param([pscustomobject]$Manifest)

    $source = Join-Path $HOME ".config/windows-terminal/settings.json"
    $target = Join-Path $Manifest.ScoopRoot "persist/windows-terminal/settings/settings.json"
    $targetDirectory = Split-Path -Parent $target

    if (-not (Test-Path $source)) {
        if ($WhatIfPreference) {
            Write-Host "Windows Terminal settings will be copied from '$source' after chezmoi applies them."
            return
        }
        throw "Managed Windows Terminal settings are not available at '$source'."
    }

    if (Test-Path $target) {
        $targetItem = Get-Item $target
        if ($targetItem.LinkType) {
            if ($PSCmdlet.ShouldProcess($target, "Replace settings link with a managed file copied from $source")) {
                Remove-Item $target -Force
            }
        }
        elseif ((Get-FileHash $target).Hash -ne (Get-FileHash $source).Hash -and $PSCmdlet.ShouldProcess($target, "Refresh managed Windows Terminal settings from $source")) {
            Copy-Item $source $target -Force
        }
    }

    if (Test-Path $targetDirectory) {
        $targetDirectoryItem = Get-Item $targetDirectory
        if ($targetDirectoryItem.LinkType -eq "Junction" -and $targetDirectoryItem.Target -contains (Split-Path -Parent $source)) {
            if ($PSCmdlet.ShouldProcess($targetDirectory, "Replace legacy settings directory junction with a local directory")) {
                $statePath = Join-Path $targetDirectory "state.json"
                $stateContent = if (Test-Path $statePath) { Get-Content -Raw $statePath }
                Remove-Item $targetDirectory -Force
                New-Item -ItemType Directory -Path $targetDirectory -Force | Out-Null
                if ($stateContent) {
                    Set-Content -Path (Join-Path $targetDirectory "state.json") -Value $stateContent -NoNewline
                }
            }
        }
    }
    elseif ($PSCmdlet.ShouldProcess($targetDirectory, "Create Windows Terminal settings directory")) {
        New-Item -ItemType Directory -Path $targetDirectory -Force | Out-Null
    }

    if (-not (Test-Path $target) -and $PSCmdlet.ShouldProcess($target, "Copy managed Windows Terminal settings from $source")) {
        Copy-Item $source $target -Force
    }
}

function Initialize-NushellIntegrations {
    param([pscustomobject]$Manifest)

    $configDirectory = Join-Path $HOME "AppData/Roaming/nushell"
    $integrations = @(
        [pscustomobject]@{
            Executable = Join-Path $Manifest.ScoopRoot "apps/carapace-bin/current/carapace.exe"
            Arguments = @("_carapace", "nushell")
            Target = Join-Path $configDirectory "carapace.nu"
        },
        [pscustomobject]@{
            Executable = Join-Path $Manifest.ScoopRoot "apps/starship/current/starship.exe"
            Arguments = @("init", "nu")
            Target = Join-Path $configDirectory "starship.nu"
        },
        [pscustomobject]@{
            Executable = Join-Path $Manifest.ScoopRoot "apps/zoxide/current/zoxide.exe"
            Arguments = @("init", "nushell")
            Target = Join-Path $configDirectory "zoxide.nu"
        }
    )

    foreach ($integration in $integrations) {
        if (-not (Test-Path $integration.Executable) -and -not $WhatIfPreference) {
            throw "Nushell integration executable is not available at '$($integration.Executable)'."
        }
        if ($PSCmdlet.ShouldProcess($integration.Target, "Generate Nushell integration with $($integration.Executable)")) {
            $content = & $integration.Executable @($integration.Arguments)
            if ($LASTEXITCODE -ne 0) {
                throw "Failed to generate Nushell integration '$($integration.Target)'."
            }
            [IO.File]::WriteAllText($integration.Target, (($content -join [Environment]::NewLine) + [Environment]::NewLine), [Text.UTF8Encoding]::new($false))
        }
    }
}

function Resolve-Chezmoi {
    param([pscustomobject]$Manifest)

    $candidate = Join-Path $Manifest.ScoopRoot "apps/chezmoi/current/chezmoi.exe"
    if (Test-Path $candidate) {
        return $candidate
    }
    if ($WhatIfPreference) {
        return $candidate
    }

    throw "chezmoi is not available at the managed Scoop location '$candidate'."
}

function Apply-ChezmoiConfig {
    param(
        [string]$Chezmoi,
        [string]$Source,
        [string]$Destination
    )

    if (-not (Test-Path $Source)) {
        return
    }

    $arguments = @("--source", $Source, "--force", "--no-tty")
    if ($Destination) {
        $arguments += @("--destination", $Destination)
    }
    $arguments += "apply"

    if ($PSCmdlet.ShouldProcess($Destination, "Apply chezmoi source $Source")) {
        & $Chezmoi @arguments
        if ($LASTEXITCODE -ne 0) {
            throw "chezmoi failed to apply '$Source'."
        }
    }
}
