<#
.SYNOPSIS
Build the llrdc Docker image on Windows.
#>

$ErrorActionPreference = "Stop"

$ImageName = if ($env:IMAGE_NAME) { $env:IMAGE_NAME } else { "danchitnis/llrdc" }
$ImageTag = if ($env:IMAGE_TAG) { $env:IMAGE_TAG } else { "latest" }

$ScriptDir = $PSScriptRoot
if (-not $ScriptDir) { $ScriptDir = "." }

Write-Host "▶ Building Docker image: ${ImageName}:${ImageTag}"
Write-Host "  Context: $ScriptDir"

# Default UID for the container user
$Uid = 1000

docker build --build-arg UID=$Uid --tag "${ImageName}:${ImageTag}" $ScriptDir

if ($LASTEXITCODE -ne 0) { throw "Docker build failed" }

Write-Host "✅ Build complete: ${ImageName}:${ImageTag}"
