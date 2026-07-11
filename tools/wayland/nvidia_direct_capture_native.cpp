#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/mman.h>
#include <sys/time.h>
#include <wayland-client.h>
#include <iostream>
#include <vector>
#include <dlfcn.h>
#include <vulkan/vulkan.h>

// Wayland unstable export-dmabuf protocol header
extern "C" {
#include "wlr-export-dmabuf-unstable-v1-client-protocol.h"
#include "wlr-export-dmabuf-unstable-v1-client-protocol.c"
}

#include "nvEncodeAPI.h"





// CUDA Driver API v2 types
typedef int CUdevice;
typedef void* CUcontext;
typedef unsigned long long CUdeviceptr;
typedef int CUresult;

#define CUDA_SUCCESS 0

// Function pointer signatures for CUDA
typedef CUresult (*t_cuInit)(unsigned int Flags);
typedef CUresult (*t_cuDeviceGet)(CUdevice *device, int ordinal);
typedef CUresult (*t_cuCtxCreate_v2)(CUcontext *pctx, unsigned int flags, CUdevice dev);
typedef CUresult (*t_cuCtxPushCurrent_v2)(CUcontext ctx);
typedef CUresult (*t_cuCtxPopCurrent_v2)(CUcontext *pctx);
typedef CUresult (*t_cuCtxSetCurrent)(CUcontext ctx);

#define CU_EXTERNAL_MEMORY_HANDLE_TYPE_OPAQUE_FD 1
#define CU_EXTERNAL_MEMORY_HANDLE_TYPE_LINUX_DMA_BUF 6
typedef void* CUexternalMemory;

struct CUDA_EXTERNAL_MEMORY_HANDLE_DESC {
    unsigned int type;
    union {
        int fd;
        struct {
            void *handle;
            const void *name;
        } win32;
        const void *nvSciBufObject;
    } handle;
    unsigned long long size;
    unsigned int flags;
    unsigned int reserved[16];
};

struct CUDA_EXTERNAL_MEMORY_BUFFER_DESC {
    unsigned long long offset;
    unsigned long long size;
    unsigned int flags;
    unsigned int reserved[16];
};

typedef CUresult (*t_cuImportExternalMemory)(CUexternalMemory *extMem_out, const CUDA_EXTERNAL_MEMORY_HANDLE_DESC *memHandleDesc);
typedef CUresult (*t_cuExternalMemoryGetMappedBuffer)(CUdeviceptr *devPtr_out, CUexternalMemory extMem, const CUDA_EXTERNAL_MEMORY_BUFFER_DESC *bufferDesc);
typedef CUresult (*t_cuDestroyExternalMemory)(CUexternalMemory extMem);
typedef CUresult (*t_cuDeviceGetByPCIBusId)(CUdevice *device, const char *pciBusId);
typedef CUresult (*t_cuDevicePrimaryCtxRetain)(CUcontext *pctx, CUdevice dev);

static t_cuInit cuInit = nullptr;
static t_cuDeviceGet cuDeviceGet = nullptr;
static t_cuCtxCreate_v2 cuCtxCreate = nullptr;
static t_cuCtxPushCurrent_v2 cuCtxPushCurrent = nullptr;
static t_cuCtxPopCurrent_v2 cuCtxPopCurrent = nullptr;
static t_cuCtxSetCurrent cuCtxSetCurrent = nullptr;
static t_cuImportExternalMemory cuImportExternalMemory = nullptr;
static t_cuExternalMemoryGetMappedBuffer cuExternalMemoryGetMappedBuffer = nullptr;
static t_cuDestroyExternalMemory cuDestroyExternalMemory = nullptr;
static t_cuDeviceGetByPCIBusId cuDeviceGetByPCIBusId = nullptr;
static t_cuDevicePrimaryCtxRetain cuDevicePrimaryCtxRetain = nullptr;

class HeadlessVulkanManager {
public:
    VkInstance instance = VK_NULL_HANDLE;
    VkPhysicalDevice physicalDevice = VK_NULL_HANDLE;
    VkDevice device = VK_NULL_HANDLE;
    VkQueue queue = VK_NULL_HANDLE;
    uint32_t queueFamilyIndex = 0;
    VkCommandPool commandPool = VK_NULL_HANDLE;
    VkCommandBuffer commandBuffer = VK_NULL_HANDLE;

    VkBuffer linearBuffer = VK_NULL_HANDLE;
    VkDeviceMemory linearMemory = VK_NULL_HANDLE;
    int linearFd = -1;
    uint32_t bufferSize = 0;
    VkDeviceSize allocationSize = 0;

    bool init(uint32_t width, uint32_t height) {
        std::cerr << "[Vulkan] Initializing Headless Vulkan zero-copy helper..." << std::endl;
        
        VkApplicationInfo appInfo = {};
        appInfo.sType = VK_STRUCTURE_TYPE_APPLICATION_INFO;
        appInfo.pApplicationName = "LLrdc Headless Capture";
        appInfo.applicationVersion = VK_MAKE_VERSION(1, 0, 0);
        appInfo.pEngineName = "No Engine";
        appInfo.engineVersion = VK_MAKE_VERSION(1, 0, 0);
        appInfo.apiVersion = VK_API_VERSION_1_1;

        const char* instanceExtensions[] = {
            VK_KHR_GET_PHYSICAL_DEVICE_PROPERTIES_2_EXTENSION_NAME,
            VK_KHR_EXTERNAL_MEMORY_CAPABILITIES_EXTENSION_NAME
        };

        VkInstanceCreateInfo createInfo = {};
        createInfo.sType = VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO;
        createInfo.pApplicationInfo = &appInfo;
        createInfo.enabledExtensionCount = 2;
        createInfo.ppEnabledExtensionNames = instanceExtensions;

        if (vkCreateInstance(&createInfo, nullptr, &instance) != VK_SUCCESS) {
            std::cerr << "[Vulkan] Failed to create Vulkan instance!" << std::endl;
            return false;
        }

        uint32_t deviceCount = 0;
        vkEnumeratePhysicalDevices(instance, &deviceCount, nullptr);
        if (deviceCount == 0) {
            std::cerr << "[Vulkan] No physical devices found!" << std::endl;
            return false;
        }
        std::vector<VkPhysicalDevice> devices(deviceCount);
        vkEnumeratePhysicalDevices(instance, &deviceCount, devices.data());

        for (auto d : devices) {
            VkPhysicalDeviceProperties props;
            vkGetPhysicalDeviceProperties(d, &props);
            if (strstr(props.deviceName, "NVIDIA") != nullptr) {
                physicalDevice = d;
                std::cerr << "[Vulkan] Selected NVIDIA physical device: " << props.deviceName << std::endl;
                break;
            }
        }
        if (physicalDevice == VK_NULL_HANDLE) {
            physicalDevice = devices[0];
            VkPhysicalDeviceProperties props;
            vkGetPhysicalDeviceProperties(physicalDevice, &props);
            std::cerr << "[Vulkan] Fallback selected device: " << props.deviceName << std::endl;
        }

        uint32_t queueFamilyCount = 0;
        vkGetPhysicalDeviceQueueFamilyProperties(physicalDevice, &queueFamilyCount, nullptr);
        std::vector<VkQueueFamilyProperties> queueFamilies(queueFamilyCount);
        vkGetPhysicalDeviceQueueFamilyProperties(physicalDevice, &queueFamilyCount, queueFamilies.data());

        bool foundQueue = false;
        for (uint32_t i = 0; i < queueFamilyCount; i++) {
            if ((queueFamilies[i].queueFlags & VK_QUEUE_TRANSFER_BIT) || (queueFamilies[i].queueFlags & VK_QUEUE_GRAPHICS_BIT)) {
                queueFamilyIndex = i;
                foundQueue = true;
                break;
            }
        }
        if (!foundQueue) {
            std::cerr << "[Vulkan] Failed to find a suitable queue family!" << std::endl;
            return false;
        }

        float queuePriority = 1.0f;
        VkDeviceQueueCreateInfo queueCreateInfo = {};
        queueCreateInfo.sType = VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO;
        queueCreateInfo.queueFamilyIndex = queueFamilyIndex;
        queueCreateInfo.queueCount = 1;
        queueCreateInfo.pQueuePriorities = &queuePriority;

        const char* deviceExtensions[] = {
            VK_KHR_EXTERNAL_MEMORY_EXTENSION_NAME,
            VK_KHR_EXTERNAL_MEMORY_FD_EXTENSION_NAME,
            VK_EXT_EXTERNAL_MEMORY_DMA_BUF_EXTENSION_NAME,
            VK_EXT_IMAGE_DRM_FORMAT_MODIFIER_EXTENSION_NAME,
            "VK_KHR_buffer_device_address"
        };

        VkPhysicalDeviceBufferDeviceAddressFeatures bufferDeviceAddressFeatures = {};
        bufferDeviceAddressFeatures.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_BUFFER_DEVICE_ADDRESS_FEATURES;
        bufferDeviceAddressFeatures.bufferDeviceAddress = VK_TRUE;

        VkDeviceCreateInfo deviceCreateInfo = {};
        deviceCreateInfo.sType = VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO;
        deviceCreateInfo.queueCreateInfoCount = 1;
        deviceCreateInfo.pQueueCreateInfos = &queueCreateInfo;
        deviceCreateInfo.enabledExtensionCount = 5;
        deviceCreateInfo.ppEnabledExtensionNames = deviceExtensions;
        deviceCreateInfo.pNext = &bufferDeviceAddressFeatures;

        if (vkCreateDevice(physicalDevice, &deviceCreateInfo, nullptr, &device) != VK_SUCCESS) {
            std::cerr << "[Vulkan] Failed to create logical device!" << std::endl;
            return false;
        }

        vkGetDeviceQueue(device, queueFamilyIndex, 0, &queue);

        VkCommandPoolCreateInfo poolInfo = {};
        poolInfo.sType = VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO;
        poolInfo.queueFamilyIndex = queueFamilyIndex;
        poolInfo.flags = VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT;
        if (vkCreateCommandPool(device, &poolInfo, nullptr, &commandPool) != VK_SUCCESS) {
            std::cerr << "[Vulkan] Failed to create command pool!" << std::endl;
            return false;
        }

        VkCommandBufferAllocateInfo allocInfo = {};
        allocInfo.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO;
        allocInfo.commandPool = commandPool;
        allocInfo.level = VK_COMMAND_BUFFER_LEVEL_PRIMARY;
        allocInfo.commandBufferCount = 1;
        if (vkAllocateCommandBuffers(device, &allocInfo, &commandBuffer) != VK_SUCCESS) {
            std::cerr << "[Vulkan] Failed to allocate command buffer!" << std::endl;
            return false;
        }

        uint32_t raw_size = width * height * 4;
        bufferSize = (raw_size + 2097151) & ~2097151; // Align to 2MB page boundary for CUDA import
        VkBufferCreateInfo bufferInfo = {};
        bufferInfo.sType = VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO;
        bufferInfo.size = bufferSize;
        bufferInfo.usage = VK_BUFFER_USAGE_TRANSFER_DST_BIT | VK_BUFFER_USAGE_SHADER_DEVICE_ADDRESS_BIT;
        bufferInfo.sharingMode = VK_SHARING_MODE_EXCLUSIVE;

        VkExternalMemoryBufferCreateInfo externalMemoryBufferInfo = {};
        externalMemoryBufferInfo.sType = VK_STRUCTURE_TYPE_EXTERNAL_MEMORY_BUFFER_CREATE_INFO;
        externalMemoryBufferInfo.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_OPAQUE_FD_BIT;
        bufferInfo.pNext = &externalMemoryBufferInfo;

        if (vkCreateBuffer(device, &bufferInfo, nullptr, &linearBuffer) != VK_SUCCESS) {
            std::cerr << "[Vulkan] Failed to create linear buffer!" << std::endl;
            return false;
        }

        VkMemoryRequirements memReqs;
        vkGetBufferMemoryRequirements(device, linearBuffer, &memReqs);
        allocationSize = memReqs.size;

        VkMemoryAllocateInfo memAllocInfo = {};
        memAllocInfo.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO;
        memAllocInfo.allocationSize = memReqs.size;

        VkExportMemoryAllocateInfo exportAllocInfo = {};
        exportAllocInfo.sType = VK_STRUCTURE_TYPE_EXPORT_MEMORY_ALLOCATE_INFO;
        exportAllocInfo.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_OPAQUE_FD_BIT;

        VkMemoryDedicatedAllocateInfo dedicatedAlloc = {};
        dedicatedAlloc.sType = VK_STRUCTURE_TYPE_MEMORY_DEDICATED_ALLOCATE_INFO;
        dedicatedAlloc.buffer = linearBuffer;
        dedicatedAlloc.pNext = &exportAllocInfo;

        VkMemoryAllocateFlagsInfo flagsInfo = {};
        flagsInfo.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_FLAGS_INFO;
        flagsInfo.flags = VK_MEMORY_ALLOCATE_DEVICE_ADDRESS_BIT;
        flagsInfo.pNext = &dedicatedAlloc;

        memAllocInfo.pNext = &flagsInfo;

        VkPhysicalDeviceMemoryProperties memProperties;
        vkGetPhysicalDeviceMemoryProperties(physicalDevice, &memProperties);
        
        std::cerr << "[Vulkan] Memory requirements size: " << memReqs.size 
                  << ", type bits: 0x" << std::hex << memReqs.memoryTypeBits << std::dec << std::endl;
        for (uint32_t i = 0; i < memProperties.memoryTypeCount; i++) {
            std::cerr << "  Type " << i << ": flags 0x" << std::hex 
                      << memProperties.memoryTypes[i].propertyFlags << std::dec 
                      << ", heap " << memProperties.memoryTypes[i].heapIndex 
                      << ", compatible: " << ((memReqs.memoryTypeBits & (1 << i)) ? "YES" : "NO") << std::endl;
        }

        uint32_t memTypeIndex = 0;
        bool foundMem = false;
        for (uint32_t i = 0; i < memProperties.memoryTypeCount; i++) {
            if ((memReqs.memoryTypeBits & (1 << i)) && 
                (memProperties.memoryTypes[i].propertyFlags & VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT)) {
                memTypeIndex = i;
                foundMem = true;
                break;
            }
        }
        if (!foundMem) {
            for (uint32_t i = 0; i < memProperties.memoryTypeCount; i++) {
                if (memReqs.memoryTypeBits & (1 << i)) {
                    memTypeIndex = i;
                    foundMem = true;
                    break;
                }
            }
        }
        std::cerr << "[Vulkan] Selected memory type index: " << memTypeIndex << std::endl;
        memAllocInfo.memoryTypeIndex = memTypeIndex;

        if (vkAllocateMemory(device, &memAllocInfo, nullptr, &linearMemory) != VK_SUCCESS) {
            std::cerr << "[Vulkan] Failed to allocate buffer memory!" << std::endl;
            return false;
        }

        vkBindBufferMemory(device, linearBuffer, linearMemory, 0);

        VkMemoryGetFdInfoKHR getFdInfo = {};
        getFdInfo.sType = VK_STRUCTURE_TYPE_MEMORY_GET_FD_INFO_KHR;
        getFdInfo.memory = linearMemory;
        getFdInfo.handleType = VK_EXTERNAL_MEMORY_HANDLE_TYPE_OPAQUE_FD_BIT;

        auto pfn_vkGetMemoryFdKHR = (PFN_vkGetMemoryFdKHR)vkGetDeviceProcAddr(device, "vkGetMemoryFdKHR");
        if (!pfn_vkGetMemoryFdKHR || pfn_vkGetMemoryFdKHR(device, &getFdInfo, &linearFd) != VK_SUCCESS) {
            std::cerr << "[Vulkan] Failed to get opaque FD for memory!" << std::endl;
            return false;
        }

        std::cerr << "[Vulkan] Linear buffer allocated, size: " << bufferSize << ", exported opaque FD: " << linearFd << std::endl;
        return true;
    }

    bool importAndCopyFrame(int dmaFd, uint32_t width, uint32_t height, uint32_t format,
                            uint32_t stride, uint32_t offset, uint64_t modifier) {
        VkSubresourceLayout layout = {};
        layout.offset = offset;
        layout.size = stride * height;
        layout.rowPitch = stride;

        VkImageDrmFormatModifierExplicitCreateInfoEXT explicitInfo = {};
        explicitInfo.sType = VK_STRUCTURE_TYPE_IMAGE_DRM_FORMAT_MODIFIER_EXPLICIT_CREATE_INFO_EXT;
        explicitInfo.drmFormatModifier = modifier;
        explicitInfo.drmFormatModifierPlaneCount = 1;
        explicitInfo.pPlaneLayouts = &layout;

        VkExternalMemoryImageCreateInfo externalCreateInfo = {};
        externalCreateInfo.sType = VK_STRUCTURE_TYPE_EXTERNAL_MEMORY_IMAGE_CREATE_INFO;
        externalCreateInfo.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT;
        externalCreateInfo.pNext = &explicitInfo;

        VkImageCreateInfo imageInfo = {};
        imageInfo.sType = VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO;
        imageInfo.pNext = &externalCreateInfo;
        imageInfo.imageType = VK_IMAGE_TYPE_2D;
        imageInfo.format = VK_FORMAT_B8G8R8A8_UNORM;
        imageInfo.extent.width = width;
        imageInfo.extent.height = height;
        imageInfo.extent.depth = 1;
        imageInfo.mipLevels = 1;
        imageInfo.arrayLayers = 1;
        imageInfo.samples = VK_SAMPLE_COUNT_1_BIT;
        imageInfo.tiling = VK_IMAGE_TILING_DRM_FORMAT_MODIFIER_EXT;
        imageInfo.usage = VK_IMAGE_USAGE_TRANSFER_SRC_BIT;
        imageInfo.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
        imageInfo.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;

        VkImage dmaImage = VK_NULL_HANDLE;
        if (vkCreateImage(device, &imageInfo, nullptr, &dmaImage) != VK_SUCCESS) {
            std::cerr << "[Vulkan] Failed to create DRM modifier VkImage!" << std::endl;
            return false;
        }

        VkMemoryRequirements memReqs;
        vkGetImageMemoryRequirements(device, dmaImage, &memReqs);

        VkImportMemoryFdInfoKHR importFdInfo = {};
        importFdInfo.sType = VK_STRUCTURE_TYPE_IMPORT_MEMORY_FD_INFO_KHR;
        importFdInfo.handleType = VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT;
        importFdInfo.fd = dup(dmaFd);

        VkMemoryAllocateInfo memAllocInfo = {};
        memAllocInfo.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO;
        memAllocInfo.pNext = &importFdInfo;
        memAllocInfo.allocationSize = memReqs.size;

        VkPhysicalDeviceMemoryProperties memProperties;
        vkGetPhysicalDeviceMemoryProperties(physicalDevice, &memProperties);
        uint32_t memTypeIndex = 0;
        for (uint32_t i = 0; i < memProperties.memoryTypeCount; i++) {
            if (memReqs.memoryTypeBits & (1 << i)) {
                memTypeIndex = i;
                break;
            }
        }
        memAllocInfo.memoryTypeIndex = memTypeIndex;

        VkDeviceMemory dmaMemory = VK_NULL_HANDLE;
        if (vkAllocateMemory(device, &memAllocInfo, nullptr, &dmaMemory) != VK_SUCCESS) {
            std::cerr << "[Vulkan] Failed to allocate/import dma memory!" << std::endl;
            vkDestroyImage(device, dmaImage, nullptr);
            close(importFdInfo.fd);
            return false;
        }

        vkBindImageMemory(device, dmaImage, dmaMemory, 0);

        VkCommandBufferBeginInfo beginInfo = {};
        beginInfo.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO;
        beginInfo.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
        vkBeginCommandBuffer(commandBuffer, &beginInfo);

        VkImageMemoryBarrier barrier = {};
        barrier.sType = VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER;
        barrier.oldLayout = VK_IMAGE_LAYOUT_UNDEFINED;
        barrier.newLayout = VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL;
        barrier.srcQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED;
        barrier.dstQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED;
        barrier.image = dmaImage;
        barrier.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
        barrier.subresourceRange.baseMipLevel = 0;
        barrier.subresourceRange.levelCount = 1;
        barrier.subresourceRange.baseArrayLayer = 0;
        barrier.subresourceRange.layerCount = 1;
        barrier.srcAccessMask = 0;
        barrier.dstAccessMask = VK_ACCESS_TRANSFER_READ_BIT;

        vkCmdPipelineBarrier(commandBuffer,
                             VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT,
                             VK_PIPELINE_STAGE_TRANSFER_BIT,
                             0, 0, nullptr, 0, nullptr, 1, &barrier);

        VkBufferImageCopy copyRegion = {};
        copyRegion.bufferOffset = 0;
        copyRegion.bufferRowLength = width;
        copyRegion.bufferImageHeight = height;
        copyRegion.imageSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
        copyRegion.imageSubresource.mipLevel = 0;
        copyRegion.imageSubresource.baseArrayLayer = 0;
        copyRegion.imageSubresource.layerCount = 1;
        copyRegion.imageOffset = {0, 0, 0};
        copyRegion.imageExtent = {width, height, 1};

        vkCmdCopyImageToBuffer(commandBuffer, dmaImage, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL, linearBuffer, 1, &copyRegion);

        vkEndCommandBuffer(commandBuffer);

        VkSubmitInfo submitInfo = {};
        submitInfo.sType = VK_STRUCTURE_TYPE_SUBMIT_INFO;
        submitInfo.commandBufferCount = 1;
        submitInfo.pCommandBuffers = &commandBuffer;

        vkQueueSubmit(queue, 1, &submitInfo, VK_NULL_HANDLE);
        vkQueueWaitIdle(queue);

        vkFreeMemory(device, dmaMemory, nullptr);
        vkDestroyImage(device, dmaImage, nullptr);

        return true;
    }

    void cleanup() {
        if (linearFd >= 0) close(linearFd);
        if (linearMemory != VK_NULL_HANDLE) vkFreeMemory(device, linearMemory, nullptr);
        if (linearBuffer != VK_NULL_HANDLE) vkDestroyBuffer(device, linearBuffer, nullptr);
        if (commandPool != VK_NULL_HANDLE) vkDestroyCommandPool(device, commandPool, nullptr);
        if (device != VK_NULL_HANDLE) vkDestroyDevice(device, nullptr);
        if (instance != VK_NULL_HANDLE) vkDestroyInstance(instance, nullptr);
    }
};

#include <signal.h>

volatile sig_atomic_t g_force_keyframe = 0;
volatile sig_atomic_t g_exit_requested = 0;

void sigusr1_handler(int sig) {
    g_force_keyframe = 1;
}

void sigterm_handler(int sig) {
    g_exit_requested = 1;
}

struct NativeCaptureState {
    struct wl_display *display;
    struct wl_registry *registry;
    struct wl_output *output;
    struct zwlr_export_dmabuf_manager_v1 *dmabuf_manager;
    struct zwlr_export_dmabuf_frame_v1 *frame;
    
    // Captured frame details
    uint32_t width;
    uint32_t height;
    uint32_t target_fps;
    uint32_t target_bitrate_mbps;
    uint32_t format;
    uint32_t mod_high;
    uint32_t mod_low;
    uint32_t num_objects;
    
    struct {
        int fd;
        uint32_t size;
        uint32_t offset;
        uint32_t stride;
    } planes[4];
    
    bool ready_received;
    bool cancel_received;
    bool exit_requested;
    
    // Chroma & Codec flags
    bool chroma_444;
    bool is_hevc;
    uint32_t target_width;
    uint32_t target_height;
    
    char target_codec[64];
    char target_chroma[16];
    GUID selected_encode_guid;
    

    
    // CUDA Context
    void* cuda_lib;
    CUcontext cuda_ctx;
    CUdevice cuda_device;
    bool cuda_initialized;
    
    // NVENC Encoder
    void* nvenc_lib;
    void* nvenc_encoder;
    NV_ENCODE_API_FUNCTION_LIST nvenc_api;
    bool nvenc_initialized;
    
    // NVENC Buffers & Resources
    NV_ENC_REGISTERED_PTR registered_input;
    NV_ENC_INPUT_PTR mapped_input;
    NV_ENC_OUTPUT_PTR bitstream_output;
};

// Wayland Registry Listeners
static void registry_global(void *data, struct wl_registry *registry, uint32_t name, const char *interface, uint32_t version) {
    NativeCaptureState *state = (NativeCaptureState *)data;
    if (strcmp(interface, "wl_output") == 0) {
        state->output = (struct wl_output *)wl_registry_bind(registry, name, &wl_output_interface, 1);
    } else if (strcmp(interface, "zwlr_export_dmabuf_manager_v1") == 0) {
        state->dmabuf_manager = (struct zwlr_export_dmabuf_manager_v1 *)wl_registry_bind(registry, name, &zwlr_export_dmabuf_manager_v1_interface, 1);
    }
}

static void registry_global_remove(void *data, struct wl_registry *registry, uint32_t name) {
    (void)data; (void)registry; (void)name;
}

static const struct wl_registry_listener registry_listener = {
    .global = registry_global,
    .global_remove = registry_global_remove,
};

// Export-dmabuf Frame Listeners
static void frame_handle_frame(void *data, struct zwlr_export_dmabuf_frame_v1 *frame,
                               uint32_t width, uint32_t height, uint32_t offset_x, uint32_t offset_y,
                               uint32_t buffer_flags, uint32_t flags, uint32_t format,
                               uint32_t mod_high, uint32_t mod_low, uint32_t num_objects) {
    NativeCaptureState *state = (NativeCaptureState *)data;
    state->width = width;
    state->height = height;
    state->format = format;
    state->mod_high = mod_high;
    state->mod_low = mod_low;
    state->num_objects = num_objects;
}

static void frame_handle_object(void *data, struct zwlr_export_dmabuf_frame_v1 *frame,
                                uint32_t index, int32_t fd, uint32_t size, uint32_t offset,
                                uint32_t stride, uint32_t plane_index) {
    NativeCaptureState *state = (NativeCaptureState *)data;
    if (index < 4) {
        state->planes[index].fd = fd;
        state->planes[index].size = size;
        state->planes[index].offset = offset;
        state->planes[index].stride = stride;
    } else {
        close(fd);
    }
}

static void frame_handle_ready(void *data, struct zwlr_export_dmabuf_frame_v1 *frame,
                               uint32_t tv_sec_hi, uint32_t tv_sec_lo, uint32_t tv_nsec) {
    NativeCaptureState *state = (NativeCaptureState *)data;
    state->ready_received = true;
}

static void frame_handle_cancel(void *data, struct zwlr_export_dmabuf_frame_v1 *frame, uint32_t reason) {
    NativeCaptureState *state = (NativeCaptureState *)data;
    state->cancel_received = true;
}

static const struct zwlr_export_dmabuf_frame_v1_listener frame_listener = {
    .frame = frame_handle_frame,
    .object = frame_handle_object,
    .ready = frame_handle_ready,
    .cancel = frame_handle_cancel,
};

static bool set_cuda_context_current(NativeCaptureState *state, const char *label) {
    if (!state || !state->cuda_ctx || !cuCtxSetCurrent) {
        std::cerr << "[NativeCapture] Cannot bind CUDA context for " << label << std::endl;
        return false;
    }

    CUresult set_res = cuCtxSetCurrent(state->cuda_ctx);
    if (set_res != CUDA_SUCCESS) {
        std::cerr << "[NativeCapture] cuCtxSetCurrent failed during " << label << "! CUresult: " << set_res << std::endl;
        return false;
    }
    return true;
}

static bool guid_equals(const GUID &a, const GUID &b) {
    return memcmp(&a, &b, sizeof(GUID)) == 0;
}

static bool build_nvenc_init_params(NativeCaptureState *state, NV_ENC_INITIALIZE_PARAMS *initParams, NV_ENC_CONFIG *encodeConfig) {
    if (!state || !initParams || !encodeConfig) {
        return false;
    }

    memset(initParams, 0, sizeof(*initParams));
    memset(encodeConfig, 0, sizeof(*encodeConfig));

    initParams->version = NV_ENC_INITIALIZE_PARAMS_VER;
    if (strstr(state->target_codec, "h265") || strstr(state->target_codec, "hevc")) {
        initParams->encodeGUID = NV_ENC_CODEC_HEVC_GUID;
        state->is_hevc = true;
    } else if (strstr(state->target_codec, "av1")) {
        initParams->encodeGUID = NV_ENC_CODEC_AV1_GUID;
        state->is_hevc = false;
    } else {
        initParams->encodeGUID = NV_ENC_CODEC_H264_GUID;
        state->is_hevc = false;
    }
    state->selected_encode_guid = initParams->encodeGUID;
    initParams->presetGUID = NV_ENC_PRESET_P1_GUID;
    initParams->tuningInfo = NV_ENC_TUNING_INFO_LOW_LATENCY;
    initParams->encodeWidth = state->width;
    initParams->encodeHeight = state->height;
    initParams->darWidth = state->width;
    initParams->darHeight = state->height;
    initParams->frameRateNum = state->target_fps ? state->target_fps : 30;
    initParams->frameRateDen = 1;
    initParams->enableEncodeAsync = 0;
    initParams->enablePTD = 1;
    initParams->maxEncodeWidth = state->width;
    initParams->maxEncodeHeight = state->height;
    initParams->encodeConfig = encodeConfig;

    encodeConfig->version = NV_ENC_CONFIG_VER;

    NV_ENC_PRESET_CONFIG presetConfig = {};
    presetConfig.version = NV_ENC_PRESET_CONFIG_VER;
    presetConfig.presetCfg.version = NV_ENC_CONFIG_VER;

    NVENCSTATUS preset_status = NV_ENC_ERR_GENERIC;
    if (state->nvenc_api.nvEncGetEncodePresetConfigEx) {
        preset_status = state->nvenc_api.nvEncGetEncodePresetConfigEx(
            state->nvenc_encoder,
            initParams->encodeGUID,
            initParams->presetGUID,
            initParams->tuningInfo,
            &presetConfig);
    } else {
        preset_status = state->nvenc_api.nvEncGetEncodePresetConfig(
            state->nvenc_encoder,
            initParams->encodeGUID,
            initParams->presetGUID,
            &presetConfig);
    }

    if (preset_status != NV_ENC_SUCCESS) {
        std::cerr << "[NativeCapture] Failed to query NVENC preset config! Status: " << preset_status << std::endl;
        return false;
    }

    memcpy(encodeConfig, &presetConfig.presetCfg, sizeof(*encodeConfig));
    encodeConfig->version = NV_ENC_CONFIG_VER;
    uint32_t fps = state->target_fps ? state->target_fps : 30;
    encodeConfig->gopLength = fps * 2; // Periodic keyframe every 2 seconds
    encodeConfig->frameIntervalP = 1;
    encodeConfig->rcParams.rateControlMode = NV_ENC_PARAMS_RC_CBR_LOWDELAY_HQ;
    uint32_t targetBitrate = (state->target_bitrate_mbps ? state->target_bitrate_mbps : 5) * 1000 * 1000;
    encodeConfig->rcParams.averageBitRate = targetBitrate;
    encodeConfig->rcParams.maxBitRate = targetBitrate;
    encodeConfig->rcParams.vbvBufferSize = targetBitrate;
    encodeConfig->rcParams.vbvInitialDelay = targetBitrate;

    uint32_t chromaFormat = 1; // Default to YUV 4:2:0
    if (strcmp(state->target_chroma, "444") == 0) {
        chromaFormat = 3; // YUV 4:4:4
    }
    state->chroma_444 = (chromaFormat == 3);

    if (guid_equals(initParams->encodeGUID, NV_ENC_CODEC_H264_GUID)) {
        if (chromaFormat == 3) {
            encodeConfig->profileGUID = NV_ENC_H264_PROFILE_HIGH_444_GUID;
        } else {
            encodeConfig->profileGUID = NV_ENC_H264_PROFILE_HIGH_GUID;
        }
        encodeConfig->encodeCodecConfig.h264Config.idrPeriod = encodeConfig->gopLength;
        encodeConfig->encodeCodecConfig.h264Config.repeatSPSPPS = 1;
        encodeConfig->encodeCodecConfig.h264Config.outputAUD = 1;
        encodeConfig->encodeCodecConfig.h264Config.chromaFormatIDC = chromaFormat;
    } else if (guid_equals(initParams->encodeGUID, NV_ENC_CODEC_HEVC_GUID)) {
        if (chromaFormat == 3) {
            encodeConfig->profileGUID = NV_ENC_HEVC_PROFILE_FREXT_GUID;
        } else {
            encodeConfig->profileGUID = NV_ENC_HEVC_PROFILE_MAIN_GUID;
        }
        encodeConfig->encodeCodecConfig.hevcConfig.idrPeriod = encodeConfig->gopLength;
        encodeConfig->encodeCodecConfig.hevcConfig.repeatSPSPPS = 1;
        encodeConfig->encodeCodecConfig.hevcConfig.outputAUD = 1;
        encodeConfig->encodeCodecConfig.hevcConfig.chromaFormatIDC = chromaFormat;
    }

    return true;
}



// Real CUDA Context and Function Resolution
bool init_cuda_pipeline(NativeCaptureState *state, const char *pciBusId) {
    std::cerr << "[NativeCapture] Dynamically loading CUDA Driver library..." << std::endl;
    state->cuda_lib = dlopen("libcuda.so.1", RTLD_LAZY);
    if (!state->cuda_lib) {
        state->cuda_lib = dlopen("libcuda.so", RTLD_LAZY);
    }
    if (!state->cuda_lib) {
        std::cerr << "[NativeCapture] Failed to load libcuda.so!" << std::endl;
        return false;
    }
    
    cuInit = (t_cuInit)dlsym(state->cuda_lib, "cuInit");
    cuDeviceGet = (t_cuDeviceGet)dlsym(state->cuda_lib, "cuDeviceGet");
    cuCtxCreate = (t_cuCtxCreate_v2)dlsym(state->cuda_lib, "cuCtxCreate_v2");
    cuCtxPushCurrent = (t_cuCtxPushCurrent_v2)dlsym(state->cuda_lib, "cuCtxPushCurrent_v2");
    cuCtxPopCurrent = (t_cuCtxPopCurrent_v2)dlsym(state->cuda_lib, "cuCtxPopCurrent_v2");
    cuCtxSetCurrent = (t_cuCtxSetCurrent)dlsym(state->cuda_lib, "cuCtxSetCurrent");
    cuImportExternalMemory = (t_cuImportExternalMemory)dlsym(state->cuda_lib, "cuImportExternalMemory");
    cuExternalMemoryGetMappedBuffer = (t_cuExternalMemoryGetMappedBuffer)dlsym(state->cuda_lib, "cuExternalMemoryGetMappedBuffer");
    cuDestroyExternalMemory = (t_cuDestroyExternalMemory)dlsym(state->cuda_lib, "cuDestroyExternalMemory");
    cuDeviceGetByPCIBusId = (t_cuDeviceGetByPCIBusId)dlsym(state->cuda_lib, "cuDeviceGetByPCIBusId");
    cuDevicePrimaryCtxRetain = (t_cuDevicePrimaryCtxRetain)dlsym(state->cuda_lib, "cuDevicePrimaryCtxRetain");
    std::cerr << "[NativeCapture] cuDeviceGetByPCIBusId resolved to: " << (void*)cuDeviceGetByPCIBusId << std::endl;
    
    if (!cuInit || !cuDeviceGet || !cuCtxPushCurrent || !cuCtxPopCurrent || !cuCtxSetCurrent || !cuImportExternalMemory || !cuExternalMemoryGetMappedBuffer || !cuDestroyExternalMemory || !cuDeviceGetByPCIBusId || !cuDevicePrimaryCtxRetain) {
        std::cerr << "[NativeCapture] Failed to resolve essential CUDA symbols!" << std::endl;
        return false;
    }
    
    if (cuInit(0) != CUDA_SUCCESS) {
        std::cerr << "[NativeCapture] cuInit failed!" << std::endl;
        return false;
    }
    
    if (cuDeviceGetByPCIBusId(&state->cuda_device, pciBusId) != CUDA_SUCCESS) {
        std::cerr << "[NativeCapture] cuDeviceGetByPCIBusId failed for PCI address: " << pciBusId << std::endl;
        return false;
    }
    
    if (cuDevicePrimaryCtxRetain(&state->cuda_ctx, state->cuda_device) != CUDA_SUCCESS) {
        std::cerr << "[NativeCapture] cuDevicePrimaryCtxRetain failed!" << std::endl;
        return false;
    }

    if (!set_cuda_context_current(state, "CUDA primary context init")) {
        return false;
    }
    
    state->cuda_initialized = true;
    return true;
}

// Real NVENC Session and SDK Initialization
bool init_nvenc_pipeline(NativeCaptureState *state) {
    std::cerr << "[NativeCapture] Dynamically loading NVENC Encoder library..." << std::endl;
    state->nvenc_lib = dlopen("libnvidia-encode.so.1", RTLD_LAZY);
    if (!state->nvenc_lib) {
        state->nvenc_lib = dlopen("libnvidia-encode.so", RTLD_LAZY);
    }
    if (!state->nvenc_lib) {
        std::cerr << "[NativeCapture] Failed to load libnvidia-encode.so!" << std::endl;
        return false;
    }
    
    typedef NVENCSTATUS (NVENCAPI *t_NvEncodeAPICreateInstance)(NV_ENCODE_API_FUNCTION_LIST *apiFunctions);
    t_NvEncodeAPICreateInstance NvEncodeAPICreateInstance = (t_NvEncodeAPICreateInstance)dlsym(state->nvenc_lib, "NvEncodeAPICreateInstance");
    if (!NvEncodeAPICreateInstance) {
        std::cerr << "[NativeCapture] Failed to resolve NvEncodeAPICreateInstance!" << std::endl;
        return false;
    }
    
    memset(&state->nvenc_api, 0, sizeof(state->nvenc_api));
    state->nvenc_api.version = NV_ENCODE_API_FUNCTION_LIST_VER;
    NVENCSTATUS status = NvEncodeAPICreateInstance(&state->nvenc_api);
    if (status != NV_ENC_SUCCESS) {
        std::cerr << "[NativeCapture] NvEncodeAPICreateInstance failed! Status: " << status << std::endl;
        return false;
    }
    
    if (!set_cuda_context_current(state, "NVENC session init")) {
        return false;
    }

    NV_ENC_OPEN_ENCODE_SESSION_EX_PARAMS openSessionParams = {0};
    openSessionParams.version = NV_ENC_OPEN_ENCODE_SESSION_EX_PARAMS_VER;
    openSessionParams.device = state->cuda_ctx;
    openSessionParams.deviceType = NV_ENC_DEVICE_TYPE_CUDA;
    openSessionParams.apiVersion = NVENCAPI_VERSION;
    
    NVENCSTATUS enc_status = state->nvenc_api.nvEncOpenEncodeSessionEx(&openSessionParams, &state->nvenc_encoder);
    if (enc_status != NV_ENC_SUCCESS) {
        std::cerr << "[NativeCapture] nvEncOpenEncodeSessionEx failed! Status: " << enc_status << std::endl;
        return false;
    }

    state->nvenc_initialized = true;
    return true;
}



void process_frame_zero_copy(NativeCaptureState *state) {
    static HeadlessVulkanManager vkManager;
    static bool vk_initialized = false;
    if (!vk_initialized) {
        vk_initialized = vkManager.init(state->width, state->height);
    }

    if (!vk_initialized) {
        std::cerr << "[NativeCapture] Vulkan initialization failed. Direct capture unavailable." << std::endl;
        close(state->planes[0].fd);
        return;
    }

    static bool pipelines_initialized = false;
    if (!pipelines_initialized) {
        // Query Vulkan PCI Bus ID and associate with CUDA
        VkPhysicalDevicePCIBusInfoPropertiesEXT pciBusInfo = {};
        pciBusInfo.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_PCI_BUS_INFO_PROPERTIES_EXT;
        
        VkPhysicalDeviceProperties2 properties2 = {};
        properties2.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_PROPERTIES_2;
        properties2.pNext = &pciBusInfo;
        
        vkGetPhysicalDeviceProperties2(vkManager.physicalDevice, &properties2);
        
        char pciBusId[32];
        snprintf(pciBusId, sizeof(pciBusId), "%04x:%02x:%02x.%x",
                 pciBusInfo.pciDomain, pciBusInfo.pciBus, pciBusInfo.pciDevice, pciBusInfo.pciFunction);
        std::cerr << "[NativeCapture] Vulkan physical device PCI Bus ID: " << pciBusId << std::endl;

        // Initialize CUDA and NVENC pipelines
        if (!init_cuda_pipeline(state, pciBusId) || !init_nvenc_pipeline(state)) {
            std::cerr << "[NativeCapture] Failed to initialize CUDA or NVENC pipelines! Exiting." << std::endl;
            close(state->planes[0].fd);
            exit(1);
        }
        pipelines_initialized = true;
    }

    // NVENC requires the paired CUDA primary context to be bound on the callback thread.
    if (!set_cuda_context_current(state, "frame processing")) {
        close(state->planes[0].fd);
        return;
    }

    // 1. Copy the block-linear Wayland DMA-BUF into the linear Vulkan buffer
    uint64_t modifier = ((uint64_t)state->mod_high << 32) | state->mod_low;
    if (!vkManager.importAndCopyFrame(state->planes[0].fd, state->width, state->height, state->format,
                                       state->planes[0].stride, state->planes[0].offset, modifier)) {
        std::cerr << "[NativeCapture] Vulkan import and copy failed! Exiting." << std::endl;
        close(state->planes[0].fd);
        exit(1);
    }

    // 2. Import the linear Vulkan buffer FD into CUDA once
    static CUexternalMemory extMem = NULL;
    static CUdeviceptr devPtr = 0;
    if (!devPtr) {
        std::cerr << "[NativeCapture] CUDA Opaque FD Import Details:" << std::endl;
        std::cerr << "  FD: " << vkManager.linearFd << std::endl;
        std::cerr << "  Buffer Size: " << vkManager.bufferSize << std::endl;
        std::cerr << "  Allocation Size: " << vkManager.allocationSize << std::endl;

        CUDA_EXTERNAL_MEMORY_HANDLE_DESC extMemDesc = {0};
        extMemDesc.type = CU_EXTERNAL_MEMORY_HANDLE_TYPE_OPAQUE_FD;
        extMemDesc.handle.fd = dup(vkManager.linearFd);
        extMemDesc.size = vkManager.allocationSize;
        extMemDesc.flags = 1; // CU_EXTERNAL_MEMORY_DEDICATED

        std::cerr << "[NativeCapture] Importing Vulkan linear buffer FD into CUDA..." << std::endl;
        CUresult import_res = cuImportExternalMemory(&extMem, &extMemDesc);
        if (import_res != CUDA_SUCCESS) {
            std::cerr << "[NativeCapture] Failed to import Vulkan linear buffer FD into CUDA! Error: " << import_res << ". Exiting." << std::endl;
            close(state->planes[0].fd);
            exit(1);
        }
        std::cerr << "[NativeCapture] Vulkan linear buffer FD imported to CUDA successfully." << std::endl;

        CUDA_EXTERNAL_MEMORY_BUFFER_DESC bufferDesc = {0};
        bufferDesc.offset = 0;
        bufferDesc.size = vkManager.allocationSize;

        std::cerr << "[NativeCapture] Mapping Vulkan buffer memory to CUDA..." << std::endl;
        CUresult map_res = cuExternalMemoryGetMappedBuffer(&devPtr, extMem, &bufferDesc);
        if (map_res != CUDA_SUCCESS) {
            std::cerr << "[NativeCapture] Failed to map Vulkan buffer memory to CUDA! Error: " << map_res << ". Exiting." << std::endl;
            cuDestroyExternalMemory(extMem);
            extMem = NULL;
            close(state->planes[0].fd);
            exit(1);
        }
        std::cerr << "[NativeCapture] Vulkan buffer memory mapped to CUDA devPtr: " << devPtr << std::endl;
    }

    // We no longer need the Wayland DMA-BUF FD in this frame as it was copied by Vulkan
    close(state->planes[0].fd);
    
    // 4. Setup NVENC configuration dynamically on first frame
    if (!state->bitstream_output) {
        if (!set_cuda_context_current(state, "NVENC encoder init")) {
            return;
        }

        NV_ENC_INITIALIZE_PARAMS initParams = {};
        NV_ENC_CONFIG encodeConfig = {};
        if (!build_nvenc_init_params(state, &initParams, &encodeConfig)) {
            close(state->planes[0].fd);
            exit(1);
        }
        
        uint32_t guidCount = 0;
        NVENCSTATUS guid_status = state->nvenc_api.nvEncGetEncodeGUIDCount(state->nvenc_encoder, &guidCount);
        std::cerr << "[NativeCapture] nvEncGetEncodeGUIDCount returned status: " << guid_status << ", count: " << guidCount << std::endl;

        uint32_t formatCount = 0;
        state->nvenc_api.nvEncGetInputFormats(state->nvenc_encoder, initParams.encodeGUID, nullptr, 0, &formatCount);
        std::vector<NV_ENC_BUFFER_FORMAT> inputFormats(formatCount);
        state->nvenc_api.nvEncGetInputFormats(state->nvenc_encoder, initParams.encodeGUID, inputFormats.data(), formatCount, &formatCount);
        std::cerr << "[NativeCapture] Supported input formats count: " << formatCount << std::endl;
        for (uint32_t i = 0; i < formatCount; i++) {
            std::cerr << "  Format " << i << ": 0x" << std::hex << inputFormats[i] << std::dec << std::endl;
        }

        uint32_t presetCount = 0;
        NVENCSTATUS preset_count_status = state->nvenc_api.nvEncGetEncodePresetCount(state->nvenc_encoder, state->selected_encode_guid, &presetCount);
        std::cerr << "[NativeCapture] nvEncGetEncodePresetCount returned status: " << preset_count_status << ", count: " << presetCount << std::endl;

        std::cerr << "[NativeCapture] Initializing NVENC with explicit preset config and tuning info." << std::endl;
        
        NVENCSTATUS enc_init_status = state->nvenc_api.nvEncInitializeEncoder(state->nvenc_encoder, &initParams);
        if (enc_init_status != NV_ENC_SUCCESS) {
            std::cerr << "[NativeCapture] Failed to initialize NVENC Encoder! Error status: " << enc_init_status << ". Exiting." << std::endl;
            close(state->planes[0].fd);
            exit(1);
        }
        std::cerr << "[NativeCapture] NVENC Encoder initialized successfully." << std::endl;
        
        // Create bitstream output buffer
        NV_ENC_CREATE_BITSTREAM_BUFFER bitstreamParams = {0};
        bitstreamParams.version = NV_ENC_CREATE_BITSTREAM_BUFFER_VER;
        if (state->nvenc_api.nvEncCreateBitstreamBuffer(state->nvenc_encoder, &bitstreamParams) != NV_ENC_SUCCESS) {
            std::cerr << "[NativeCapture] Failed to create NVENC Bitstream Buffer!" << std::endl;
            return;
        }
        state->bitstream_output = bitstreamParams.bitstreamBuffer;
        std::cerr << "[NativeCapture] NVENC Bitstream Buffer created successfully." << std::endl;
    }
    
    // 5. Register mapped CUDA device pointer as an input resource with NVENC
    if (!set_cuda_context_current(state, "NVENC frame encode")) {
        return;
    }

    NV_ENC_REGISTER_RESOURCE registerParams = {0};
    registerParams.version = NV_ENC_REGISTER_RESOURCE_VER;
    registerParams.resourceType = NV_ENC_INPUT_RESOURCE_TYPE_CUDADEVICEPTR;
    registerParams.resourceToRegister = (void*)devPtr;
    registerParams.width = state->width;
    registerParams.height = state->height;
    registerParams.pitch = state->width * 4;
    registerParams.bufferFormat = NV_ENC_BUFFER_FORMAT_ARGB; // Little-endian BGRX
    registerParams.bufferUsage = NV_ENC_INPUT_IMAGE;
    
    NVENCSTATUS reg_status = state->nvenc_api.nvEncRegisterResource(state->nvenc_encoder, &registerParams);
    if (reg_status != NV_ENC_SUCCESS) {
        std::cerr << "[NativeCapture] Failed to register CUDA pointer with NVENC! Error: " << reg_status << std::endl;
        return;
    }
    state->registered_input = registerParams.registeredResource;
    
    // 6. Map the NVENC input resource
    NV_ENC_MAP_INPUT_RESOURCE mapParams = {0};
    mapParams.version = NV_ENC_MAP_INPUT_RESOURCE_VER;
    mapParams.registeredResource = state->registered_input;
    NVENCSTATUS map_status = state->nvenc_api.nvEncMapInputResource(state->nvenc_encoder, &mapParams);
    if (map_status != NV_ENC_SUCCESS) {
        std::cerr << "[NativeCapture] Failed to map input resource! Error: " << map_status << std::endl;
        state->nvenc_api.nvEncUnregisterResource(state->nvenc_encoder, state->registered_input);
        return;
    }
    state->mapped_input = mapParams.mappedResource;
    
    // 7. Encode the picture
    NV_ENC_PIC_PARAMS picParams = {0};
    picParams.version = NV_ENC_PIC_PARAMS_VER;
    picParams.inputBuffer = state->mapped_input;
    picParams.bufferFmt = NV_ENC_BUFFER_FORMAT_ARGB;
    picParams.inputWidth = state->width;
    picParams.inputHeight = state->height;
    picParams.outputBitstream = state->bitstream_output;
    picParams.pictureStruct = NV_ENC_PIC_STRUCT_FRAME;
    if (g_force_keyframe) {
        picParams.encodePicFlags = NV_ENC_PIC_FLAG_FORCEIDR | NV_ENC_PIC_FLAG_OUTPUT_SPSPPS;
        g_force_keyframe = 0;
        std::cerr << "[NativeCapture] Forcing keyframe (IDR + SPS/PPS) due to SIGUSR1." << std::endl;
    }
    
    NVENCSTATUS enc_status = state->nvenc_api.nvEncEncodePicture(state->nvenc_encoder, &picParams);
    if (enc_status != NV_ENC_SUCCESS) {
        std::cerr << "[NativeCapture] nvEncEncodePicture failed! Error: " << enc_status << std::endl;
    } else {
        // Lock bitstream to read compressed video data
        NV_ENC_LOCK_BITSTREAM lockParams = {0};
        lockParams.version = NV_ENC_LOCK_BITSTREAM_VER;
        lockParams.outputBitstream = state->bitstream_output;
        lockParams.doNotWait = 0;
        
        NVENCSTATUS lock_status = state->nvenc_api.nvEncLockBitstream(state->nvenc_encoder, &lockParams);
        if (lock_status == NV_ENC_SUCCESS) {
            // Write compressed H.264 video bytes straight to stdout
            fwrite(lockParams.bitstreamBufferPtr, 1, lockParams.bitstreamSizeInBytes, stdout);
            fflush(stdout);
            
            state->nvenc_api.nvEncUnlockBitstream(state->nvenc_encoder, state->bitstream_output);
        } else {
            std::cerr << "[NativeCapture] nvEncLockBitstream failed! Error: " << lock_status << std::endl;
        }
    }
    
    // 8. Cleanup resources for the frame
    state->nvenc_api.nvEncUnmapInputResource(state->nvenc_encoder, state->mapped_input);
    state->nvenc_api.nvEncUnregisterResource(state->nvenc_encoder, state->registered_input);
}

bool probe_hardware_import(NativeCaptureState *state) {
    // Under unprivileged headless NVIDIA contexts, GBM and EGL are completely blocked at the driver level.
    // However, direct Vulkan-CUDA interop via Linux DMA-BUF sharing is fully supported.
    // We probe capability by verifying successful CUDA and Vulkan headless initialization.
    if (!state) return false;
    HeadlessVulkanManager vkManager;
    if (vkManager.init(64, 64)) {
        VkPhysicalDevicePCIBusInfoPropertiesEXT pciBusInfo = {};
        pciBusInfo.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_PCI_BUS_INFO_PROPERTIES_EXT;
        VkPhysicalDeviceProperties2 properties2 = {};
        properties2.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_PROPERTIES_2;
        properties2.pNext = &pciBusInfo;
        vkGetPhysicalDeviceProperties2(vkManager.physicalDevice, &properties2);
        
        char pciBusId[32];
        snprintf(pciBusId, sizeof(pciBusId), "%04x:%02x:%02x.%x",
                 pciBusInfo.pciDomain, pciBusInfo.pciBus, pciBusInfo.pciDevice, pciBusInfo.pciFunction);
                 
        if (state->cuda_initialized || init_cuda_pipeline(state, pciBusId)) {
            std::cerr << "[NativeCapture] Vulkan-CUDA interop hardware import probe SUCCEEDED!" << std::endl;
            return true;
        }
    }
    std::cerr << "[NativeCapture] Vulkan-CUDA interop hardware import probe FAILED!" << std::endl;
    return false;
}

int main(int argc, char **argv) {
    signal(SIGUSR1, sigusr1_handler);
    signal(SIGINT, sigterm_handler);
    signal(SIGTERM, sigterm_handler);
    NativeCaptureState state = {0};
    for (int i = 0; i < 4; i++) state.planes[i].fd = -1;
    state.target_fps = 30;
    state.target_bitrate_mbps = 5;
    state.target_width = 0;
    state.target_height = 0;
    strcpy(state.target_codec, "h264");
    strcpy(state.target_chroma, "420");

    bool probe_only = false;
    for (int i = 1; i < argc; i++) {
        if (strcmp(argv[i], "--probe") == 0) {
            probe_only = true;
            continue;
        }
        if (strcmp(argv[i], "--fps") == 0 && i + 1 < argc) {
            int parsed_fps = atoi(argv[++i]);
            if (parsed_fps > 0) {
                state.target_fps = (uint32_t)parsed_fps;
            }
            continue;
        }
        if (strcmp(argv[i], "--bitrate") == 0 && i + 1 < argc) {
            int parsed_bitrate = atoi(argv[++i]);
            if (parsed_bitrate > 0) {
                state.target_bitrate_mbps = (uint32_t)parsed_bitrate;
            }
            continue;
        }
        if (strcmp(argv[i], "--width") == 0 && i + 1 < argc) {
            state.target_width = (uint32_t)atoi(argv[++i]);
            continue;
        }
        if (strcmp(argv[i], "--height") == 0 && i + 1 < argc) {
            state.target_height = (uint32_t)atoi(argv[++i]);
            continue;
        }
        if (strcmp(argv[i], "--codec") == 0 && i + 1 < argc) {
            strncpy(state.target_codec, argv[++i], sizeof(state.target_codec) - 1);
            state.is_hevc = (strstr(state.target_codec, "hevc") || strstr(state.target_codec, "h265"));
            continue;
        }
        if (strcmp(argv[i], "--chroma") == 0 && i + 1 < argc) {
            strncpy(state.target_chroma, argv[++i], sizeof(state.target_chroma) - 1);
            state.chroma_444 = (strcmp(state.target_chroma, "444") == 0);
            continue;
        }
    }

    if (probe_only) {
        std::cerr << "[NativeCapture] Running direct dma-buf import hardware probe..." << std::endl;
        bool hw_ok = probe_hardware_import(&state);
        if (hw_ok) {
            std::cerr << "[NativeCapture] Probe SUCCEEDED!" << std::endl;
            return 0;
        } else {
            std::cerr << "[NativeCapture] Probe FAILED!" << std::endl;
            return 1;
        }
    }
    
    std::cerr << "[NativeCapture] Starting native C++ zero-copy capture engine..." << std::endl;
    std::cerr << "[NativeCapture] Requested target FPS: " << state.target_fps
              << ", bitrate: " << state.target_bitrate_mbps << " Mbps"
              << ", chroma: " << (state.chroma_444 ? "4:4:4" : "4:2:0")
              << ", codec: " << (state.is_hevc ? "HEVC" : "H.264") << std::endl;
    
    state.display = wl_display_connect(NULL);
    if (!state.display) {
        std::cerr << "[NativeCapture] Failed to connect to Wayland display!" << std::endl;
        return 1;
    }
    
    state.registry = wl_display_get_registry(state.display);
    wl_registry_add_listener(state.registry, &registry_listener, &state);
    wl_display_roundtrip(state.display);
    
    if (!state.dmabuf_manager) {
        std::cerr << "[NativeCapture] Compositor lacks zwlr_export_dmabuf_manager_v1 support!" << std::endl;
        wl_display_disconnect(state.display);
        return 1;
    }
    

    
    std::cerr << "[NativeCapture] Capture pipeline established successfully." << std::endl;
    
    while (!state.exit_requested && !g_exit_requested) {
        state.frame = zwlr_export_dmabuf_manager_v1_capture_output(state.dmabuf_manager, 1, state.output);
        zwlr_export_dmabuf_frame_v1_add_listener(state.frame, &frame_listener, &state);
        
        state.ready_received = false;
        state.cancel_received = false;
        
        while (!state.ready_received && !state.cancel_received) {
            if (wl_display_dispatch(state.display) < 0) {
                state.exit_requested = true;
                break;
            }
        }
        
        if (state.ready_received) {
            if (state.target_width > 0 && state.target_height > 0 &&
                (state.width != state.target_width || state.height != state.target_height)) {
                static bool warned = false;
                if (!warned) {
                    std::cerr << "[NativeCapture] Dropping frame with mismatched compositor dimensions: " 
                              << state.width << "x" << state.height << " (target: " 
                              << state.target_width << "x" << state.target_height << ")" << std::endl;
                    warned = true;
                }
                if (state.planes[0].fd >= 0) {
                    close(state.planes[0].fd);
                    state.planes[0].fd = -1;
                }
                state.ready_received = false;
            } else {
                process_frame_zero_copy(&state);
            }
        }
        
        if (state.frame) {
            zwlr_export_dmabuf_frame_v1_destroy(state.frame);
            state.frame = NULL;
        }
    }
    
    // Cleanup Wayland resources
    if (state.dmabuf_manager) zwlr_export_dmabuf_manager_v1_destroy(state.dmabuf_manager);
    if (state.output) wl_output_destroy(state.output);
    if (state.registry) wl_registry_destroy(state.registry);
    if (state.display) wl_display_disconnect(state.display);
    
    std::cerr << "[NativeCapture] Closed successfully." << std::endl;
    return 0;
}
