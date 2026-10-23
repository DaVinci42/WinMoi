$psHomeModules = Join-Path $PSHOME "Modules"
$env:PSModulePath = (@($psHomeModules) + @($env:PSModulePath -split ";" | Where-Object {
    $_ -and $_ -notmatch '(?i)(\\|/)PowerShell(\\|/)Modules$' -and $_ -notmatch '(?i)(\\|/)apps(\\|/)pwsh(\\|/)'
}) | Select-Object -Unique) -join ";"

function Expand-WinMoiPath {
    param(
        [string]$Path,
        [string]$ScoopRoot
    )

    $expanded = $Path
    if ($expanded.StartsWith("~/") -or $expanded.StartsWith("~\")) {
        $expanded = Join-Path $HOME $expanded.Substring(2)
    }
    elseif ($ScoopRoot -and $expanded -match '^(?i)D:/Scoop(/|\\)(.*)$') {
        $expanded = Join-Path $ScoopRoot $Matches[2]
    }

    [IO.Path]::GetFullPath($expanded)
}

function Read-WinMoiManifest {
    param(
        [string]$Path,
        [switch]$Core
    )

    if (-not (Test-Path $Path)) {
        throw "Declaration file '$Path' does not exist."
    }

    $content = Get-Content -Raw $Path
    $projectRoot = Split-Path -Parent $Path

    $scoopRootMatch = [regex]::Match($content, '(?m)^SCOOP\s*=\s*"([^"]+)"\s*$')
    if (-not $scoopRootMatch.Success) {
        throw "mise.toml must define SCOOP in [env]."
    }

    $configuredRoot = if ($env:WINMOI_SCOOP_ROOT) { $env:WINMOI_SCOOP_ROOT } else { $scoopRootMatch.Groups[1].Value }
    if (-not [IO.Path]::IsPathRooted($configuredRoot)) {
        throw "SCOOP must be an absolute path, got '$configuredRoot'."
    }
    $scoopRoot = [IO.Path]::GetFullPath($configuredRoot)
    $volumeRoot = [IO.Path]::GetPathRoot($scoopRoot)
    if (-not (Test-Path $volumeRoot -PathType Container)) {
        throw "The volume for SCOOP '$scoopRoot' does not exist. Update SCOOP in mise.toml."
    }
    if ($scoopRoot.TrimEnd('\', '/') -eq $volumeRoot.TrimEnd('\', '/')) {
        throw "SCOOP cannot be the volume root '$volumeRoot'. Choose a dedicated directory."
    }
    if (Test-Path $scoopRoot -PathType Leaf) {
        throw "SCOOP '$scoopRoot' is a file. Choose a directory."
    }

    $bucketsMatch = [regex]::Match($content, '(?ms)#\s*scoop_buckets\s*=\s*\[(.*?)\n#?\s*\]')
    $buckets = if ($bucketsMatch.Success) {
        @([regex]::Matches($bucketsMatch.Groups[1].Value, '"([^"]+)"') |
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
    }
    else {
        @(
            [pscustomobject]@{ Name = "extras"; Source = "https://github.com/ScoopInstaller/Extras.git" },
            [pscustomobject]@{ Name = "games"; Source = "https://github.com/Calinou/scoop-games.git" },
            [pscustomobject]@{ Name = "versions"; Source = "https://github.com/ScoopInstaller/Versions.git" },
            [pscustomobject]@{ Name = "nerd-fonts"; Source = "https://github.com/matthewjberger/scoop-nerd-fonts.git" },
            [pscustomobject]@{ Name = "charm"; Source = "https://github.com/charmbracelet/scoop-bucket.git" },
            [pscustomobject]@{ Name = "winmoi"; Source = "https://github.com/DaVinci42/WinMoi.git" }
        )
    }

    $packagesSectionMatch = [regex]::Match($content, '(?ms)^\[bootstrap\.packages\]\s*(.*?)(?=^\[|\z)')
    if (-not $packagesSectionMatch.Success) {
        throw "mise.toml must define [bootstrap.packages]."
    }

    $corePackages = @()
    $fullPackages = @()
    $inFullProfile = $false
    foreach ($line in ($packagesSectionMatch.Groups[1].Value -split "`r?`n")) {
        if ($line -match '^\s*#\s*full\b') {
            $inFullProfile = $true
            continue
        }
        if ($line -match '^\s*"scoop:([^"]+)"\s*=\s*"([^"]+)"\s*$') {
            $pkg = $Matches[1]
            if ($inFullProfile) {
                $fullPackages += $pkg
            }
            else {
                $corePackages += $pkg
            }
        }
    }

    if ($corePackages.Count -eq 0) {
        throw "mise.toml must contain at least one core Scoop package in [bootstrap.packages]."
    }

    $localPackages = @()
    $localPackagesPath = Join-Path $projectRoot "scoop.local.txt"
    if (Test-Path -LiteralPath $localPackagesPath) {
        $knownPackages = @{}
        foreach ($package in ($corePackages + $fullPackages)) {
            $knownPackages[$package.Split('/')[-1]] = $package
        }
        $lineNumber = 0
        foreach ($line in (Get-Content -LiteralPath $localPackagesPath -Encoding UTF8 -ErrorAction Stop)) {
            $lineNumber++
            $package = ($line -split '#', 2)[0].Trim()
            if (-not $package) {
                continue
            }
            $package = $package -replace '^scoop:', ''
            if ($package -notmatch '^[a-zA-Z0-9][a-zA-Z0-9._-]*(/[a-zA-Z0-9][a-zA-Z0-9._-]*)?$') {
                throw "Invalid Scoop package ID in '${localPackagesPath}' at line ${lineNumber}: '$package'. Use 'bucket/package' or 'package'."
            }
            $name = $package.Split('/')[-1]
            if ($knownPackages.ContainsKey($name)) {
                $knownPackage = $knownPackages[$name]
                if ($package.Contains('/') -and -not $knownPackage.Contains('/')) {
                    $localPackages = @($localPackages | Where-Object { $_ -ne $knownPackage })
                    $knownPackages[$name] = $package
                }
                elseif ($package.Contains('/') -and $package -ne $knownPackage) {
                    throw "Conflicting Scoop package IDs in '${localPackagesPath}' at line ${lineNumber}: '$package' and '$knownPackage'."
                }
                else {
                    $package = $knownPackage
                }
            }
            else {
                $knownPackages[$name] = $package
            }
            if ($package -notin $localPackages) {
                $localPackages += $package
            }
        }
    }

    $packages = $corePackages + $(if ($Core) { @() } else { $fullPackages })
    foreach ($package in $localPackages) {
        if ($package -notin $packages) {
            $packages += $package
        }
    }

    $dotfilesSectionMatch = [regex]::Match($content, '(?ms)^\[dotfiles\]\s*(.*?)(?=^\[|\z)')
    $dotfiles = @()
    if ($dotfilesSectionMatch.Success) {
        foreach ($line in ($dotfilesSectionMatch.Groups[1].Value -split "`r?`n")) {
            if ($line -match '^\s*"([^"]+)"\s*=\s*"([^"]+)"\s*$') {
                $targetRaw = $Matches[1]
                $sourceRel = $Matches[2]
                $isFullOnly = $sourceRel.Replace('\', '/').StartsWith("dotfiles/full/", [StringComparison]::OrdinalIgnoreCase)
                if ($Core -and $isFullOnly) {
                    continue
                }
                $dotfiles += [pscustomobject]@{
                    TargetRaw = $targetRaw
                    Target = Expand-WinMoiPath $targetRaw $scoopRoot
                    SourceRelative = $sourceRel
                    Source = [IO.Path]::GetFullPath((Join-Path $projectRoot $sourceRel))
                    FullOnly = $isFullOnly
                }
            }
        }
    }

    [pscustomobject]@{
        ScoopRoot = $scoopRoot
        Buckets = $buckets
        CorePackages = $corePackages
        FullPackages = $fullPackages
        LocalPackages = $localPackages
        Packages = $packages
        Dotfiles = $dotfiles
    }
}

function Get-ScoopConfiguredRoot {
    $configuredRoot = [Environment]::GetEnvironmentVariable("SCOOP", "User")
    if (-not $configuredRoot) {
        $configPath = Join-Path $HOME ".config/scoop/config.json"
        if (Test-Path $configPath) {
            $config = Get-Content -Raw $configPath | ConvertFrom-Json
            $configuredRoot = $config.root_path
        }
    }
    if (-not $configuredRoot) {
        $configuredRoot = Join-Path $HOME "scoop"
    }

    [IO.Path]::GetFullPath($configuredRoot)
}

function Assert-ScoopRoot {
    param([string]$ExpectedRoot)

    $configuredRoot = Get-ScoopConfiguredRoot
    $normalizedConfigured = $configuredRoot.TrimEnd('\', '/')
    $normalizedExpected = $ExpectedRoot.TrimEnd('\', '/')

    if ($normalizedConfigured -ne $normalizedExpected) {
        throw "Scoop root_path is '$configuredRoot', expected '$ExpectedRoot'."
    }
}

function Get-ScoopEngineDir {
    param([string]$ScoopRoot)

    $legacyCore = Join-Path $ScoopRoot "Core"
    if (Test-Path (Join-Path $legacyCore "apps/scoop/current")) {
        throw "Legacy global Scoop layout found at '$legacyCore'. Migrate it explicitly before managing packages."
    }
    return $ScoopRoot
}
