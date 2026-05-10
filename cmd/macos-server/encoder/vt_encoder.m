#include <VideoToolbox/VideoToolbox.h>
#include <CoreMedia/CoreMedia.h>
#include <CoreVideo/CoreVideo.h>
#include <mach/mach_time.h>
#include <stdio.h>

typedef void (*VTEncoderCallback)(void* outputCallbackRefCon, void* sourceFrameRefCon, OSStatus status, VTEncodeInfoFlags infoFlags, CMSampleBufferRef sampleBuffer);

// Callback function that receives encoded frames
void compressionCallback(void* outputCallbackRefCon, void* sourceFrameRefCon, OSStatus status, VTEncodeInfoFlags infoFlags, CMSampleBufferRef sampleBuffer) {
    if (status != noErr) {
        return;
    }

    if (!CMSampleBufferDataIsReady(sampleBuffer)) {
        return;
    }

    // Go-side callback
    extern void goEncodedFrameCallback(uintptr_t handle, void* data, int length, int isKeyframe);

    // Extract NALUs safely
    CMBlockBufferRef dataBuffer = CMSampleBufferGetDataBuffer(sampleBuffer);
    if (!dataBuffer) return;

    size_t totalLength = CMBlockBufferGetDataLength(dataBuffer);
    if (totalLength == 0 || totalLength > 10000000) return; // Sanity check max 10MB frame

    // Check if it's a keyframe
    CFArrayRef attachments = CMSampleBufferGetSampleAttachmentsArray(sampleBuffer, false);
    int isKeyframe = 0;
    if (attachments != NULL && CFArrayGetCount(attachments) > 0) {
        CFDictionaryRef dict = (CFDictionaryRef)CFArrayGetValueAtIndex(attachments, 0);
        CFBooleanRef depends = (CFBooleanRef)CFDictionaryGetValue(dict, kCMSampleAttachmentKey_DependsOnOthers);
        isKeyframe = (depends == kCFBooleanFalse);
    }

    CMFormatDescriptionRef format = CMSampleBufferGetFormatDescription(sampleBuffer);
    
    size_t spsSize = 0, ppsSize = 0;
    const uint8_t *sps = NULL, *pps = NULL;
    
    if (isKeyframe && format) {
        size_t spsCount, ppsCount;
        CMVideoFormatDescriptionGetH264ParameterSetAtIndex(format, 0, &sps, &spsSize, &spsCount, NULL);
        CMVideoFormatDescriptionGetH264ParameterSetAtIndex(format, 1, &pps, &ppsSize, &ppsCount, NULL);
    }

    // Calculate total buffer size including SPS/PPS (with 4-byte AVCC length prefixes)
    size_t allocSize = totalLength;
    if (isKeyframe) {
        if (sps) allocSize += spsSize + 4;
        if (pps) allocSize += ppsSize + 4;
    }

    void* dataPointer = malloc(allocSize);
    if (!dataPointer) return;

    uint8_t* outPtr = (uint8_t*)dataPointer;

    if (isKeyframe) {
        if (sps) {
            uint32_t spsLenHost = (uint32_t)spsSize;
            uint32_t spsLenBig = CFSwapInt32HostToBig(spsLenHost);
            memcpy(outPtr, &spsLenBig, 4);
            memcpy(outPtr + 4, sps, spsSize);
            outPtr += spsSize + 4;
        }
        if (pps) {
            uint32_t ppsLenHost = (uint32_t)ppsSize;
            uint32_t ppsLenBig = CFSwapInt32HostToBig(ppsLenHost);
            memcpy(outPtr, &ppsLenBig, 4);
            memcpy(outPtr + 4, pps, ppsSize);
            outPtr += ppsSize + 4;
        }
    }

    OSStatus copyStatus = CMBlockBufferCopyDataBytes(dataBuffer, 0, totalLength, outPtr);
    if (copyStatus != kCMBlockBufferNoErr) {
        free(dataPointer);
        return;
    }

    // Pass the safely copied data to Go
    goEncodedFrameCallback((uintptr_t)outputCallbackRefCon, dataPointer, (int)allocSize, isKeyframe);
    
    // Free the copied data after Go has processed it (GoBytes makes its own copy)
    free(dataPointer);
}

typedef struct {
    VTCompressionSessionRef session;
    int width;
    int height;
    int fps;
    int64_t frame_count;
} VTEncoder;

VTEncoder* vt_encoder_create(int width, int height, int fps, int bitrate_kbps, uintptr_t handle) {
    VTEncoder* encoder = (VTEncoder*)malloc(sizeof(VTEncoder));
    encoder->width = width;
    encoder->height = height;
    encoder->fps = fps > 0 ? fps : 60;
    encoder->frame_count = 0;

    OSStatus status = VTCompressionSessionCreate(
        kCFAllocatorDefault,
        width, height,
        kCMVideoCodecType_H264,
        NULL, // encoderSpecification
        NULL, // sourceImageBufferAttributes
        NULL, // compressedDataAllocator
        compressionCallback,
        (void*)handle, // outputCallbackRefCon
        &encoder->session
    );

    if (status != noErr) {
        free(encoder);
        return NULL;
    }

    // Set properties for low latency
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_RealTime, kCFBooleanTrue);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_ProfileLevel, kVTProfileLevel_H264_High_AutoLevel);
    
    int bitrate = bitrate_kbps * 1000;
    CFNumberRef bitrateNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &bitrate);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_AverageBitRate, bitrateNum);
    CFRelease(bitrateNum);

    int limitBytes = (bitrate_kbps * 1200) / 8; // 1.2x average bitrate in bytes
    CFNumberRef bytesNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &limitBytes);
    double limitSeconds = 1.0;
    CFNumberRef secondsNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberDoubleType, &limitSeconds);
    CFTypeRef limitsArray[] = { bytesNum, secondsNum };
    CFArrayRef limits = CFArrayCreate(kCFAllocatorDefault, limitsArray, 2, &kCFTypeArrayCallBacks);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_DataRateLimits, limits);
    CFRelease(bytesNum);
    CFRelease(secondsNum);
    CFRelease(limits);

    int gop = fps; // 1 second GOP
    CFNumberRef gopNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &gop);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_MaxKeyFrameInterval, gopNum);
    CFRelease(gopNum);

    // Disable B-frames for lowest latency
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_AllowFrameReordering, kCFBooleanFalse);

    // Set expected frame rate to help the rate controller
    CFNumberRef fpsNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &fps);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_ExpectedFrameRate, fpsNum);
    CFRelease(fpsNum);

    // Ensure we don't delay frames for rate control
    int zero = 0;
    CFNumberRef zeroNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &zero);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_MaxFrameDelayCount, zeroNum);
    CFRelease(zeroNum);

    VTCompressionSessionPrepareToEncodeFrames(encoder->session);

    return encoder;
}

int vt_encoder_encode(VTEncoder* encoder, uint8_t* yuv_data, int force_keyframe) {
    CVPixelBufferRef pixelBuffer = NULL;

    // Create a new pixel buffer that owns its memory
    OSStatus status = CVPixelBufferCreate(
        kCFAllocatorDefault,
        encoder->width, encoder->height,
        kCVPixelFormatType_420YpCbCr8Planar,
        NULL,
        &pixelBuffer
    );

    if (status != kCVReturnSuccess || !pixelBuffer) {
        return -1;
    }

    // Lock the buffer and copy the data
    CVPixelBufferLockBaseAddress(pixelBuffer, 0);

    uint8_t* yDest = (uint8_t*)CVPixelBufferGetBaseAddressOfPlane(pixelBuffer, 0);
    uint8_t* uDest = (uint8_t*)CVPixelBufferGetBaseAddressOfPlane(pixelBuffer, 1);
    uint8_t* vDest = (uint8_t*)CVPixelBufferGetBaseAddressOfPlane(pixelBuffer, 2);

    size_t yStride = CVPixelBufferGetBytesPerRowOfPlane(pixelBuffer, 0);
    size_t uStride = CVPixelBufferGetBytesPerRowOfPlane(pixelBuffer, 1);
    size_t vStride = CVPixelBufferGetBytesPerRowOfPlane(pixelBuffer, 2);

    // Copy Y plane
    uint8_t* ySrc = yuv_data;
    for (int i = 0; i < encoder->height; i++) {
        memcpy(yDest + i * yStride, ySrc + i * encoder->width, encoder->width);
    }

    // Copy U plane
    uint8_t* uSrc = yuv_data + (encoder->width * encoder->height);
    for (int i = 0; i < encoder->height / 2; i++) {
        memcpy(uDest + i * uStride, uSrc + i * (encoder->width / 2), encoder->width / 2);
    }

    // Copy V plane
    uint8_t* vSrc = yuv_data + (encoder->width * encoder->height) + (encoder->width * encoder->height / 4);
    for (int i = 0; i < encoder->height / 2; i++) {
        memcpy(vDest + i * vStride, vSrc + i * (encoder->width / 2), encoder->width / 2);
    }

    CVPixelBufferUnlockBaseAddress(pixelBuffer, 0);

    CFDictionaryRef frameProps = NULL;
    if (force_keyframe) {
        CFTypeRef keys[] = { kVTEncodeFrameOptionKey_ForceKeyFrame };
        CFTypeRef values[] = { kCFBooleanTrue };
        frameProps = CFDictionaryCreate(kCFAllocatorDefault, keys, values, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    }

    CMTime pts = CMTimeMake(encoder->frame_count, encoder->fps);
    encoder->frame_count++;

    status = VTCompressionSessionEncodeFrame(
        encoder->session,
        pixelBuffer,
        pts, // PTS
        kCMTimeInvalid, // Duration
        frameProps, // frameProperties
        NULL, // sourceFrameRefCon
        NULL // infoFlagsOut
    );

    if (frameProps) {
        CFRelease(frameProps);
    }

    CVPixelBufferRelease(pixelBuffer);

    return (status == noErr) ? 0 : -1;
}

void vt_encoder_destroy(VTEncoder* encoder) {
    if (encoder->session) {
        VTCompressionSessionInvalidate(encoder->session);
        CFRelease(encoder->session);
    }
    free(encoder);
}
