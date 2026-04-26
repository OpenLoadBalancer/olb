# OpenLoadBalancer install script for Windows
# Usage:
#   Binary:  irm https://raw.githubusercontent.com/openloadbalancer/olb/main/install.ps1 | iex
#   Docker: irm https://raw.githubusercontent.com/openloadbalancer/olb/main/install.ps1 | iex -InstallMethod Docker
param(
    [ValidateSet("Binary", "Docker")]
    [string]$InstallMethod = "Binary"
)

$ErrorActionPreference = "Stop"

$Repo = "openloadbalancer/olb"
$Binary = "olb"
$Image = "ghcr.io/$Repo:latest"
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:ProgramFiles\OpenLoadBalancer" }

function Install-Binary {
    param([string]$Tag, [string]$Arch)

    Write-Host "[info] Installing OpenLoadBalancer $Tag for windows-$Arch" -ForegroundColor Cyan

    $Filename = "${Binary}-windows-${Arch}.exe"
    $Url = "https://github.com/$Repo/releases/download/$Tag/$Filename"

    $TmpDir = [System.IO.Path]::GetTempPath()
    $Target = Join-Path $TmpDir "$Binary.exe"

    Write-Host "[info] Downloading $Url ..."
    try {
        Invoke-WebRequest -Uri $Url -OutFile $Target -ErrorAction Stop
    } catch {
        Write-Host "[error] Download failed: $_" -ForegroundColor Red
        exit 1
    }

    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    $Dest = Join-Path $InstallDir "$Binary.exe"
    Move-Item -Path $Target -Destination $Dest -Force

    $Path = [Environment]::GetEnvironmentVariable("Path", "Machine")
    if ($Path -notlike "*$InstallDir*") {
        Write-Host "[info] Adding $InstallDir to system PATH..." -ForegroundColor Yellow
        [Environment]::SetEnvironmentVariable("Path", "$Path;$InstallDir", "Machine")
        $env:Path = "$env:Path;$InstallDir"
    }

    $VersionOutput = & $Dest version 2>$null | Select-Object -First 1
    Write-Host "[ok] Installed: $VersionOutput" -ForegroundColor Green
    Write-Host ""
    Write-Host "Run 'olb setup' to create an initial configuration, or:" -ForegroundColor Green
    Write-Host "  olb start --config olb.yaml" -ForegroundColor Green
}

function Install-Docker {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        Write-Host "[error] Docker is not installed. Install Docker Desktop from https://docker.com" -ForegroundColor Red
        exit 1
    }

    Write-Host "[info] Pulling $Image ..." -ForegroundColor Cyan
    docker pull $Image

    Write-Host ""
    Write-Host "[ok] Image pulled successfully" -ForegroundColor Green
    Write-Host ""
    Write-Host "Run with Docker:" -ForegroundColor Green
    Write-Host "  docker run -it --rm -p 8080:8080 -p 9090:9090 -v `$pwd/olb.yaml:/etc/olb/olb.yaml:ro $Image start" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "Or use docker-compose from:" -ForegroundColor Yellow
    Write-Host "  https://github.com/$Repo/tree/main/deploy" -ForegroundColor Cyan
}

# --- Detect architecture ---
$Arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "amd64" }

# --- Determine version ---
$Tag = $env:VERSION
if (-not $Tag) {
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -ErrorAction Stop
        $Tag = $release.tag_name
    } catch {
        Write-Host "[info] Could not query GitHub API. Using 'latest' tag." -ForegroundColor Yellow
        $Tag = "latest"
    }
}

switch ($InstallMethod) {
    "Binary" { Install-Binary -Tag $Tag -Arch $Arch }
    "Docker" { Install-Docker }
}
