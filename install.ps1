<#
.SYNOPSIS
    Install sbomb on Windows.

.DESCRIPTION
    irm https://andste82.github.io/sbomb/install.ps1 | iex
    &([scriptblock]::Create((irm https://andste82.github.io/sbomb/install.ps1))) -Version <sbomb-version>

    The checksum check is not optional and there is no switch to skip it. A
    tool whose whole argument is that you should be able to verify what you
    were given cannot hand you a binary it did not verify itself.

.PARAMETER Version
    Release to install, with or without the leading v. Default: latest.

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

# Fetch one file, telling the two kinds of failure apart.
#
# A 404 is an answer: the file is not in that release, and asking again will
# not change it. Anything else -- a 5xx, a reset connection, a timed-out
# request -- says nothing about the release and is worth another try. Reporting
# the second as the first is what makes a transient failure read as a broken
# release, and sends whoever hits it looking in the wrong place.
#
# -MaximumRetryCount would do the waiting, but it arrived in PowerShell 6 and
# this script has to run under Windows PowerShell 5.1 as well.
function Get-ReleaseFile([string]$Url, [string]$OutFile, [string]$What) {
    $attempts = 3
    for ($i = 1; $i -le $attempts; $i++) {
        try {
            Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing
            return
        } catch {
            $status = 0
            if ($_.Exception.PSObject.Properties['Response'] -and $_.Exception.Response) {
                try { $status = [int]$_.Exception.Response.StatusCode } catch { $status = 0 }
            }
            if ($status -eq 404) {
                Fail "$What is not there ($Url); check that the release exists and carries this file"
            }
            if ($i -eq $attempts) {
                $why = if ($status) { "HTTP $status" } else { $_.Exception.Message }
                Fail "$What could not be downloaded after $attempts attempts ($why); the release itself may be fine, so this is worth retrying"
            }
            Start-Sleep -Seconds (2 * $i)
        }
    }
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
    # Invoke-WebRequest's handling of -MaximumRedirection 0 is inconsistent
    # across PowerShell versions/hosts: sometimes it returns the 3xx response,
    # sometimes it throws a WebException carrying it, and sometimes it throws
    # an InvalidOperationException with no Response at all. HttpWebRequest
    # with AllowAutoRedirect off behaves the same way everywhere.
    $location = $null
    try {
        $req = [System.Net.HttpWebRequest]::Create("https://github.com/$repo/releases/latest")
        $req.AllowAutoRedirect = $false
        $req.Method = 'GET'
        $resp = $req.GetResponse()
        $location = $resp.Headers['Location']
        $resp.Close()
    } catch [System.Net.WebException] {
        if ($_.Exception.Response) {
            $location = $_.Exception.Response.Headers['Location']
        }
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
    # A tag carries a leading v; accepting it without saves a support round.
    $tag = "v$Version"
}

$asset = 'sbomb-windows-amd64.exe'
$base = "https://github.com/$repo/releases/download/$tag"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $tmp -Force | Out-Null

try {
    Write-Host "install.ps1: fetching sbomb $tag for windows-amd64"
    Get-ReleaseFile "$base/$asset" (Join-Path $tmp $asset) $asset
    Get-ReleaseFile "$base/SHA256SUMS" (Join-Path $tmp 'SHA256SUMS') 'SHA256SUMS'

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
        Get-ReleaseFile "$base/$asset.cdx.json" (Join-Path $BinDir 'sbomb.cdx.json') "$asset.cdx.json"
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
