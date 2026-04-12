<#
.SYNOPSIS
Build the llrdc Docker image on Windows.
#>

param(
    [switch]$Intel,
    [string]$Tag
)

$ErrorActionPreference = "Stop"

$ImageName = if ($env:IMAGE_NAME) { $env:IMAGE_NAME } else { "danchitnis/llrdc" }
$ImageTag = if ($env:IMAGE_TAG) { $env:IMAGE_TAG } else { "latest" }
$TagExplicit = $false

if ($env:IMAGE_TAG) { $TagExplicit = $true }
if ($PSBoundParameters.ContainsKey('Tag')) {
    $ImageTag = $Tag
    $TagExplicit = $true
}

$BuildVariant = "cpu"
$EnableIntel = "false"
if ($Intel) {
    $BuildVariant = "intel"
    $EnableIntel = "true"
    if (-not $TagExplicit) {
        $ImageTag = "intel"
    }
}

$ScriptDir = $PSScriptRoot
if (-not $ScriptDir) { $ScriptDir = "." }

Write-Host "▶ Building Docker image: ${ImageName}:${ImageTag}"
Write-Host "  Context: $ScriptDir"
Write-Host "  Variant: $BuildVariant"

# Default UID for the container user
$Uid = 1000

docker build --build-arg UID=$Uid --build-arg ENABLE_INTEL=$EnableIntel --build-arg BUILD_VARIANT=$BuildVariant --tag "${ImageName}:${ImageTag}" $ScriptDir

if ($LASTEXITCODE -ne 0) { throw "Docker build failed" }

Write-Host "✅ Build complete: ${ImageName}:${ImageTag}"
