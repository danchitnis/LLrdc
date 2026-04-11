<#
.SYNOPSIS
Run the llrdc Docker container on Windows.
#>

$ErrorActionPreference = "Stop"

$ImageName = if ($env:IMAGE_NAME) { $env:IMAGE_NAME } else { "danchitnis/llrdc" }
$ImageTag = if ($env:IMAGE_TAG) { $env:IMAGE_TAG } else { "latest" }
$ContainerName = if ($env:CONTAINER_NAME) { $env:CONTAINER_NAME } else { "llrdc" }

$ServerPort = if ($env:PORT) { $env:PORT } else { "8080" }
$ServerFps = if ($env:FPS) { $env:FPS } else { "30" }
$ServerDisplayNum = if ($env:DISPLAY_NUM) { $env:DISPLAY_NUM } else { "99" }
$ServerVideoCodec = if ($env:VIDEO_CODEC) { $env:VIDEO_CODEC } else { "h264" }

$HostPort = if ($env:HOST_PORT) { $env:HOST_PORT } else { "8080" }
$ContainerPort = if ($env:CONTAINER_PORT) { $env:CONTAINER_PORT } else { $ServerPort }

$UseNvidia = if ($env:USE_NVIDIA) { $env:USE_NVIDIA } else { "false" }
$UseDebugX11 = "false"
$UseDebugFfmpeg = "false"
$WebrtcInterfaces = if ($env:WEBRTC_INTERFACES) { $env:WEBRTC_INTERFACES } else { "" }
$WebrtcExcludeInterfaces = if ($env:WEBRTC_EXCLUDE_INTERFACES) { $env:WEBRTC_EXCLUDE_INTERFACES } else { "" }

$i = 0
while ($i -lt $args.Length) {
    switch ($args[$i]) {
        "--nvidia" {
            $UseNvidia = "true"
        }
        "--debug-ffmpeg" {
            $UseDebugFfmpeg = "true"
        }
        "--debug" {
            $UseDebugX11 = "true"
            $UseDebugFfmpeg = "true"
        }
        { $_ -in "--iface", "-i" } {
            $i++
            if ($i -lt $args.Length) {
                $WebrtcInterfaces = $args[$i]
            } else {
                Write-Error "Error: --iface requires an argument."
                exit 1
            }
        }
        { $_ -in "--exclude-iface", "-x" } {
            $i++
            if ($i -lt $args.Length) {
                $WebrtcExcludeInterfaces = $args[$i]
            } else {
                Write-Error "Error: --exclude-iface requires an argument."
                exit 1
            }
        }
    }
    $i++
}

$GpuArgs = @()
if ($UseNvidia -eq "true") {
    $ServerVideoCodec = "h264_nvenc"
    $GpuArgs = @("--gpus", "all", "-e", "NVIDIA_DRIVER_CAPABILITIES=all")
}

# Determine number of CPUs
$NumCpus = (Get-CimInstance Win32_ComputerSystem).NumberOfLogicalProcessors
if (-not $NumCpus) { $NumCpus = 1 }
$CpuList = "0-$($NumCpus - 1)"

Write-Host "▶ Starting container: $ContainerName"
Write-Host "  Image : ${ImageName}:${ImageTag}"
Write-Host "  Port  : $HostPort -> $ContainerPort"
Write-Host "  CPUs  : $NumCpus (cores $CpuList)"

if ($env:USE_DEBUG -eq "true" -or $UseDebugX11 -eq "true" -or $UseDebugFfmpeg -eq "true") {
    Write-Host "  FPS   : $ServerFps"
}
if ($UseNvidia -eq "true") {
    Write-Host "  GPU   : Enabled (Codec: $ServerVideoCodec)"
}

$InteractiveArgs = @()
if ([Console]::IsInputRedirected -eq $false) {
    $InteractiveArgs = @("--interactive", "--tty")
}

# Find WebRTC public IP
$WebrtcPublicIp = $env:WEBRTC_PUBLIC_IP
if (-not $WebrtcPublicIp) {
    if ($WebrtcInterfaces) {
        $ifaceName = $WebrtcInterfaces.Split(',')[0]
        $ipInfo = Get-NetIPAddress -InterfaceAlias $ifaceName -AddressFamily IPv4 -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($ipInfo) {
            $WebrtcPublicIp = $ipInfo.IPAddress
        }
    }
    
    if (-not $WebrtcPublicIp) {
        try {
            $route = Find-NetRoute -RemoteIPAddress "8.8.8.8" -ErrorAction SilentlyContinue | Sort-Object RouteMetric | Select-Object -First 1
            if ($route) {
                $ipInfo = Get-NetIPAddress -InterfaceIndex $route.InterfaceIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Select-Object -First 1
                if ($ipInfo) {
                    $WebrtcPublicIp = $ipInfo.IPAddress
                }
            }
        } catch {
            # Ignore errors during auto-detection
        }
    }
}

$displayIp = if ($WebrtcPublicIp) { $WebrtcPublicIp } else { "none" }
Write-Host "  WebRTC IP : $displayIp (auto-detected)"

$DockerArgs = @(
    "run",
    "--rm"
)
$DockerArgs += $GpuArgs
$DockerArgs += $InteractiveArgs
$DockerArgs += @(
    "--name", $ContainerName,
    "--publish", "${HostPort}:${ContainerPort}/tcp",
    "--publish", "${HostPort}:${ContainerPort}/udp",
    "--shm-size", "256m",
    "--cpuset-cpus", $CpuList,
    "--ulimit", "rtprio=99",
    "--cap-add=SYS_NICE",
    "--env", "PORT=$ServerPort",
    "--env", "FPS=$ServerFps",
    "--env", "VIDEO_CODEC=$ServerVideoCodec",
    "--env", "USE_NVIDIA=$UseNvidia",
    "--env", "TEST_PATTERN=$($env:TEST_PATTERN)",
    "--env", "WEBRTC_PUBLIC_IP=$WebrtcPublicIp",
    "--env", "WEBRTC_INTERFACES=$WebrtcInterfaces",
    "--env", "WEBRTC_EXCLUDE_INTERFACES=$WebrtcExcludeInterfaces",
    "--env", "USE_DEBUG_FFMPEG=$UseDebugFfmpeg",
    "--env", "HOST_UID=1000",
    "${ImageName}:${ImageTag}"
)

# Execute Docker
& docker $DockerArgs
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
