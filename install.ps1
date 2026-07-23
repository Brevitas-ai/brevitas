<#
.SYNOPSIS
    Install the Brevitas CLI (bvx) on Windows.

.DESCRIPTION
    Downloads the prebuilt bvx release for your architecture, verifies its
    checksum, installs it to %LOCALAPPDATA%\Programs\bvx, and adds that folder
    to your user PATH. This installs the SAME binary Homebrew ships on
    macOS/Linux — it does not build from source and needs no Go toolchain.

    The optimization engine (brevitas-systems) is NOT installed here; run
    `bvx install` afterwards to sign in, detect your AI tools, and set it up.

.EXAMPLE
    irm https://raw.githubusercontent.com/Brevitas-ai/brevitas/main/install.ps1 | iex

.NOTES
    Set $env:BVX_VERSION (e.g. "0.1.19") to pin a specific version instead of
    installing the latest release.
#>

$ErrorActionPreference = 'Stop'

# Windows PowerShell 5.1 often defaults to TLS 1.0/1.1, which GitHub rejects.
# Force TLS 1.2 so the downloads below work everywhere (no-op on PowerShell 7).
try {
    [Net.ServicePointManager]::SecurityProtocol =
        [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
} catch { }

$Repo = 'Brevitas-ai/brevitas'
$InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\bvx'

function Write-Step($msg) { Write-Host "  $msg" }

Write-Host "Installing bvx (Brevitas)..." -ForegroundColor Cyan

# --- Detect architecture -------------------------------------------------
# Read the environment variables Windows always sets. This works on both
# Windows PowerShell 5.1 (.NET Framework) and PowerShell 7 — unlike
# [RuntimeInformation]::OSArchitecture, which can come back empty on 5.1.
# PROCESSOR_ARCHITEW6432 is set (to the native arch) when a 32-bit shell runs
# on a 64-bit OS under WOW64, so prefer it to still pick the native build.
# Override with $env:BVX_ARCH ("amd64" or "arm64") if detection ever misfires.
$archRaw = $env:BVX_ARCH
if (-not $archRaw) { $archRaw = $env:PROCESSOR_ARCHITEW6432 }
if (-not $archRaw) { $archRaw = $env:PROCESSOR_ARCHITECTURE }
if (-not $archRaw) {
    try { $archRaw = "$([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture)" } catch { }
}
# switch is case-insensitive, so AMD64/amd64/X64 and ARM64/arm64/Arm64 all match.
$arch = switch ($archRaw) {
    'AMD64' { 'amd64'; break }
    'x64'   { 'amd64'; break }
    'ARM64' { 'arm64'; break }
    default { throw "bvx: unsupported or undetected architecture '$archRaw' (need AMD64/x64 or ARM64). Set `$env:BVX_ARCH to 'amd64' or 'arm64' and retry." }
}

# --- Resolve version -----------------------------------------------------
$version = $env:BVX_VERSION
if (-not $version) {
    Write-Step "Resolving latest release..."

    # Do not use GitHub's REST API here. Anonymous requests are rate-limited per
    # public IP, so a shared office, VPN, or ISP can exhaust the quota for every
    # Windows user behind it. GitHub's normal latest-release URL redirects to
    # /releases/tag/vX.Y.Z and is not subject to that REST API quota.
    $request = [System.Net.WebRequest]::Create("https://github.com/$Repo/releases/latest")
    $request.Method = 'HEAD'
    $request.AllowAutoRedirect = $true
    $request.UserAgent = 'bvx-install'

    $response = $null
    try {
        $response = $request.GetResponse()
        $releaseUri = $response.ResponseUri
    }
    catch {
        throw "bvx: could not resolve the latest release from GitHub. Check HTTPS access to github.com or set `$env:BVX_VERSION to a published version and retry. $($_.Exception.Message)"
    }
    finally {
        if ($response) { $response.Close() }
    }

    $expectedPath = "/$Repo/releases/tag/"
    $tag = [System.IO.Path]::GetFileName($releaseUri.AbsolutePath)
    if ($releaseUri.Scheme -ne 'https' -or
        $releaseUri.Host -ne 'github.com' -or
        -not $releaseUri.AbsolutePath.StartsWith($expectedPath) -or
        $tag -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+(?:[-.][0-9A-Za-z.-]+)?$') {
        throw "bvx: GitHub returned an unexpected latest-release URL '$releaseUri'."
    }
    $version = $tag
}
$version = $version.TrimStart('v')
if ($version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+(?:[-.][0-9A-Za-z.-]+)?$') {
    throw "bvx: invalid BVX_VERSION '$version'. Expected a published semantic version such as 0.1.27."
}

$asset   = "bvx-$version-windows-$arch.zip"
$baseUrl = "https://github.com/$Repo/releases/download/v$version"
Write-Step "Version $version ($arch)"

# --- Download ------------------------------------------------------------
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("bvx-" + [System.Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
$zipPath = Join-Path $tmp $asset

try {
    Write-Step "Downloading $asset..."
    Invoke-WebRequest "$baseUrl/$asset" -OutFile $zipPath -UseBasicParsing

    # --- Verify checksum -------------------------------------------------
    # Fetching checksums.txt is best-effort (tolerate its absence), but if we
    # do get it, a mismatch is fatal. Keep the tolerant catch around ONLY the
    # download so a real mismatch below still aborts the install.
    $sums = $null
    try {
        $sums = (Invoke-WebRequest "$baseUrl/checksums.txt" -UseBasicParsing).Content
    } catch {
        Write-Step "checksums.txt unavailable; skipping verification."
    }
    if ($sums) {
        $expected = ($sums -split "`n" |
            Where-Object { $_ -match [regex]::Escape($asset) } |
            ForEach-Object { ($_ -split '\s+')[0] } | Select-Object -First 1)
        if ($expected) {
            $actual = (Get-FileHash $zipPath -Algorithm SHA256).Hash.ToLower()
            if ($actual -ne $expected.ToLower()) {
                throw "bvx: checksum mismatch for $asset (expected $expected, got $actual)"
            }
            Write-Step "Checksum verified."
        }
    }

    # --- Extract & install -----------------------------------------------
    Write-Step "Installing to $InstallDir..."
    Expand-Archive -Path $zipPath -DestinationPath $tmp -Force
    $exe = Get-ChildItem -Path $tmp -Filter 'bvx.exe' -Recurse | Select-Object -First 1
    if (-not $exe) { throw "bvx: bvx.exe not found in $asset" }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Copy-Item $exe.FullName (Join-Path $InstallDir 'bvx.exe') -Force
}
finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

# --- Add to user PATH ----------------------------------------------------
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $InstallDir) {
    $newPath = if ($userPath) { "$userPath;$InstallDir" } else { $InstallDir }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    Write-Step "Added $InstallDir to your user PATH."
}
# Make bvx usable in the current session too.
if (($env:Path -split ';') -notcontains $InstallDir) {
    $env:Path = "$env:Path;$InstallDir"
}

Write-Host ""
Write-Host "bvx $version installed." -ForegroundColor Green
Write-Host "Next step:  bvx install     # sign in, detect tools, configure, start"
Write-Host "(Open a new terminal if 'bvx' isn't found — PATH updates apply to new shells.)"
