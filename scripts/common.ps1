function Read-WinMoiManifest {
    param(
        [string]$Path,
        [switch]$Core
    )

    $content = Get-Content -Raw $Path
    $rootMatch = [regex]::Match($content, '(?m)^scoop_root\s*=\s*"([^"]+)"\s*$')
    $bucketsMatch = [regex]::Match($content, '(?ms)^buckets\s*=\s*\[(.*?)\]')
    $corePackagesMatch = [regex]::Match($content, '(?ms)^core_packages\s*=\s*\[(.*?)\]')
    $fullPackagesMatch = [regex]::Match($content, '(?ms)^full_packages\s*=\s*\[(.*?)\]')

    if (-not $rootMatch.Success -or -not $bucketsMatch.Success -or -not $corePackagesMatch.Success -or -not $fullPackagesMatch.Success) {
        throw "winmoi.toml must define scoop_root, buckets, core_packages, and full_packages."
    }

    $buckets = @([regex]::Matches($bucketsMatch.Groups[1].Value, '"([^"]+)"') |
        ForEach-Object {
            $parts = $_.Groups[1].Value.Split('|', 2)
            if ($parts.Count -ne 2 -or [string]::IsNullOrWhiteSpace($parts[0]) -or [string]::IsNullOrWhiteSpace($parts[1])) {
                throw "Each bucket must use the 'name|source' format."
            }
            [pscustomobject]@{
                Name = $parts[0]
                Source = $parts[1]
            }
        })
    $corePackages = @([regex]::Matches($corePackagesMatch.Groups[1].Value, '"([^"]+)"') |
        ForEach-Object { $_.Groups[1].Value })
    $fullPackages = @([regex]::Matches($fullPackagesMatch.Groups[1].Value, '"([^"]+)"') |
        ForEach-Object { $_.Groups[1].Value })

    if ($corePackages.Count -eq 0) {
        throw "winmoi.toml must contain at least one core package."
    }

    $configuredRoot = $rootMatch.Groups[1].Value
    if (-not [IO.Path]::IsPathRooted($configuredRoot)) {
        throw "scoop_root must be an absolute path, got '$configuredRoot'."
    }
    $scoopRoot = [IO.Path]::GetFullPath($configuredRoot)
    $volumeRoot = [IO.Path]::GetPathRoot($scoopRoot)
    if (-not (Test-Path $volumeRoot -PathType Container)) {
        throw "The volume for scoop_root '$scoopRoot' does not exist. Update scoop_root in winmoi.toml."
    }
    if ($scoopRoot.TrimEnd('\', '/') -eq $volumeRoot.TrimEnd('\', '/')) {
        throw "scoop_root cannot be the volume root '$volumeRoot'. Choose a dedicated directory."
    }
    if (Test-Path $scoopRoot -PathType Leaf) {
        throw "scoop_root '$scoopRoot' is a file. Choose a directory."
    }

    [pscustomobject]@{
        ScoopRoot = $scoopRoot
        Buckets = $buckets
        CorePackages = $corePackages
        FullPackages = $fullPackages
        Packages = $corePackages + $(if ($Core) { @() } else { $fullPackages })
    }
}

function Get-ScoopConfiguredGlobalRoot {
    $configuredRoot = [Environment]::GetEnvironmentVariable("SCOOP_GLOBAL", "Machine")
    if (-not $configuredRoot) {
        $configPath = Join-Path $HOME ".config/scoop/config.json"
        if (Test-Path $configPath) {
            $configuredRoot = (Get-Content -Raw $configPath | ConvertFrom-Json).global_path
        }
    }
    if (-not $configuredRoot) {
        $configuredRoot = Join-Path $env:ProgramData "scoop"
    }

    [IO.Path]::GetFullPath($configuredRoot)
}

function Assert-ScoopGlobalRoot {
    param([string]$ExpectedRoot)

    $configuredRoot = Get-ScoopConfiguredGlobalRoot
    if ($configuredRoot.TrimEnd('\', '/') -ne $ExpectedRoot.TrimEnd('\', '/')) {
        throw "Scoop global_path is '$configuredRoot', expected '$ExpectedRoot'."
    }
}
