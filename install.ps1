[CmdletBinding()]
param(
    [string]$Version = $(if ($env:CRONCTL_VERSION) { $env:CRONCTL_VERSION } else { "latest" }),
    [string]$InstallDir = $(if ($env:CRONCTL_INSTALL_DIR) { $env:CRONCTL_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\cronctl" })
)

$ErrorActionPreference = "Stop"
$repo = "R44VC0RP/cronctl"

$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
switch ($architecture) {
    "X64" { $arch = "amd64" }
    "Arm64" { $arch = "arm64" }
    default { throw "cronctl installer: unsupported architecture: $architecture" }
}

$asset = "cronctl-windows-$arch.zip"
if ($env:CRONCTL_DOWNLOAD_BASE) {
    $downloadBase = $env:CRONCTL_DOWNLOAD_BASE.TrimEnd("/")
} elseif ($Version -eq "latest") {
    $downloadBase = "https://github.com/$repo/releases/latest/download"
} else {
    $tag = if ($Version.StartsWith("v")) { $Version } else { "v$Version" }
    $downloadBase = "https://github.com/$repo/releases/download/$tag"
}

$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("cronctl-install-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tempDir | Out-Null

try {
    $archivePath = Join-Path $tempDir $asset
    $checksumsPath = Join-Path $tempDir "SHA256SUMS"

    Write-Host "Downloading $asset..."
    Invoke-WebRequest -Uri "$downloadBase/$asset" -OutFile $archivePath
    Invoke-WebRequest -Uri "$downloadBase/SHA256SUMS" -OutFile $checksumsPath

    $checksumPattern = "(?im)^([a-f0-9]{64})\s+\*?" + [regex]::Escape($asset) + "\s*$"
    $checksumMatch = [regex]::Match((Get-Content -Raw $checksumsPath), $checksumPattern)
    if (-not $checksumMatch.Success) {
        throw "cronctl installer: $asset is missing from SHA256SUMS"
    }

    $expected = $checksumMatch.Groups[1].Value.ToLowerInvariant()
    $actual = (Get-FileHash -Algorithm SHA256 $archivePath).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "cronctl installer: checksum verification failed for $asset"
    }

    Expand-Archive -Path $archivePath -DestinationPath $tempDir -Force
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Copy-Item (Join-Path $tempDir "cronctl.exe") (Join-Path $InstallDir "cronctl.exe") -Force
    Copy-Item (Join-Path $tempDir "cronctl-daemon.exe") (Join-Path $InstallDir "cronctl-daemon.exe") -Force

    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $pathParts = @($userPath -split ";" | Where-Object { $_ })
    if ($pathParts -notcontains $InstallDir) {
        $newUserPath = (($pathParts + $InstallDir) -join ";")
        [Environment]::SetEnvironmentVariable("Path", $newUserPath, "User")
        $env:Path = "$env:Path;$InstallDir"
        Write-Host "Added $InstallDir to your user PATH. Open a new terminal to use it there."
    }

    Write-Host "Installed cronctl.exe and cronctl-daemon.exe to $InstallDir"
    Write-Host "Run 'cronctl service install' when you are ready to start the scheduler."
} finally {
    Remove-Item -Recurse -Force $tempDir -ErrorAction SilentlyContinue
}
