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

$Repo = 'Brevitas-ai/brevitas'
$InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\bvx'

function Write-Step($msg) { Write-Host "  $msg" }

Write-Host "Installing bvx (Brevitas)..." -ForegroundColor Cyan

# --- Detect architecture -------------------------------------------------
# Use the real OS architecture so an x64 PowerShell running under emulation on
# an ARM64 machine still gets the native build.
$osArch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
$arch = switch ("$osArch") {
    'X64'   { 'amd64' }
    'Arm64' { 'arm64' }
    default { throw "bvx: unsupported architecture '$osArch' (need X64 or Arm64)" }
}

# --- Resolve version -----------------------------------------------------
$version = $env:BVX_VERSION
if (-not $version) {
    Write-Step "Resolving latest release..."
    $release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest" `
        -Headers @{ 'User-Agent' = 'bvx-install' }
    $version = $release.tag_name
}
$version = $version.TrimStart('v')

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

    # --- Verify checksum (best-effort; skips if checksums.txt is absent) --
    try {
        $sums = (Invoke-WebRequest "$baseUrl/checksums.txt" -UseBasicParsing).Content
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
    } catch [System.Net.WebException] {
        Write-Step "checksums.txt not found; skipping verification."
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
