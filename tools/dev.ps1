#Requires -Version 7.4
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('setup', 'setup-browser', 'up', 'release', 'doctor', 'format', 'check-go', 'check-web', 'security', 'build', 'check', 'test-browser', 'install-hooks')]
    [string]$Command = 'check',
    # up only: the port the local service listens on, and an escape hatch for
    # an environment where opening a browser is unwanted.
    [int]$Port = 8765,
    [switch]$NoBrowser,
    # release only: the version stamped into the binary. Defaults to what
    # git describes, so a local build is never mistaken for a tagged one.
    [string]$Version = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$RepositoryRoot = Split-Path -Parent $PSScriptRoot
if (-not $env:PLAYWRIGHT_BROWSERS_PATH) {
    $env:PLAYWRIGHT_BROWSERS_PATH = Join-Path $RepositoryRoot '.cache/browsers'
}
$GoCommand = Get-Command go -ErrorAction SilentlyContinue
if ($null -eq $GoCommand) {
    $GoFallback = 'C:\Program Files\Go\bin\go.exe'
    if (-not (Test-Path -LiteralPath $GoFallback -PathType Leaf)) {
        throw 'Go is unavailable. Install the toolchain declared in go.mod.'
    }
    $GoExecutable = $GoFallback
} else {
    $GoExecutable = $GoCommand.Source
}
$GoFmtExecutable = Join-Path (Split-Path -Parent $GoExecutable) 'gofmt.exe'
if (-not (Test-Path -LiteralPath $GoFmtExecutable -PathType Leaf)) {
    $GoFmtExecutable = 'gofmt'
}

function Invoke-Checked {
    param(
        [Parameter(Mandatory)] [string]$Executable,
        [Parameter(Mandatory)] [string[]]$CommandArguments
    )
    # Diagnostic output must survive callers that capture a returned build path.
    & $Executable @CommandArguments | Out-Host
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed with exit code ${LASTEXITCODE}: $Executable $($CommandArguments -join ' ')"
    }
}

function Remove-RoutevaneGeneratedDirectory {
    param([Parameter(Mandatory)][string]$Path)
    $CacheRoot = [IO.Path]::GetFullPath((Join-Path $RepositoryRoot '.cache'))
    $Resolved = [IO.Path]::GetFullPath($Path)
    $Prefix = $CacheRoot + [IO.Path]::DirectorySeparatorChar
    if (-not $Resolved.StartsWith($Prefix, [StringComparison]::OrdinalIgnoreCase)) {
        throw "refusing generated-directory removal outside the cache: $Resolved"
    }
    $Ancestor = $Resolved
    while ($Ancestor.Length -ge $CacheRoot.Length) {
        if (Test-Path -LiteralPath $Ancestor) {
            if ((Get-Item -LiteralPath $Ancestor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) {
                throw "refusing linked cache directory: $Ancestor"
            }
        }
        $Ancestor = Split-Path -Parent $Ancestor
    }
    if (Test-Path -LiteralPath $Resolved) { Remove-Item -LiteralPath $Resolved -Recurse -Force }
}

function Test-RoutevaneOrigin {
    param([Parameter(Mandatory)] [string]$Origin)

    try {
        $Health = Invoke-RestMethod -Method Get -Uri "${Origin}/health" -TimeoutSec 2
        if ($Health.status -ne 'ok') { return $false }
        $Page = Invoke-WebRequest -UseBasicParsing -Uri $Origin -TimeoutSec 2
        $Digest = $Page.Headers['X-Routevane-UI-Digest']
        return $Page.StatusCode -eq 200 -and $Digest -match '^sha256-[a-f0-9]{64}$'
    } catch {
        return $false
    }
}

function Open-RoutevaneOrigin {
    param([Parameter(Mandatory)] [string]$Origin)

    if ($NoBrowser) { return }
    try { Start-Process $Origin } catch { }
}

function Invoke-GoSecurity {
    # Ask Go which packages belong to this module. Gosec's own ./... walk also
    # enters ignored caches and nested checkouts, unlike Go's package resolver.
    $PackageDirectories = @(& $GoExecutable list '-f' '{{.Dir}}' './...')
    if ($LASTEXITCODE -ne 0 -or $PackageDirectories.Count -eq 0) {
        throw 'unable to resolve the module packages for security scanning'
    }
    Invoke-Checked $GoExecutable (@('tool', 'gosec', '-quiet', '-nosec-require-justification') + $PackageDirectories)
    Invoke-Checked $GoExecutable @('tool', 'govulncheck', './...')
}

function Invoke-GoCheck {
    $GoRoots = @('cmd', 'sdk', 'examples' | ForEach-Object { Join-Path $RepositoryRoot $_ })
    $InternalRoot = Join-Path $RepositoryRoot 'internal'
    if (Test-Path -LiteralPath $InternalRoot -PathType Container) {
        $GoRoots += $InternalRoot
    }
    $GoFiles = Get-ChildItem -LiteralPath $GoRoots -Filter '*.go' -File -Recurse | ForEach-Object { $_.FullName }
    $Unformatted = @(& $GoFmtExecutable -l @GoFiles)
    if ($LASTEXITCODE -ne 0) { throw 'gofmt failed' }
    if ($Unformatted.Count -gt 0) { throw "gofmt is required: $($Unformatted -join ', ')" }

    Invoke-Checked $GoExecutable @('mod', 'tidy', '-diff')
    Invoke-Checked $GoExecutable @('mod', 'download')
    Invoke-Checked $GoExecutable @('mod', 'verify')
    Invoke-Checked $GoExecutable @('vet', './...')
    Invoke-Checked $GoExecutable @('tool', 'staticcheck', './...')

    $CoverageRoot = Join-Path $RepositoryRoot '.cache/reports'
    New-Item -ItemType Directory -Force -Path $CoverageRoot | Out-Null
    Invoke-Checked $GoExecutable @('test', '-shuffle=on', '-covermode=atomic', '-coverprofile', (Join-Path $CoverageRoot 'go.cover'), './...')
    Invoke-Checked $GoExecutable @('tool', 'cover', '-func', (Join-Path $CoverageRoot 'go.cover'))
    Invoke-GoSecurity

    $BuildRoot = Join-Path $RepositoryRoot '.cache\build'
    New-Item -ItemType Directory -Force -Path $BuildRoot | Out-Null
    $BinaryName = if ($IsWindows) { 'routing-agent.exe' } else { 'routing-agent' }
    Invoke-Checked $GoExecutable @('build', '-trimpath', '-buildvcs=true', '-mod=readonly', '-o', (Join-Path $BuildRoot $BinaryName), './cmd/routing-agent')
}

function Get-RoutevanePublicManifest {
    param([Parameter(Mandatory)] [string]$PublicRoot)

    $root = (Resolve-Path -LiteralPath $PublicRoot -ErrorAction Stop).Path
    $prefix = $root.TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    return @(
        Get-ChildItem -LiteralPath $root -File -Recurse -Force |
            Sort-Object FullName |
            ForEach-Object {
                if (($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                    throw "Generated UI contains a reparse-point file: $($_.FullName)"
                }
                $relative = $_.FullName.Substring($prefix.Length).Replace('\', '/')
                "$(($hash = Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant())  $relative"
            }
    )
}

function Write-RoutevaneGeneratedAsset {
    param(
        [Parameter(Mandatory)] [string]$PublicRoot,
        [Parameter(Mandatory)] [string]$Prefix,
        [Parameter(Mandatory)] [string]$Extension,
        [Parameter(Mandatory)] [string]$Content
    )

    $GeneratedRoot = Join-Path $PublicRoot '_routevane'
    New-Item -ItemType Directory -Force -Path $GeneratedRoot | Out-Null
    $Encoding = [Text.UTF8Encoding]::new($false)
    $Bytes = $Encoding.GetBytes($Content)
    $Hash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($Bytes)).ToLowerInvariant()
    $Name = "${Prefix}.${Hash}.${Extension}"
    [IO.File]::WriteAllBytes((Join-Path $GeneratedRoot $Name), $Bytes)
    return "/_routevane/$Name"
}

function Prepare-RoutevaneGeneratedUI {
    $PublicRoot = Join-Path $RepositoryRoot 'web\.output\public'
    $IndexPath = Join-Path $PublicRoot 'index.html'
    if (-not (Test-Path -LiteralPath $IndexPath -PathType Leaf)) {
        throw 'Nuxt did not produce an index.html for CSP preparation.'
    }
    $Html = Get-Content -LiteralPath $IndexPath -Raw
    # The build must not emit an entry import map (experimental.entryImportMap
    # is off): rewriting "#entry" inside content-hashed chunks would change
    # their bytes under unchanged names and break the immutable asset cache.
    if ($Html -match '<script type="importmap">') {
        throw 'Generated HTML contains an import map; disable experimental.entryImportMap.'
    }
    foreach ($Script in Get-ChildItem -LiteralPath (Join-Path $PublicRoot '_nuxt') -File -Recurse -Filter '*.js') {
        if ((Get-Content -LiteralPath $Script.FullName -Raw) -match '#entry') {
            throw "Generated Nuxt script references the import map: $($Script.FullName)"
        }
    }
    $BootScripts = [regex]::Matches($Html, '<script>(?<content>window\.__NUXT__=[\s\S]*?)</script>')
    if ($BootScripts.Count -ne 1) {
        throw 'Generated HTML must contain exactly one Nuxt bootstrap script.'
    }
    $BootURI = Write-RoutevaneGeneratedAsset -PublicRoot $PublicRoot -Prefix 'nuxt-bootstrap' -Extension 'js' -Content $BootScripts[0].Groups['content'].Value
    $Html = $Html.Replace($BootScripts[0].Value, ('<script src="' + $BootURI + '"></script>'))
    $PayloadScripts = [regex]::Matches($Html, '<script(?<before>[^>]*)type="application/json"(?<after>[^>]*)>(?<content>[\s\S]*?)</script>')
    foreach ($PayloadScript in $PayloadScripts) {
        $Attributes = ($PayloadScript.Groups['before'].Value + $PayloadScript.Groups['after'].Value).Trim()
        $Replacement = '<div hidden ' + $Attributes + '>' + $PayloadScript.Groups['content'].Value + '</div>'
        $Html = $Html.Replace($PayloadScript.Value, $Replacement)
    }
    if ($Html -match '<script(?![^>]*\ssrc=)' -or $Html -match '<style' -or $Html -match '\sstyle=' -or $Html -match '#entry' -or $Html -match 'https?://') {
        throw 'Generated HTML is incompatible with the strict self-only CSP.'
    }
    [IO.File]::WriteAllText($IndexPath, $Html, [Text.UTF8Encoding]::new($false))
}

function Sync-RoutevaneGeneratedUI {
    $PublicRoot = Join-Path $RepositoryRoot 'web\.output\public'
    $ExpectedTarget = Join-Path $RepositoryRoot 'internal\infrastructure\httpapi\ui'
    $MarkerName = 'routevane-ui.marker'
    $MarkerText = 'This tracked marker keeps the embedded generated UI directory present in a clean checkout.'
    if (-not (Test-Path -LiteralPath $PublicRoot -PathType Container) -or -not (Test-Path -LiteralPath (Join-Path $PublicRoot 'index.html') -PathType Leaf)) {
        throw 'Nuxt did not produce a public index.html for embedding.'
    }
    $TargetRoot = (Resolve-Path -LiteralPath $ExpectedTarget -ErrorAction Stop).Path
    $ExpectedRoot = [IO.Path]::GetFullPath($ExpectedTarget)
    if (-not [string]::Equals($TargetRoot, $ExpectedRoot, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to replace an unexpected generated-UI target: $TargetRoot"
    }
    $Marker = Join-Path $TargetRoot $MarkerName
    if (-not (Test-Path -LiteralPath $Marker -PathType Leaf) -or (Get-Content -LiteralPath $Marker -Raw).Trim() -ne $MarkerText) {
        throw 'Generated UI target marker is missing or invalid.'
    }
    foreach ($entry in Get-ChildItem -LiteralPath $TargetRoot -Force -Recurse) {
        if (($entry.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Generated UI target contains a reparse point: $($entry.FullName)"
        }
    }
    foreach ($entry in Get-ChildItem -LiteralPath $PublicRoot -Force -Recurse) {
        if (($entry.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Generated UI source contains a reparse point: $($entry.FullName)"
        }
    }
    $manifest = Get-RoutevanePublicManifest -PublicRoot $PublicRoot
    if ($manifest.Count -eq 0) {
        throw 'Nuxt public output is empty.'
    }
    Get-ChildItem -LiteralPath $TargetRoot -Force |
        Where-Object { $_.Name -ne $MarkerName } |
        ForEach-Object { Remove-Item -LiteralPath $_.FullName -Recurse -Force }
    Get-ChildItem -LiteralPath $PublicRoot -Force |
        ForEach-Object { Copy-Item -LiteralPath $_.FullName -Destination $TargetRoot -Recurse -Force }
    Write-Host "Synced $($manifest.Count) generated UI files into the embedded HTTP package."
    return $manifest
}

function Invoke-WebGenerateAndSync {
    Push-Location (Join-Path $RepositoryRoot 'web')
    try {
        Invoke-Checked 'npm' @('run', 'generate')
    } finally {
        Pop-Location
    }
    Prepare-RoutevaneGeneratedUI
    return Sync-RoutevaneGeneratedUI
}

function Get-RoutevaneBrowserPath {
    Push-Location (Join-Path $RepositoryRoot 'web')
    try {
        # playwright-core reports the exact browser revision this repository
        # pinned, so the Go tests and the Playwright tests never disagree about
        # which browser was exercised.
        $Path = & node '-e' 'console.log(require("playwright-core").chromium.executablePath())'
        if ($LASTEXITCODE -ne 0) { throw 'unable to resolve the Playwright Chromium path' }
    } finally {
        Pop-Location
    }
    $Path = $Path.Trim()
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Playwright Chromium is not installed at $Path; run tools/dev.ps1 setup-browser"
    }
    return $Path
}

function Invoke-WebCheck {
    Push-Location (Join-Path $RepositoryRoot 'web')
    try {
        Invoke-Checked 'npm' @('run', 'check')
    } finally {
        Pop-Location
    }
    Prepare-RoutevaneGeneratedUI
    return Sync-RoutevaneGeneratedUI
}

function Invoke-ProductBuild {
    Invoke-WebGenerateAndSync | Out-Null
    $BuildRoot = Join-Path $RepositoryRoot '.cache\build'
    New-Item -ItemType Directory -Force -Path $BuildRoot | Out-Null
    $BinaryName = if ($IsWindows) { 'routing-agent.exe' } else { 'routing-agent' }
    $Binary = Join-Path $BuildRoot $BinaryName
    Invoke-Checked $GoExecutable @('build', '-trimpath', '-buildvcs=true', '-mod=readonly', '-o', $Binary, './cmd/routing-agent')
    return $Binary
}

function Assert-ReleaseArchives {
    param(
        [Parameter(Mandatory = $true)][string]$ReleaseRoot,
        [Parameter(Mandatory = $true)][string]$ReleaseVersion,
        [Parameter(Mandatory = $true)][array]$Platforms,
        [Parameter(Mandatory = $true)][array]$Archives
    )
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $ChecksumPath = Join-Path $ReleaseRoot 'SHA256SUMS'
    $ExpectedChecksums = @{}
    foreach ($Line in Get-Content -LiteralPath $ChecksumPath) {
        if ($Line -notmatch '^([0-9a-f]{64})\s{2}(.+)$') { throw "invalid checksum line: $Line" }
        $ExpectedChecksums[$Matches[2]] = $Matches[1]
    }
    if ($Archives.Count -ne $Platforms.Count -or $ExpectedChecksums.Count -ne $Archives.Count) {
        throw "release inventory mismatch: platforms=$($Platforms.Count) archives=$($Archives.Count) checksums=$($ExpectedChecksums.Count)"
    }

    foreach ($Index in 0..($Platforms.Count - 1)) {
        $Platform = $Platforms[$Index]
        $ArchivePath = $Archives[$Index]
        $ArchiveName = Split-Path -Leaf $ArchivePath
        $ActualHash = (Get-FileHash -LiteralPath $ArchivePath -Algorithm SHA256).Hash.ToLower()
        if ($ExpectedChecksums[$ArchiveName] -ne $ActualHash) { throw "checksum mismatch: $ArchiveName" }

        $StemName = "routevane-$ReleaseVersion-$($Platform.OS)-$($Platform.Arch)"
        $Prefix = "$StemName/"
        $Binary = "$Prefix" + "routing-agent$($Platform.Suffix)"
        $Launcher = "$Prefix" + $(if ($Platform.OS -eq 'windows') { 'start-routevane.cmd' } else { 'start-routevane.sh' })
        $Zip = [System.IO.Compression.ZipFile]::OpenRead($ArchivePath)
        try {
            $Entries = @{}
            foreach ($Entry in $Zip.Entries) {
                $Name = $Entry.FullName.Replace('\', '/')
                if ($Entries.ContainsKey($Name)) { throw "$ArchiveName carries duplicate entries: $Name" }
                $Entries[$Name] = $Entry
            }
            $Expected = @($Binary, $Launcher, "${Prefix}README.txt", "${Prefix}LICENSE", "${Prefix}THIRD_PARTY_NOTICES.txt", "${Prefix}SBOM.spdx.json")
            $CatalogFiles = @(& git -C $RepositoryRoot ls-files -- catalog)
            if ($LASTEXITCODE -ne 0 -or $CatalogFiles.Count -eq 0) { throw 'tracked catalog inventory unavailable' }
            $Expected += @($CatalogFiles | ForEach-Object { "${Prefix}$_" })
            if ($Entries.Count -ne $Expected.Count) { throw "$ArchiveName carries unexpected or missing files" }
            foreach ($Required in $Expected) {
                if (-not $Entries.ContainsKey($Required)) { throw "$ArchiveName is missing $Required" }
                if ($Entries[$Required].Length -eq 0) { throw "$ArchiveName carries empty $Required" }
            }
            if ($Platform.OS -ne 'windows') {
                foreach ($Executable in @($Binary, $Launcher)) {
                    if ((($Entries[$Executable].ExternalAttributes -shr 16) -band 511) -ne 493) {
                        throw "$ArchiveName does not preserve executable permissions: $Executable"
                    }
                }
            }
            $LicenseReader = [System.IO.StreamReader]::new($Entries["${Prefix}LICENSE"].Open())
            try { $ArchivedLicense = $LicenseReader.ReadToEnd() } finally { $LicenseReader.Dispose() }
            $ExpectedLicense = Get-Content -LiteralPath (Join-Path $RepositoryRoot 'LICENSE') -Raw
            $NormalizedArchivedLicense = $ArchivedLicense.Replace("`r`n", "`n").TrimEnd()
            $NormalizedExpectedLicense = $ExpectedLicense.Replace("`r`n", "`n").TrimEnd()
            if ($NormalizedArchivedLicense -ne $NormalizedExpectedLicense) {
                throw "$ArchiveName carries a modified or empty Routevane LICENSE"
            }
            if ($Entries["${Prefix}THIRD_PARTY_NOTICES.txt"].Length -eq 0) {
                throw "$ArchiveName carries empty third-party notices"
            }
            if (@($Entries.Keys | Where-Object { $_ -like "${Prefix}catalog/targets/*.yaml" }).Count -eq 0) {
                throw "$ArchiveName carries no target catalog"
            }
            $Reader = [System.IO.StreamReader]::new($Entries["${Prefix}SBOM.spdx.json"].Open())
            try { $SBOM = ($Reader.ReadToEnd() | ConvertFrom-Json) } finally { $Reader.Dispose() }
            if ($SBOM.spdxVersion -ne 'SPDX-2.3' -or $SBOM.name -ne "routevane-$ReleaseVersion" -or $SBOM.packages.Count -lt 2) {
                throw "$ArchiveName carries invalid release SBOM metadata"
            }
        } finally {
            $Zip.Dispose()
        }
    }

    $NativeOS = if ($IsWindows) { 'windows' } elseif ($IsLinux) { 'linux' } elseif ($IsMacOS) { 'darwin' } else { '' }
    $NativeArch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
        ([System.Runtime.InteropServices.Architecture]::X64) { 'amd64' }
        ([System.Runtime.InteropServices.Architecture]::Arm64) { 'arm64' }
        default { '' }
    }
    $Native = $Platforms | Where-Object { $_.OS -eq $NativeOS -and $_.Arch -eq $NativeArch } | Select-Object -First 1
    if ($null -ne $Native) {
        $NativeStem = "routevane-$ReleaseVersion-$($Native.OS)-$($Native.Arch)"
        $NativeArchive = Join-Path $ReleaseRoot "$NativeStem.zip"
        Invoke-Checked 'python' @((Join-Path $RepositoryRoot 'tools/release_smoke.py'),
            '--archive', $NativeArchive, '--version', $ReleaseVersion,
            '--platform', "$($Native.OS)-$($Native.Arch)")
    }
    Write-Host "verified $($Archives.Count) release archives"
}

function Invoke-ReleaseBuild {
    # One generated control surface, then one archive per platform. The SQLite
    # driver is pure Go, so CGO stays off and every platform is cross-compiled
    # exactly rather than depending on the host toolchain.
    Invoke-WebGenerateAndSync | Out-Null
    $ReleaseVersion = $Version
    if ([string]::IsNullOrWhiteSpace($ReleaseVersion)) {
        $Described = & git -C $RepositoryRoot describe --tags --always --dirty 2>$null
        $ReleaseVersion = if ($LASTEXITCODE -eq 0 -and $Described) { $Described.Trim() } else { 'dev' }
    }
    $ReleaseRoot = Join-Path $RepositoryRoot '.cache\release'
    if ($ReleaseVersion -notmatch '^[a-zA-Z0-9][a-zA-Z0-9.+-]{0,99}$') { throw 'invalid local release version' }
    Remove-RoutevaneGeneratedDirectory -Path $ReleaseRoot
    New-Item -ItemType Directory -Force -Path $ReleaseRoot | Out-Null
    $SourceDate = (& git -C $RepositoryRoot show -s --format=%cI HEAD).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($SourceDate)) { throw 'release source date is unavailable' }
    $NoticesPath = Join-Path $ReleaseRoot 'THIRD_PARTY_NOTICES.txt'
    $SBOMPath = Join-Path $ReleaseRoot 'SBOM.spdx.json'
    Invoke-Checked 'python' @(
        (Join-Path $RepositoryRoot 'tools\release_metadata.py'),
        '--version', $ReleaseVersion,
        '--created', $SourceDate,
        '--notices', $NoticesPath,
        '--sbom', $SBOMPath)
    $Platforms = @(
        @{ OS = 'windows'; Arch = 'amd64'; Suffix = '.exe' },
        @{ OS = 'windows'; Arch = 'arm64'; Suffix = '.exe' },
        @{ OS = 'linux';   Arch = 'amd64'; Suffix = '' },
        @{ OS = 'linux';   Arch = 'arm64'; Suffix = '' },
        @{ OS = 'darwin';  Arch = 'arm64'; Suffix = '' }
    )
    $Archives = @()
    foreach ($Platform in $Platforms) {
        $StemName = "routevane-$ReleaseVersion-$($Platform.OS)-$($Platform.Arch)"
        $Stage = Join-Path $ReleaseRoot $StemName
        New-Item -ItemType Directory -Force -Path $Stage | Out-Null
        $Output = Join-Path $Stage "routing-agent$($Platform.Suffix)"
        $env:CGO_ENABLED = '0'
        $env:GOOS = $Platform.OS
        $env:GOARCH = $Platform.Arch
        try {
            Invoke-Checked $GoExecutable @(
                'build', '-trimpath', '-buildvcs=true', '-mod=readonly',
                '-ldflags', "-X main.version=$ReleaseVersion",
                '-o', $Output, './cmd/routing-agent')
        } finally {
            Remove-Item Env:\CGO_ENABLED, Env:\GOOS, Env:\GOARCH -ErrorAction SilentlyContinue
        }
        # The catalog travels with the binary. It is operator-editable data, so
        # embedding it would hide which copy is in effect.
        $CatalogPaths = @(& git -C $RepositoryRoot ls-files -- catalog)
        if ($LASTEXITCODE -ne 0 -or $CatalogPaths.Count -eq 0) { throw 'tracked catalog inventory unavailable' }
        foreach ($CatalogPath in $CatalogPaths) {
            $Destination = Join-Path $Stage $CatalogPath
            New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Destination) | Out-Null
            Copy-Item -LiteralPath (Join-Path $RepositoryRoot $CatalogPath) -Destination $Destination
        }
        Copy-Item -Force -Path (Join-Path $RepositoryRoot 'LICENSE') -Destination $Stage
        Copy-Item -Force -Path $NoticesPath -Destination $Stage
        Copy-Item -Force -Path $SBOMPath -Destination $Stage
        # Each archive carries the launcher for its own platform, so the product
        # starts from a double-click rather than from a remembered command.
        $LauncherRoot = Join-Path $RepositoryRoot 'tools\launchers'
        Copy-Item -LiteralPath (Join-Path $LauncherRoot 'README.txt') -Destination $Stage
        if ($Platform.OS -eq 'windows') {
            Copy-Item -Force -Path (Join-Path $LauncherRoot 'start-routevane.cmd') -Destination $Stage
        } else {
            $Launcher = Join-Path $Stage 'start-routevane.sh'
            Copy-Item -Force -Path (Join-Path $LauncherRoot 'start-routevane.sh') -Destination $Launcher
            if (-not $IsWindows) { & chmod '+x' $Launcher }
        }
        $Archive = Join-Path $ReleaseRoot "$StemName.zip"
        Invoke-Checked 'python' @((Join-Path $RepositoryRoot 'tools/release_archive.py'),
            '--stage', $Stage, '--output', $Archive,
            '--platform', "$($Platform.OS)-$($Platform.Arch)", '--created', $SourceDate)
        Remove-RoutevaneGeneratedDirectory -Path $Stage
        $Archives += $Archive
        Write-Host "packaged $StemName.zip"
    }
    # A checksum file is what lets someone verify the download they were given
    # rather than trusting the page it came from.
    $ChecksumPath = Join-Path $ReleaseRoot 'SHA256SUMS'
    $Checksums = foreach ($File in $Archives) {
        $Hash = (Get-FileHash -LiteralPath $File -Algorithm SHA256).Hash.ToLower()
        "$Hash  $(Split-Path -Leaf $File)"
    }
    Set-Content -LiteralPath $ChecksumPath -Value $Checksums -Encoding ascii
    Assert-ReleaseArchives -ReleaseRoot $ReleaseRoot -ReleaseVersion $ReleaseVersion -Platforms $Platforms -Archives $Archives
    Write-Host "checksums: $ChecksumPath"
    return $ReleaseRoot
}

function Get-RoutevaneGitCommonDirectory {
    $GitDirectory = (& git -C $RepositoryRoot rev-parse --git-common-dir).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($GitDirectory)) {
        throw 'Routevane is not a Git repository'
    }
    if (-not [System.IO.Path]::IsPathRooted($GitDirectory)) {
        $GitDirectory = Join-Path $RepositoryRoot $GitDirectory
    }
    return [System.IO.Path]::GetFullPath($GitDirectory)
}

function Assert-RoutevaneHooksCurrent {
    $HookRoot = Join-Path (Get-RoutevaneGitCommonDirectory) 'hooks'
    foreach ($HookName in @('pre-commit', 'commit-msg')) {
        $Tracked = Join-Path $RepositoryRoot ".githooks\$HookName"
        $Installed = Join-Path $HookRoot $HookName
        if (-not (Test-Path -LiteralPath $Installed -PathType Leaf)) {
            throw "Routevane hook copy is missing: $Installed. Run tools/dev.ps1 install-hooks."
        }
        $TrackedHash = (Get-FileHash -LiteralPath $Tracked -Algorithm SHA256).Hash
        $InstalledHash = (Get-FileHash -LiteralPath $Installed -Algorithm SHA256).Hash
        if ($TrackedHash -ne $InstalledHash) {
            throw "Routevane hook copy is stale: $Installed. Run tools/dev.ps1 install-hooks."
        }
    }
    Write-Host 'Routevane hook copies match the tracked entry points.'
}

function Install-Hooks {
    $HookRoot = Join-Path (Get-RoutevaneGitCommonDirectory) 'hooks'
    New-Item -ItemType Directory -Force -Path $HookRoot | Out-Null
    Copy-Item -LiteralPath (Join-Path $RepositoryRoot '.githooks\pre-commit') -Destination (Join-Path $HookRoot 'pre-commit') -Force
    Copy-Item -LiteralPath (Join-Path $RepositoryRoot '.githooks\commit-msg') -Destination (Join-Path $HookRoot 'commit-msg') -Force
    Assert-RoutevaneHooksCurrent
    Write-Host "Installed Routevane hook entry points in $HookRoot"
}

Push-Location $RepositoryRoot
try {
    switch ($Command) {
        'setup' {
            # Preflight before downloads so an unsupported host fails without
            # leaving partial setup state.
            Invoke-Checked 'python' @((Join-Path $RepositoryRoot 'tools\doctor.py'))
            Invoke-Checked $GoExecutable @('mod', 'download')
            Push-Location (Join-Path $RepositoryRoot 'web')
            try { Invoke-Checked 'npm' @('ci') } finally { Pop-Location }
            Invoke-Checked 'python' @((Join-Path $RepositoryRoot 'tools\doctor.py'))
        }
        'setup-browser' {
            Invoke-Checked 'python' @((Join-Path $RepositoryRoot 'tools/doctor.py'))
            Push-Location (Join-Path $RepositoryRoot 'web')
            try {
                if ($IsLinux) { Invoke-Checked 'npx' @('--no-install', 'playwright', 'install', '--with-deps', 'chromium') }
                else { Invoke-Checked 'npx' @('--no-install', 'playwright', 'install', 'chromium') }
            } finally { Pop-Location }
            Get-RoutevaneBrowserPath | Out-Null
        }
        'doctor' {
            Invoke-Checked 'python' @((Join-Path $RepositoryRoot 'tools\doctor.py'))
        }
        'format' {
            $GoRoots = @('cmd', 'sdk', 'examples' | ForEach-Object { Join-Path $RepositoryRoot $_ })
            if (Test-Path -LiteralPath (Join-Path $RepositoryRoot 'internal')) { $GoRoots += (Join-Path $RepositoryRoot 'internal') }
            $GoFiles = Get-ChildItem -LiteralPath $GoRoots -Filter '*.go' -File -Recurse | ForEach-Object { $_.FullName }
            Invoke-Checked $GoFmtExecutable (@('-w') + $GoFiles)
            Push-Location (Join-Path $RepositoryRoot 'web')
            try { Invoke-Checked 'npm' @('run', 'format') } finally { Pop-Location }
        }
        'check-go' { Invoke-GoCheck }
        'check-web' { Invoke-WebCheck | Out-Null }
        'security' {
            Invoke-GoSecurity
        }
        'build' { Invoke-ProductBuild | Out-Null }
        'release' { Invoke-ReleaseBuild | Out-Null }
        'up' {
            $Origin = "http://127.0.0.1:${Port}"
            if (Test-RoutevaneOrigin -Origin $Origin) {
                Write-Host ''
                Write-Host "Routevane is already running: ${Origin}"
                Write-Host 'Stop the existing window with Ctrl+C before rebuilding it.'
                Write-Host ''
                Open-RoutevaneOrigin -Origin $Origin
                break
            }
            # One step from a checkout to a working page. The control surface is
            # generated into the binary, so building it is what separates a
            # browser page from an API-only process.
            $Binary = Invoke-ProductBuild
            $CatalogDirectory = Join-Path $RepositoryRoot 'catalog'
            $DataDirectory = Join-Path $RepositoryRoot 'data'
            New-Item -ItemType Directory -Force -Path $DataDirectory | Out-Null
            Write-Host ''
            Write-Host "Routevane: ${Origin}"
            Write-Host 'Stop with Ctrl+C.'
            Write-Host ''
            $OpenBrowser = (-not $NoBrowser).ToString().ToLowerInvariant()
            Invoke-Checked $Binary @('serve', '--port', "$Port", '--catalog-dir', $CatalogDirectory, '--data-dir', $DataDirectory, "--open-browser=$OpenBrowser")
        }
        'check' {
            Assert-RoutevaneHooksCurrent
            Invoke-Checked 'python' @((Join-Path $RepositoryRoot 'tools\doctor.py'))
            Invoke-WebCheck | Out-Null
            Invoke-GoCheck
        }
        'test-browser' {
            Get-RoutevaneBrowserPath | Out-Null
            Invoke-ProductBuild | Out-Null
            Push-Location (Join-Path $RepositoryRoot 'web')
            try { Invoke-Checked 'npm' @('run', 'test:browser') } finally { Pop-Location }
            # The discovery browser is the same owned dependency the web gate
            # uses, so its Go tests belong to this command rather than to the
            # default gate, which must not require a browser binary.
            $env:ROUTEVANE_BROWSER = Get-RoutevaneBrowserPath
            try {
                Invoke-Checked $GoExecutable @('test', '-count=1', './internal/discovery/...', './cmd/routing-agent/...')
            } finally {
                Remove-Item Env:\ROUTEVANE_BROWSER -ErrorAction SilentlyContinue
            }
        }
        'install-hooks' { Install-Hooks }
    }
} finally {
    Pop-Location
}
