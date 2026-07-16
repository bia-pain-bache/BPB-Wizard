$ErrorActionPreference = "Stop"

$Binary = "wizard.exe"
$InstallDir = "bpb-wizard"
$Arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }

if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64" -or $env:PROCESSOR_ARCHITEW6432 -eq "ARM64") {
    $Arch = "arm64"
}

$Archive = "BPB-Wizard-windows-$Arch.zip"
$DownloadUrl = "https://github.com/bia-pain-bache/BPB-Wizard/releases/latest/download/$Archive"
$WorkerUrl = "https://github.com/bia-pain-bache/BPB-Worker-Panel/releases/latest/download/worker.js"
$LatestVersion = (Invoke-RestMethod -Uri "https://raw.githubusercontent.com/bia-pain-bache/BPB-Wizard/main/VERSION").Trim()

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

$BinaryPath = Join-Path $InstallDir $Binary
$NeedsInstall = $false

if (Test-Path $BinaryPath) {
    $InstalledVersion = (& $BinaryPath --version).Trim()
    Write-Host "Installed version: $InstalledVersion"
    Write-Host "Latest version: $LatestVersion"

    if ($InstalledVersion -eq $LatestVersion) {
        Write-Host "Wizard is up to date."
    } else {
        Write-Host "Updating to version $LatestVersion..."
        $NeedsInstall = $true
    }
} else {
    Write-Host "Wizard not found here. Installing version $LatestVersion..."
    $NeedsInstall = $true
}

if ($NeedsInstall) {
    Write-Host "Downloading $Archive..."

    $httpClient = New-Object System.Net.Http.HttpClient
    try {
        $zipBytes = $httpClient.GetByteArrayAsync($DownloadUrl).GetAwaiter().GetResult()
    } finally {
        $httpClient.Dispose()
    }

    $zipStream = New-Object System.IO.MemoryStream(,$zipBytes)
    try {
        $zipArchive = [System.IO.Compression.ZipArchive]::new($zipStream, [System.IO.Compression.ZipArchiveMode]::Read)
        try {
            [System.IO.Compression.ZipFileExtensions]::ExtractToDirectory($zipArchive, (Resolve-Path $InstallDir), $true)
        } finally {
            $zipArchive.Dispose()
        }
    } finally {
        $zipStream.Dispose()
    }
}

Write-Host "Downloading worker.js..."
Invoke-WebRequest -Uri $WorkerUrl -OutFile (Join-Path $InstallDir "worker.js")

$env:LAUNCHED_BY_SCRIPT = "1"

Push-Location $InstallDir
try {
    & ".\$Binary"
} finally {
    Pop-Location
}