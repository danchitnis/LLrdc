#include <VideoToolbox/VideoToolbox.h>
#include <CoreMedia/CoreMedia.h>
#include <CoreVideo/CoreVideo.h>
#include <mach/mach_time.h>
#include <stdio.h>
#include <string.h>

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
    FourCharCode codecType = CMFormatDescriptionGetMediaSubType(format);
    
    size_t headerSize = 0;
    const uint8_t *vps = NULL, *sps = NULL, *pps = NULL;
    size_t vpsSize = 0, spsSize = 0, ppsSize = 0;
    
    if (isKeyframe && format) {
        if (codecType == kCMVideoCodecType_H264) {
            size_t spsCount, ppsCount;
            CMVideoFormatDescriptionGetH264ParameterSetAtIndex(format, 0, &sps, &spsSize, &spsCount, NULL);
            CMVideoFormatDescriptionGetH264ParameterSetAtIndex(format, 1, &pps, &ppsSize, &ppsCount, NULL);
            if (sps) headerSize += spsSize + 4;
            if (pps) headerSize += ppsSize + 4;
        } else if (codecType == kCMVideoCodecType_HEVC) {
            size_t vpsCount, spsCount, ppsCount;
            CMVideoFormatDescriptionGetHEVCParameterSetAtIndex(format, 0, &vps, &vpsSize, &vpsCount, NULL);
            CMVideoFormatDescriptionGetHEVCParameterSetAtIndex(format, 1, &sps, &spsSize, &spsCount, NULL);
            CMVideoFormatDescriptionGetHEVCParameterSetAtIndex(format, 2, &pps, &ppsSize, &ppsCount, NULL);
            if (sps && spsSize > 0) {
                printf("VT Encoder HEVC SPS hex: ");
                for (size_t i = 0; i < spsSize; i++) {
                    printf("%02x", sps[i]);
                }
                printf("\n");
                fflush(stdout);
            }
            if (vps) headerSize += vpsSize + 4;
            if (sps) headerSize += spsSize + 4;
            if (pps) headerSize += ppsSize + 4;
        }
    }

    // Calculate total buffer size including SPS/PPS/VPS (with 4-byte AVCC/HVCC length prefixes)
    size_t allocSize = totalLength + headerSize;

    void* dataPointer = malloc(allocSize);
    if (!dataPointer) return;

    uint8_t* outPtr = (uint8_t*)dataPointer;

    if (isKeyframe) {
        if (vps) {
            uint32_t vpsLenBig = CFSwapInt32HostToBig((uint32_t)vpsSize);
            memcpy(outPtr, &vpsLenBig, 4);
            memcpy(outPtr + 4, vps, vpsSize);
            outPtr += vpsSize + 4;
        }
        if (sps) {
            uint32_t spsLenBig = CFSwapInt32HostToBig((uint32_t)spsSize);
            memcpy(outPtr, &spsLenBig, 4);
            memcpy(outPtr + 4, sps, spsSize);
            outPtr += spsSize + 4;
        }
        if (pps) {
            uint32_t ppsLenBig = CFSwapInt32HostToBig((uint32_t)ppsSize);
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
    int pix_fmt; // 0 for 420p, 1 for 444
    int64_t frame_count;
} VTEncoder;

VTEncoder* vt_encoder_create(const char* codec, int width, int height, int fps, int bitrate_kbps, int pix_fmt, uintptr_t handle) {
    VTEncoder* encoder = (VTEncoder*)malloc(sizeof(VTEncoder));
    encoder->width = width;
    encoder->height = height;
    encoder->fps = fps > 0 ? fps : 60;
    encoder->pix_fmt = pix_fmt;
    encoder->frame_count = 0;

    CMVideoCodecType codecType = kCMVideoCodecType_H264;
    if (strstr(codec, "h265") != NULL || strstr(codec, "hevc") != NULL) {
        codecType = kCMVideoCodecType_HEVC;
    }

    CFMutableDictionaryRef sourceAttributes = CFDictionaryCreateMutable(kCFAllocatorDefault, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    OSType inputFormat = kCVPixelFormatType_420YpCbCr8Planar;
    if (pix_fmt == 1) {
        inputFormat = kCVPixelFormatType_444YpCbCr8BiPlanarVideoRange;
    }
    CFNumberRef formatNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberSInt32Type, &inputFormat);
    CFDictionarySetValue(sourceAttributes, kCVPixelBufferPixelFormatTypeKey, formatNum);
    CFRelease(formatNum);

    OSStatus status = VTCompressionSessionCreate(
        kCFAllocatorDefault,
        width, height,
        codecType,
        NULL, // encoderSpecification
        sourceAttributes, // sourceImageBufferAttributes
        NULL, // compressedDataAllocator
        compressionCallback,
        (void*)handle, // outputCallbackRefCon
        &encoder->session
    );

    CFRelease(sourceAttributes);

    if (status != noErr) {
        fprintf(stderr, "VTCompressionSessionCreate failed: %d\n", (int)status);
        free(encoder);
        return NULL;
    }

    // Set properties for low latency
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_RealTime, kCFBooleanTrue);
    
    if (codecType == kCMVideoCodecType_H264) {
        if (pix_fmt == 1) {
            status = VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_ProfileLevel, CFSTR("H264_High444Predictive_AutoLevel"));
        } else {
            status = VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_ProfileLevel, kVTProfileLevel_H264_High_AutoLevel);
        }
    } else {
        // HEVC
        if (pix_fmt == 1) {
            status = VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_ProfileLevel, CFSTR("HEVC_Main444_AutoLevel"));
        } else {
            status = VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_ProfileLevel, kVTProfileLevel_HEVC_Main_AutoLevel);
        }
    }

    if (status != noErr) {
        fprintf(stderr, "Warning: Failed to set ProfileLevel: %d\n", (int)status);
    }
    
    // Set color space for screen capture
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_ColorPrimaries, kCVImageBufferColorPrimaries_ITU_R_709_2);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_TransferFunction, kCVImageBufferTransferFunction_ITU_R_709_2);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_YCbCrMatrix, kCVImageBufferYCbCrMatrix_ITU_R_709_2);
    
    int bitrate = bitrate_kbps * 1000;
    CFNumberRef bitrateNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &bitrate);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_AverageBitRate, bitrateNum);
    CFRelease(bitrateNum);

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
    
    OSType formatType = kCVPixelFormatType_420YpCbCr8Planar;
    if (encoder->pix_fmt == 1) {
        formatType = kCVPixelFormatType_444YpCbCr8BiPlanarVideoRange;
    }

    // Create a new pixel buffer that owns its memory
    OSStatus status = CVPixelBufferCreate(
        kCFAllocatorDefault,
        encoder->width, encoder->height,
        formatType,
        NULL,
        &pixelBuffer
    );

    if (status != kCVReturnSuccess || !pixelBuffer) {
        return -1;
    }

    // Set attachments for the buffer
    CVBufferSetAttachment(pixelBuffer, kCVImageBufferYCbCrMatrixKey, kCVImageBufferYCbCrMatrix_ITU_R_709_2, kCVAttachmentMode_ShouldPropagate);
    CVBufferSetAttachment(pixelBuffer, kCVImageBufferColorPrimariesKey, kCVImageBufferColorPrimaries_ITU_R_709_2, kCVAttachmentMode_ShouldPropagate);
    CVBufferSetAttachment(pixelBuffer, kCVImageBufferTransferFunctionKey, kCVImageBufferTransferFunction_ITU_R_709_2, kCVAttachmentMode_ShouldPropagate);

    // Lock the buffer and copy the data
    CVPixelBufferLockBaseAddress(pixelBuffer, 0);

    if (encoder->pix_fmt == 1) {
        // Bi-planar 444: Plane 0 is Y, Plane 1 is interleaved UV
        uint8_t* yDest = (uint8_t*)CVPixelBufferGetBaseAddressOfPlane(pixelBuffer, 0);
        uint8_t* uvDest = (uint8_t*)CVPixelBufferGetBaseAddressOfPlane(pixelBuffer, 1);
        
        size_t yStride = CVPixelBufferGetBytesPerRowOfPlane(pixelBuffer, 0);
        size_t uvStride = CVPixelBufferGetBytesPerRowOfPlane(pixelBuffer, 1);
        
        uint8_t* ySrc = yuv_data;
        uint8_t* uSrc = yuv_data + (encoder->width * encoder->height);
        uint8_t* vSrc = uSrc + (encoder->width * encoder->height);
        
        // Copy Y
        for (int i = 0; i < encoder->height; i++) {
            memcpy(yDest + i * yStride, ySrc + i * encoder->width, encoder->width);
        }
        
        // Interleave U and V into Plane 1 (CbCr order)
        for (int i = 0; i < encoder->height; i++) {
            uint8_t* lineDest = uvDest + i * uvStride;
            uint8_t* lineUSrc = uSrc + i * encoder->width;
            uint8_t* lineVSrc = vSrc + i * encoder->width;
            for (int j = 0; j < encoder->width; j++) {
                lineDest[j*2] = lineUSrc[j];
                lineDest[j*2+1] = lineVSrc[j];
            }
        }
    } else {
        // Planar 420
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
