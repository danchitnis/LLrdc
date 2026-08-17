# GCP NVIDIA Tesla T4 Direct-Buffer (Zero-Copy) Setup Guide

This guide details how to prepare a Google Cloud Platform (GCP) Compute Engine instance equipped with an NVIDIA Tesla T4 GPU to run **LLrdc** on its native, zero-copy direct-buffer capture path (Vulkan-CUDA-NVENC) under Wayland (`labwc`).

---

## Prerequisites & Architecture

For Wayland direct-buffer capture to succeed inside a Docker container, several layers must align between the host kernel, the host NVIDIA proprietary driver, the NVIDIA Container Toolkit, and the container's graphics libraries:

1. **Kernel Modesetting (KMS)**: The `nvidia-drm` module must be loaded with `modeset=1` on the host to expose a KMS-capable `/dev/dri/card0` node. Headless GPUs like the Tesla T4 require this to initialize Wayland compositors (`labwc`/`wlroots`) under GBM.
2. **NVIDIA GBM Backend**: The host must have the closed-source EGL/GBM allocation driver (`nvidia-drm_gbm.so` and `libnvidia-allocator.so`) installed, which is then mounted inside the container.
3. **NVIDIA Container Toolkit**: Must be configured to expose both compute (CUDA) and graphics/display (OpenGL, EGL, Vulkan) capabilities to the container.

---

## Step 1: Install Closed-Source NVIDIA Drivers & GBM Libraries

GCP’s default GCE open-gpu driver packages omit proprietary EGL/GLX/Vulkan components necessary for headless GBM rendering. You must install the full proprietary closed-source server driver metapackage along with the extra allocator/GBM library.

Run the following on the host GCP instance:

```bash
sudo apt-get update

# Install EGL development headers (required for compiling/probing EGL on the host)
sudo apt-get install -y libegl-dev mesa-utils

# Install the full closed-source proprietary server driver
sudo apt-get install -y nvidia-driver-580-server

# Install the critical extra library package providing the GBM backend & NVIDIA Allocator
sudo apt-get install -y libnvidia-extra-580-server
```

---

## Step 2: Enable Kernel-Level KMS (Modesetting)

On Google Cloud, standard GRUB configurations are overridden by cloud-init profiles. You must append modesetting parameters to the Cloud Image GRUB configuration.

1. Edit the GCE GRUB override file `/etc/default/grub.d/50-cloudimg-settings.cfg` and append `nvidia-drm.modeset=1` to the kernel parameters:

   ```bash
   # Before edit:
   # GRUB_CMDLINE_LINUX_DEFAULT="console=ttyS0,115200"

   # After edit:
   GRUB_CMDLINE_LINUX_DEFAULT="console=ttyS0,115200 nvidia-drm.modeset=1"
   ```

   You can apply this programmatically:
   ```bash
   sudo sed -i 's/GRUB_CMDLINE_LINUX_DEFAULT="console=ttyS0,115200"/GRUB_CMDLINE_LINUX_DEFAULT="console=ttyS0,115200 nvidia-drm.modeset=1"/g' /etc/default/grub.d/50-cloudimg-settings.cfg
   ```

2. Rebuild the GRUB configuration and regenerate the initramfs:

   ```bash
   sudo update-grub
   sudo update-initramfs -u
   ```

3. **Reboot the instance** to apply the settings:

   ```bash
   sudo reboot
   ```

4. Once the host boots back up, verify that modesetting is active (`Y`):

   ```bash
   cat /sys/module/nvidia_drm/parameters/modeset
   # Expected output: Y
   ```

---

## Step 3: Install Docker & Configure NVIDIA Container Toolkit

1. Install Docker CE using the official repository:

   ```bash
   sudo apt-get install -y ca-certificates curl
   sudo install -m 0755 -d /etc/apt/keyrings
   sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
   sudo chmod a+r /etc/apt/keyrings/docker.asc

   echo \
     "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
     $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
     sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

   sudo apt-get update
   sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
   ```

2. Configure the NVIDIA Container Toolkit to register itself with Docker:

   ```bash
   sudo nvidia-ctk runtime configure --runtime=docker
   sudo systemctl restart docker
   ```

3. Add your user to the `docker` group to run commands without `sudo`:

   ```bash
   sudo usermod -aG docker $USER
   # Apply group changes dynamically in the current shell
   newgrp docker
   ```

---

## Step 4: Build and Run the LLrdc Container

1. Clone the repository and checkout the `next` branch:

   ```bash
   git clone -b next https://github.com/danchitnis/LLrdc.git ~/LLrdc
   cd ~/LLrdc
   ```

2. Build the NVIDIA-specific container image:

   ```bash
   ./docker-build.sh --nvidia
   ```

3. Run the container with direct-buffer capture enabled:

   ```bash
   ./docker-run.sh --nvidia --direct-buffer --detach --name llrdc --host-net
   ```

---

## Step 5: Verification

Verify that the C++ direct capture pipeline initialized successfully and is actively producing frames.

1. Check the container logs:

   ```bash
   docker logs llrdc
   ```

   You should see:
   ```text
   [NativeCapture] Importing Vulkan linear buffer FD into CUDA...
   [NativeCapture] Vulkan linear buffer FD imported to CUDA successfully.
   [NativeCapture] Mapping Vulkan buffer memory to CUDA...
   [NativeCapture] Vulkan buffer memory mapped to CUDA devPtr: 130808413159424
   [NativeCapture] NVENC Encoder initialized successfully.
   [NativeCapture] NVENC Bitstream Buffer created successfully.
   2026/07/18 22:38:22 Zero-copy hardware capture pipeline is active.
   ```

2. Query the readiness endpoint `/readyz`:

   ```bash
   curl -s http://localhost:8080/readyz
   ```

   You should observe `"directActive": true` and `"zeroCopyValidated": true`:
   ```json
   {
     "acceleratorMode": "nvidia",
     "directActive": true,
     "directBuffer": {
       "requested": true,
       "supported": true,
       "active": true,
       "reason": "Direct-buffer probe passed and hardware capture is active",
       "captureMode": "direct",
       "backend": "nvidia-native",
       "zeroCopyValidated": true
     },
     "directModeActive": true,
     "ready": true,
     "useNvidia": true,
     "videoCodec": "h264_nvenc"
   }
   ```
