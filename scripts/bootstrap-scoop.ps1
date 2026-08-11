function Test-IsAdministrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

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

    $scoopDirectory = Join-Path $Manifest.ScoopRoot "Core"
    $scoop = Join-Path $scoopDirectory "shims/scoop.ps1"
    if (Test-Path $scoop) {
        return $scoop
    }

    $existing = Get-Command scoop -ErrorAction SilentlyContinue
    if ($existing) {
        throw "Scoop is installed outside '$scoopDirectory'. Remove or migrate it before WinMoi manages packages."
    }

    if ($PSCmdlet.ShouldProcess($scoopDirectory, "Install Scoop with global applications in $($Manifest.ScoopRoot)")) {
        $installer = Join-Path ([IO.Path]::GetTempPath()) "winmoi-install-scoop.ps1"
        try {
            Invoke-WebRequest -Uri "https://raw.githubusercontent.com/ScoopInstaller/Install/master/install.ps1" -OutFile $installer
            & $installer -RunAsAdmin -ScoopDir $scoopDirectory -ScoopGlobalDir $Manifest.ScoopRoot
            if ($LASTEXITCODE -ne 0 -or -not (Test-Path $scoop)) {
                throw "Scoop failed to install in '$scoopDirectory'."
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
        Assert-ScoopGlobalRoot $Manifest.ScoopRoot
    }
    catch {
        throw "$($_.Exception.Message.TrimEnd('.')). Refusing to install or update packages in another location."
    }
}

function Set-ScoopPathPrecedence {
    param([pscustomobject]$Manifest)

    $shimPath = Join-Path $Manifest.ScoopRoot "shims"
    foreach ($scope in @("Machine", "User")) {
        $entries = @([Environment]::GetEnvironmentVariable("Path", $scope) -split ";" |
            Where-Object { $_ -and $_.TrimEnd('\', '/') -ne $shimPath.TrimEnd('\', '/') })
        [Environment]::SetEnvironmentVariable("Path", (@($shimPath) + $entries -join ";"), $scope)
    }

    $processEntries = @($env:Path -split ";" |
        Where-Object { $_ -and $_.TrimEnd('\', '/') -ne $shimPath.TrimEnd('\', '/') })
    $env:Path = @($shimPath) + $processEntries -join ";"
}

function Add-WinMoiBuckets {
    param(
        [pscustomobject]$Manifest,
        [string]$Scoop
    )

    $gitCurrent = Join-Path $Manifest.ScoopRoot "apps/git/current"
    if (-not (Test-Path $gitCurrent) -and $PSCmdlet.ShouldProcess("main/git", "Install bootstrap dependency globally")) {
        Invoke-Scoop $Scoop @("install", "--global", "main/git")
    }

    $bucketRoot = Join-Path $Manifest.ScoopRoot "Core/buckets"
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
    $availableManifest = if ($selfHosted) {
        Join-Path $Manifest.ScoopRoot "Core/buckets/winmoi/bucket/$name.json"
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

    if ($UpdatePackages -and $PSCmdlet.ShouldProcess("Scoop and its buckets", "Update manifests before package updates")) {
        Invoke-Scoop $Scoop @("update", "scoop")
        foreach ($bucket in $Manifest.Buckets) {
            $bucketPath = Join-Path $Manifest.ScoopRoot "Core/buckets/$($bucket.Name)"
            if (Test-Path $bucketPath) {
                git -C $bucketPath fetch origin
                if ($LASTEXITCODE -ne 0) {
                    throw "Failed to fetch Scoop bucket '$($bucket.Name)'."
                }
                $upstream = git -C $bucketPath rev-parse --abbrev-ref "@{upstream}"
                if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($upstream)) {
                    throw "Failed to resolve the upstream branch for Scoop bucket '$($bucket.Name)'."
                }
                git -C $bucketPath reset --hard $upstream
                if ($LASTEXITCODE -ne 0) {
                    throw "Failed to reset Scoop bucket '$($bucket.Name)' to '$upstream'."
                }
            }
        }
    }

    foreach ($package in $Manifest.Packages) {
        $resolvedPackage = Get-ScoopPackage $Manifest $package
        $current = Join-Path $Manifest.ScoopRoot "apps/$($resolvedPackage.Name)/current"

        if (-not (Test-Path $current)) {
            if ($PSCmdlet.ShouldProcess($package, "Install globally in $($Manifest.ScoopRoot)")) {
                Invoke-Scoop $Scoop @("install", "--global", $resolvedPackage.Source)
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
            if ($installedVersion -ne $availableVersion -and $PSCmdlet.ShouldProcess($package, "Update globally in $($Manifest.ScoopRoot)")) {
                Invoke-Scoop $Scoop @("update", "--global", $resolvedPackage.Source)
            }
            else {
                Write-Host "Self-hosted package '$package' is current at version '$installedVersion'."
            }
        }
        elseif ($UpdatePackages -and $PSCmdlet.ShouldProcess($package, "Update globally in $($Manifest.ScoopRoot)")) {
            Invoke-Scoop $Scoop @("update", "--global", $resolvedPackage.Source)
        }
        else {
            Write-Host "Package '$package' is already installed in '$current'."
        }

        if (-not $WhatIfPreference -and (Test-Path $Scoop) -and -not (Test-Path $current)) {
            throw "Scoop did not create the expected package location '$current'."
        }
    }
}
