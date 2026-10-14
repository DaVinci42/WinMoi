[CmdletBinding(SupportsShouldProcess)]
param(
    [switch]$SkipPackages,
    [switch]$SkipUserConfig,
    [switch]$UpdatePackages,
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

function Invoke-Scoop {
    param(
        [string]$Scoop,
        [string[]]$Arguments
    )

    & $Scoop @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Scoop failed: scoop $($Arguments -join ' ')"
    }
}

function Resolve-Scoop {
    param([pscustomobject]$Manifest)

    $engineDir = Get-ScoopEngineDir $Manifest.ScoopRoot
    $scoop = Join-Path $engineDir "shims/scoop.ps1"
    if (Test-Path $scoop) {
        return $scoop
    }

    $existing = Get-Command scoop -ErrorAction SilentlyContinue
    if ($existing) {
        throw "Scoop is installed outside '$($Manifest.ScoopRoot)'. Remove or migrate it before managing packages."
    }

    if ($PSCmdlet.ShouldProcess($Manifest.ScoopRoot, "Install user-level Scoop in $($Manifest.ScoopRoot)")) {
        [Environment]::SetEnvironmentVariable("SCOOP", $Manifest.ScoopRoot, "User")
        $env:SCOOP = $Manifest.ScoopRoot
        $installer = Join-Path ([IO.Path]::GetTempPath()) "install-scoop.ps1"
        try {
            Invoke-WebRequest -Uri "https://raw.githubusercontent.com/ScoopInstaller/Install/master/install.ps1" -OutFile $installer
            & $installer -ScoopDir $Manifest.ScoopRoot
            $scoop = Join-Path $Manifest.ScoopRoot "shims/scoop.ps1"
            if ($LASTEXITCODE -ne 0 -or -not (Test-Path $scoop)) {
                throw "Scoop failed to install in '$($Manifest.ScoopRoot)'."
            }
        }
        finally {
            Remove-Item $installer -Force -ErrorAction SilentlyContinue
        }
    }

    $scoop
}

function Assert-ScoopLayout {
    param(
        [pscustomobject]$Manifest,
        [string]$Scoop
    )

    if (-not (Test-Path $Scoop)) {
        return
    }

    try {
        Assert-ScoopRoot $Manifest.ScoopRoot
    }
    catch {
        throw "$($_.Exception.Message.TrimEnd('.')). Refusing to install or update packages in another location."
    }
}

function Set-ScoopPathPrecedence {
    param([pscustomobject]$Manifest)

    $scoopShimPath = Join-Path $Manifest.ScoopRoot "shims"
    $normalizedShim = $scoopShimPath.TrimEnd('\', '/')

    [Environment]::SetEnvironmentVariable("SCOOP", $Manifest.ScoopRoot, "User")
    $env:SCOOP = $Manifest.ScoopRoot
    $entries = @([Environment]::GetEnvironmentVariable("Path", "User") -split ";" |
        Where-Object { $_ -and $_.TrimEnd('\', '/') -ne $normalizedShim })
    [Environment]::SetEnvironmentVariable("Path", (@($scoopShimPath) + $entries -join ";"), "User")

    $processEntries = @($env:Path -split ";" |
        Where-Object { $_ -and $_.TrimEnd('\', '/') -ne $normalizedShim })
    $env:Path = @($scoopShimPath) + $processEntries -join ";"
}

function Add-WinMoiBuckets {
    param(
        [pscustomobject]$Manifest,
        [string]$Scoop
    )

    $gitCurrent = Join-Path $Manifest.ScoopRoot "apps/git/current"
    $gitCommand = Get-Command git -ErrorAction SilentlyContinue
    if (-not (Test-Path $gitCurrent) -and -not $gitCommand -and $PSCmdlet.ShouldProcess("main/git", "Install bootstrap dependency")) {
        Invoke-Scoop $Scoop @("install", "main/git")
    }

    $engineDir = Get-ScoopEngineDir $Manifest.ScoopRoot
    $bucketRoot = Join-Path $engineDir "buckets"
    foreach ($bucket in $Manifest.Buckets) {
        if (Test-Path (Join-Path $bucketRoot $bucket.Name)) {
            continue
        }

        if ($PSCmdlet.ShouldProcess($bucket.Name, "Add Scoop bucket from $($bucket.Source)")) {
            Invoke-Scoop $Scoop @("bucket", "add", $bucket.Name, $bucket.Source)
        }
    }
}

function Get-ScoopPackage {
    param(
        [pscustomobject]$Manifest,
        [string]$Package
    )

    $parts = $Package.Split('/', 2)
    $name = $parts[-1]
    $selfHosted = $parts.Count -eq 2 -and $parts[0] -eq "winmoi"
    $engineDir = Get-ScoopEngineDir $Manifest.ScoopRoot
    $availableManifest = if ($selfHosted) {
        Join-Path $engineDir "buckets/winmoi/bucket/$name.json"
    }

    [pscustomobject]@{
        Name = $name
        Source = $Package
        SelfHosted = $selfHosted
        AvailableManifest = $availableManifest
    }
}

function Install-WinMoiPackages {
    param(
        [pscustomobject]$Manifest,
        [string]$Scoop
    )

    $engineDir = Get-ScoopEngineDir $Manifest.ScoopRoot

    if ($UpdatePackages -and $PSCmdlet.ShouldProcess("Scoop and its buckets", "Update manifests before package updates")) {
        foreach ($bucket in $Manifest.Buckets) {
            $bucketPath = Join-Path $engineDir "buckets/$($bucket.Name)"
            if (Test-Path $bucketPath) {
                $changes = @(git -C $bucketPath status --porcelain)
                if ($LASTEXITCODE -ne 0 -or $changes.Count -gt 0) {
                    throw "Bucket '$($bucket.Name)' has local changes or cannot be inspected. Refusing to overwrite it."
                }
                git -C $bucketPath fetch origin
                if ($LASTEXITCODE -ne 0) {
                    throw "Failed to fetch Scoop bucket '$($bucket.Name)'."
                }
                $upstream = git -C $bucketPath rev-parse --abbrev-ref "@{upstream}"
                if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($upstream)) {
                    throw "Failed to resolve the upstream branch for Scoop bucket '$($bucket.Name)'."
                }
                git -C $bucketPath merge --ff-only $upstream
                if ($LASTEXITCODE -ne 0) {
                    throw "Bucket '$($bucket.Name)' cannot fast-forward to '$upstream'. Resolve its history manually."
                }
            }
        }
        Invoke-Scoop $Scoop @("update", "scoop")
    }

    foreach ($package in $Manifest.Packages) {
        $resolvedPackage = Get-ScoopPackage $Manifest $package
        $current = Join-Path $Manifest.ScoopRoot "apps/$($resolvedPackage.Name)/current"

        if (-not (Test-Path $current)) {
            if ($PSCmdlet.ShouldProcess($package, "Install in $($Manifest.ScoopRoot)")) {
                Invoke-Scoop $Scoop @("install", $resolvedPackage.Source)
            }
        }
        elseif ($UpdatePackages -and $resolvedPackage.SelfHosted) {
            $availableManifest = $resolvedPackage.AvailableManifest
            if (-not (Test-Path $availableManifest) -and $WhatIfPreference) {
                $availableManifest = Join-Path $ProjectRoot "bucket/$($resolvedPackage.Name).json"
            }
            if (-not (Test-Path $availableManifest)) {
                throw "The registered winmoi bucket does not contain '$($resolvedPackage.Name)'."
            }
            $installedManifest = Join-Path $current "manifest.json"
            $installedVersion = if (Test-Path $installedManifest) {
                (Get-Content -Raw $installedManifest | ConvertFrom-Json).version
            }
            $availableVersion = (Get-Content -Raw $availableManifest | ConvertFrom-Json).version
            if ($installedVersion -ne $availableVersion -and $PSCmdlet.ShouldProcess($package, "Update in $($Manifest.ScoopRoot)")) {
                Invoke-Scoop $Scoop @("update", $resolvedPackage.Source)
            }
            else {
                Write-Host "Self-hosted package '$package' is current at version '$installedVersion'."
            }
        }
        elseif ($UpdatePackages -and $PSCmdlet.ShouldProcess($package, "Update in $($Manifest.ScoopRoot)")) {
            Invoke-Scoop $Scoop @("update", $resolvedPackage.Source)
        }
        else {
            Write-Host "Package '$package' is already installed in '$current'."
        }

        if (-not $WhatIfPreference -and (Test-Path $Scoop) -and -not (Test-Path $current)) {
            throw "Scoop did not create the expected package location '$current'."
        }
    }
}

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
            $targetItem = Get-Item $link.Target -Force
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

function Get-WinMoiFileHash {
    param([string]$LiteralPath)

    $stream = [IO.File]::OpenRead($LiteralPath)
    $hasher = [Security.Cryptography.SHA256]::Create()
    try { [pscustomobject]@{ Hash = [BitConverter]::ToString($hasher.ComputeHash($stream)).Replace('-', '') } }
    finally { $stream.Dispose(); $hasher.Dispose() }
}

function Apply-WinMoiDotfiles {
    param([pscustomobject]$Manifest)

    $stateDirectory = Join-Path $env:LOCALAPPDATA "WinMoi/dotfiles"
    foreach ($dotfile in $Manifest.Dotfiles) {
        if (-not (Test-Path -LiteralPath $dotfile.Source -PathType Leaf)) {
            throw "Declared dotfile source '$($dotfile.Source)' does not exist."
        }
        $hasher = [Security.Cryptography.SHA256]::Create()
        try {
            $key = [BitConverter]::ToString($hasher.ComputeHash([Text.Encoding]::UTF8.GetBytes($dotfile.Target.ToLowerInvariant()))).Replace('-', '')
        }
        finally { $hasher.Dispose() }
        $statePath = Join-Path $stateDirectory "$key.json"
        $state = if (Test-Path -LiteralPath $statePath) { Get-Content -Raw -LiteralPath $statePath | ConvertFrom-Json }
        $item = Get-Item -LiteralPath $dotfile.Target -Force -ErrorAction SilentlyContinue
        $correctLink = $item -and $item.LinkType -eq "SymbolicLink" -and
            $item.Target -contains $dotfile.Source
        $sourceHash = (Get-WinMoiFileHash -LiteralPath $dotfile.Source).Hash
        $syncBack = $false
        if ($item -and -not $item.LinkType) {
            if ($item.PSIsContainer) { throw "Dotfile target '$($dotfile.Target)' is a directory." }
            $targetHash = (Get-WinMoiFileHash -LiteralPath $dotfile.Target).Hash
            if ($state -and $state.Source -eq $dotfile.Source -and $targetHash -ne $sourceHash) {
                if ($sourceHash -ne $state.Hash -and $targetHash -ne $state.Hash) {
                    throw "Both repository and target changed: '$($dotfile.Target)'. Resolve the conflict before applying."
                }
                $syncBack = $sourceHash -eq $state.Hash
            }
        }
        if ($correctLink -and $state -and $state.Source -eq $dotfile.Source -and $state.Hash -eq $sourceHash) {
            continue
        }
        if (-not $PSCmdlet.ShouldProcess($dotfile.Target, "Safely link to repository; back up existing files and sync only previously managed edits")) {
            continue
        }
        New-Item -ItemType Directory -Path $stateDirectory -Force | Out-Null
        if (-not $correctLink) {
            $targetDirectory = Split-Path -Parent $dotfile.Target
            New-Item -ItemType Directory -Path $targetDirectory -Force | Out-Null
            $temporary = "$($dotfile.Target).winmoi-$([guid]::NewGuid().ToString('N'))"
            $backup = "$($dotfile.Target).winmoi-backup-$([guid]::NewGuid().ToString('N'))"
            $moved = $false
            try {
                New-Item -ItemType SymbolicLink -Path $temporary -Target $dotfile.Source -ErrorAction Stop | Out-Null
                if ($syncBack) {
                    Copy-Item -LiteralPath $dotfile.Source -Destination (Join-Path $stateDirectory "$key-$([guid]::NewGuid().ToString('N')).backup")
                    Copy-Item -LiteralPath $dotfile.Target -Destination $dotfile.Source -Force
                }
                if ($item) {
                    Move-Item -LiteralPath $dotfile.Target -Destination $backup -ErrorAction Stop
                    $moved = $true
                }
                Move-Item -LiteralPath $temporary -Destination $dotfile.Target -ErrorAction Stop
                if ($moved) { Write-Host "Previous target preserved at '$backup'." }
            }
            catch {
                if ($moved -and -not (Get-Item -LiteralPath $dotfile.Target -Force -ErrorAction SilentlyContinue)) {
                    Move-Item -LiteralPath $backup -Destination $dotfile.Target -ErrorAction Stop
                }
                throw
            }
            finally {
                if (Get-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue) {
                    Remove-Item -LiteralPath $temporary -Force
                }
            }
        }
        @{ Source = $dotfile.Source; Hash = (Get-WinMoiFileHash -LiteralPath $dotfile.Source).Hash } |
            ConvertTo-Json | Set-Content -LiteralPath $statePath -Encoding UTF8
    }
}

$manifest = Read-WinMoiManifest $ManifestPath -Core:$Core

if (-not $SkipPackages) {
    $scoop = Resolve-Scoop $manifest
    Assert-ScoopLayout $manifest $scoop
    if ($PSCmdlet.ShouldProcess((Join-Path $manifest.ScoopRoot "shims"), "Prepend Scoop shims to user PATH")) {
        Set-ScoopPathPrecedence $manifest
    }
    Add-WinMoiBuckets $manifest $scoop
    if ($UpdatePackages) {
        & (Join-Path $PSScriptRoot "update-selfhost-packages.ps1") -WhatIf:$WhatIfPreference
    }
    Install-WinMoiPackages $manifest $scoop
}

if ($PSVersionTable.PSVersion.Major -lt 7 -and -not $WhatIfPreference) {
    $pwsh = Join-Path $manifest.ScoopRoot "apps/pwsh/current/pwsh.exe"
    if (-not (Test-Path -LiteralPath $pwsh)) {
        throw "PowerShell 7 is required for user-level symlinks. Install the declared pwsh package first."
    }
    $selectedProfile = if ($Core) { "core" } else { "full" }
    $parameters = @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", $PSCommandPath, "-SkipPackages", "-Profile", $selectedProfile)
    if ($SkipUserConfig) { $parameters += "-SkipUserConfig" }
    & $pwsh @parameters
    if ($LASTEXITCODE -ne 0) { throw "PowerShell 7 configuration phase failed." }
    return
}

Link-WinMoiSkills

if (-not $SkipUserConfig) {
    Apply-WinMoiDotfiles $manifest
}
