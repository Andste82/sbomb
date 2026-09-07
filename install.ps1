<#
.SYNOPSIS
    Install sbomb on Windows.

.DESCRIPTION
    irm https://andste82.github.io/sbomb/install.ps1 | iex
    &([scriptblock]::Create((irm https://andste82.github.io/sbomb/install.ps1))) -Version v0.12.0

    The checksum check is not optional and there is no switch to skip it. A
    tool whose whole argument is that you should be able to verify what you
    were given cannot hand you a binary it did not verify itself.

.PARAMETER Version
    Release to install, e.g. v0.12.0 or 0.12.0. Default: latest.

.PARAMETER BinDir
    Where to put sbomb.exe. Default: %LOCALAPPDATA%\sbomb\bin.

.PARAMETER WithSbom
    Also install the release's own CycloneDX document beside the binary.
#>
[CmdletBinding()]
param(
    [string]$Version = $(if ($env:SBOMB_VERSION) { $env:SBOMB_VERSION } else { 'latest' }),
    [string]$BinDir = $(if ($env:SBOMB_BIN_DIR) { $env:SBOMB_BIN_DIR } else { Join-Path $env:LOCALAPPDATA 'sbomb\bin' }),
    [switch]$WithSbom
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'   # the progress bar makes iwr crawl

$repo = 'Andste82/sbomb'

function Fail([string]$message) {
    Write-Error "install.ps1: $message"
    exit 1
}

# Only amd64 is published for Windows. Saying so beats a 404 on a URL that
# could never have existed.
$arch = $env:PROCESSOR_ARCHITECTURE
if ($arch -ne 'AMD64') {
    Fail "no Windows release for $arch; sbomb publishes windows-amd64, and linux/darwin through install.sh"
}

# "latest" is resolved from where /releases/latest redirects to, not from the
# API. The API is rate limited per IP for unauthenticated callers, which a
# shared CI runner or an office behind one address will hit through no fault of
# its own -- and an installer that fails because somebody else installed too
# often is not an installer. The redirect has no such limit and needs no token.
if ($Version -eq 'latest') {
    $location = $null
    try {
        # Windows PowerShell returns the 3xx response here...
        $response = Invoke-WebRequest -Uri "https://github.com/$repo/releases/latest" `
            -UseBasicParsing -MaximumRedirection 0 -ErrorAction Stop
        $location = $response.Headers.Location
    } catch {
        # ...and PowerShell 7 throws it instead. Both carry the same header.
        $location = $_.Exception.Response.Headers.Location
    }
    $location = [string]$location
    if ($location -match '/releases/tag/(.+)$') {
        $tag = $Matches[1]
    } else {
        Fail 'could not work out the latest version; pass -Version to name one'
    }
} elseif ($Version.StartsWith('v')) {
    $tag = $Version
} else {
    # A tag is written v0.12.0; accepting 0.12.0 as well saves a support round.
    $tag = "v$Version"
}

$asset = 'sbomb-windows-amd64.exe'
$base = "https://github.com/$repo/releases/download/$tag"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $tmp -Force | Out-Null

try {
    Write-Host "install.ps1: fetching sbomb $tag for windows-amd64"
    try {
        Invoke-WebRequest -Uri "$base/$asset" -OutFile (Join-Path $tmp $asset) -UseBasicParsing
    } catch {
        Fail "could not download $asset for ${tag}; check that the release exists at https://github.com/$repo/releases"
    }
    try {
        Invoke-WebRequest -Uri "$base/SHA256SUMS" -OutFile (Join-Path $tmp 'SHA256SUMS') -UseBasicParsing
    } catch {
        Fail 'the release has no SHA256SUMS, so the download cannot be verified; refusing to install'
    }

    $actual = (Get-FileHash -Path (Join-Path $tmp $asset) -Algorithm SHA256).Hash.ToLower()
    $expected = $null
    foreach ($line in Get-Content (Join-Path $tmp 'SHA256SUMS')) {
        # "<64 hex>  <name>", with an optional * marking a binary read.
        if ($line -match "^([0-9a-f]{64})\s+\*?$([regex]::Escape($asset))$") {
            $expected = $Matches[1].ToLower()
            break
        }
    }
    if (-not $expected) { Fail "SHA256SUMS names no checksum for $asset" }
    if ($actual -ne $expected) { Fail "checksum mismatch for ${asset}: expected $expected, got $actual" }
    Write-Host 'install.ps1: checksum verified'

    New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
    $target = Join-Path $BinDir 'sbomb.exe'
    Copy-Item -Path (Join-Path $tmp $asset) -Destination $target -Force

    if ($WithSbom) {
        Invoke-WebRequest -Uri "$base/$asset.cdx.json" -OutFile (Join-Path $BinDir 'sbomb.cdx.json') -UseBasicParsing
        Write-Host "install.ps1: SBOM written to $(Join-Path $BinDir 'sbomb.cdx.json')"
    }

    $reported = & $target version
    Write-Host "install.ps1: installed $reported to $target"
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

# The user's PATH is theirs. Say what to do rather than editing it from a
# script they piped in from the network.
$onPath = ($env:PATH -split ';') -contains $BinDir
if (-not $onPath) {
    Write-Warning "$BinDir is not on your PATH. To add it for future sessions:"
    Write-Warning "    setx PATH `"$BinDir;`$env:PATH`""
}
