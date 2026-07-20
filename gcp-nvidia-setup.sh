#!/usr/bin/env bash
# gcp-nvidia-setup.sh — Automated GCP GPU direct-buffer setup script.
set -euo pipefail

# Text formatting helper functions
info() { echo -e "\e[32m[INFO]\e[0m $*"; }
warn() { echo -e "\e[33m[WARNING]\e[0m $*"; }
error() { echo -e "\e[31m[ERROR]\e[0m $*"; exit 1; }

# Ensure script is run with sudo/root privileges
if [ "$EUID" -ne 0 ]; then
  error "This script must be run as root. Please run with sudo: sudo $0"
fi

info "Starting GCP NVIDIA direct-buffer host preparation..."

# 1. Verify NVIDIA GPU exists
if ! lspci | grep -qi "nvidia"; then
  error "No NVIDIA GPU detected on this system via lspci!"
fi
info "NVIDIA GPU detected successfully."

# 2. Update package list
info "Updating apt package index..."
apt-get update -y || error "Failed to update package repositories."

# 3. Install Mesa and EGL development utilities
info "Installing required Mesa and EGL host libraries..."
apt-get install -y --no-install-recommends libegl-dev mesa-utils || error "Failed to install Mesa/EGL host utilities."

# 4. Detect and install the latest available closed-source NVIDIA server driver and allocator libraries
info "Querying latest available closed-source NVIDIA server driver..."
LATEST_DRIVER_VER=$(apt-cache search nvidia-driver- | grep -oE 'nvidia-driver-[0-9]+-server' | grep -oE '[0-9]+' | sort -rn | head -n 1 || echo "")
if [ -z "$LATEST_DRIVER_VER" ]; then
  LATEST_DRIVER_VER="580"
  warn "Could not auto-detect server driver version. Falling back to default: ${LATEST_DRIVER_VER}-server"
else
  info "Successfully auto-detected newest NVIDIA server driver version: ${LATEST_DRIVER_VER}-server"
fi

DRIVER_PKG="nvidia-driver-${LATEST_DRIVER_VER}-server"
EXTRA_PKG="libnvidia-extra-${LATEST_DRIVER_VER}-server"

info "Installing closed-source NVIDIA server driver (${DRIVER_PKG}) and GBM allocator (${EXTRA_PKG})..."
apt-get install -y --no-install-recommends "$DRIVER_PKG" "$EXTRA_PKG" || error "Failed to install NVIDIA driver metapackages."

# 5. Check/Enable modesetting inside GCP-specific GRUB configs
info "Configuring kernel modesetting for nvidia-drm..."
GRUB_OVERRIDE="/etc/default/grub.d/50-cloudimg-settings.cfg"
REBOOT_NEEDED="false"

if [ -f "$GRUB_OVERRIDE" ]; then
  if ! grep -q "nvidia-drm.modeset=1" "$GRUB_OVERRIDE"; then
    info "Appending nvidia-drm.modeset=1 to GCE GRUB configuration..."
    # Safely inject nvidia-drm.modeset=1
    sed -i 's/GRUB_CMDLINE_LINUX_DEFAULT="console=ttyS0,115200"/GRUB_CMDLINE_LINUX_DEFAULT="console=ttyS0,115200 nvidia-drm.modeset=1"/g' "$GRUB_OVERRIDE"
    
    info "Rebuilding GRUB and Initramfs..."
    update-grub || error "Failed to update GRUB."
    update-initramfs -u || error "Failed to update initramfs."
    REBOOT_NEEDED="true"
  else
    info "Kernel modesetting parameter is already configured in GRUB."
  fi
else
  warn "GRUB override file not found at $GRUB_OVERRIDE. Checking standard modeset parameters..."
  # Check if modeset is loaded
  CURRENT_MODESET=$(cat /sys/module/nvidia_drm/parameters/modeset 2>/dev/null || echo "N")
  if [ "$CURRENT_MODESET" != "Y" ]; then
    info "Adding modprobe configuration for nvidia-drm modeset..."
    echo "options nvidia-drm modeset=1" > /etc/modprobe.d/nvidia-modeset.conf
    update-initramfs -u || error "Failed to update initramfs."
    REBOOT_NEEDED="true"
  fi
fi

# 6. Install Docker CE
if ! command -v docker &> /dev/null; then
  info "Installing Docker CE..."
  apt-get install -y ca-certificates curl || error "Failed to install certificates & curl."
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc || error "Failed to download Docker GPG key."
  chmod a+r /etc/apt/keyrings/docker.asc

  echo \
    "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
    $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
    tee /etc/apt/sources.list.d/docker.list > /dev/null

  apt-get update -y || error "Failed to update package repositories after adding Docker."
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin || error "Failed to install Docker CE."
else
  info "Docker is already installed."
fi

# 7. Configure NVIDIA Container Toolkit
info "Configuring NVIDIA Container Toolkit for Docker..."
if command -v nvidia-ctk &> /dev/null; then
  nvidia-ctk runtime configure --runtime=docker || error "Failed to configure NVIDIA container runtime."
  systemctl restart docker || error "Failed to restart Docker service."
else
  error "nvidia-ctk command not found! Ensure nvidia-container-toolkit package is installed."
fi

# 8. Add invoking user to the docker group
if [ -n "${SUDO_USER:-}" ]; then
  info "Adding user $SUDO_USER to the docker group..."
  usermod -aG docker "$SUDO_USER" || warn "Failed to add $SUDO_USER to the docker group."
fi

# 9. Success messages & instructions
echo "--------------------------------------------------------"
info "Host setup completed successfully!"
echo "--------------------------------------------------------"

if [ "$REBOOT_NEEDED" = "true" ]; then
  warn "A reboot is required to activate kernel modesetting (nvidia-drm.modeset=1)."
  echo -e "\e[33m>>> Please reboot the system now by running: sudo reboot <<<\e[0m"
else
  info "Modesetting is active and GID/allocators are aligned!"
  info "You can now build and run LLrdc container with direct-buffer capture:"
  echo "  ./docker-build.sh --nvidia"
  echo "  ./docker-run.sh --nvidia --direct-buffer --detach --name llrdc --host-net"
fi
echo "--------------------------------------------------------"
