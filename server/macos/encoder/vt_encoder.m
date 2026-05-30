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
            if (sps && spsSize > 0) {
                printf("VT Encoder H264 SPS hex: ");
                for (size_t i = 0; i < spsSize; i++) {
                    printf("%02x", sps[i]);
                }
                printf("\n");
                fflush(stdout);
            }
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

    // Calculate total buffer size including SPS/PPS/VPS
    size_t allocSize = totalLength + headerSize;

    void* dataPointer = malloc(allocSize);
    if (!dataPointer) return;

    uint8_t* outPtr = (uint8_t*)dataPointer;

    if (isKeyframe) {
        if (vps) {
            static const uint8_t startCode[] = {0, 0, 0, 1};
            memcpy(outPtr, startCode, 4);
            memcpy(outPtr + 4, vps, vpsSize);
            outPtr += vpsSize + 4;
        }
        if (sps) {
            static const uint8_t startCode[] = {0, 0, 0, 1};
            memcpy(outPtr, startCode, 4);
            memcpy(outPtr + 4, sps, spsSize);
            outPtr += spsSize + 4;
        }
        if (pps) {
            static const uint8_t startCode[] = {0, 0, 0, 1};
            memcpy(outPtr, startCode, 4);
            memcpy(outPtr + 4, pps, ppsSize);
            outPtr += ppsSize + 4;
        }
    }

    OSStatus copyStatus = CMBlockBufferCopyDataBytes(dataBuffer, 0, totalLength, outPtr);
    if (copyStatus != kCMBlockBufferNoErr) {
        free(dataPointer);
        return;
    }

    // Convert AVCC lengths to Annex-B start codes in the main data buffer
    size_t pos = 0;
    while (pos < totalLength - 4) {
        uint32_t naluLen;
        memcpy(&naluLen, outPtr + pos, 4);
        naluLen = CFSwapInt32BigToHost(naluLen);
        
        static const uint8_t startCode[] = {0, 0, 0, 1};
        memcpy(outPtr + pos, startCode, 4);
        
        pos += 4 + naluLen;
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
    OSType input_format;
    int64_t frame_count;
} VTEncoder;

void log_encoder_capabilities() {
    CFArrayRef encoderList = NULL;
    VTCopyVideoEncoderList(NULL, &encoderList);
    if (encoderList) {
        printf("VT Encoder: Available Encoders:\n");
        for (CFIndex i = 0; i < CFArrayGetCount(encoderList); i++) {
            CFDictionaryRef encoderDict = (CFDictionaryRef)CFArrayGetValueAtIndex(encoderList, i);
            CFStringRef name = (CFStringRef)CFDictionaryGetValue(encoderDict, kVTVideoEncoderList_EncoderName);
            CFStringRef codecName = (CFStringRef)CFDictionaryGetValue(encoderDict, kVTVideoEncoderList_CodecName);
            CFBooleanRef isHW = (CFBooleanRef)CFDictionaryGetValue(encoderDict, kVTVideoEncoderList_IsHardwareAccelerated);
            
            char nameBuf[256], codecBuf[256];
            CFStringGetCString(name, nameBuf, sizeof(nameBuf), kCFStringEncodingUTF8);
            CFStringGetCString(codecName, codecBuf, sizeof(codecBuf), kCFStringEncodingUTF8);
            printf("  - %s (%s) HW=%s\n", nameBuf, codecBuf, (isHW == kCFBooleanTrue) ? "YES" : "NO");
        }
        CFRelease(encoderList);
    }
    fflush(stdout);
}

VTEncoder* vt_encoder_create(const char* codec, int width, int height, int fps, int bitrate_kbps, int pix_fmt, uintptr_t handle) {
    log_encoder_capabilities();
    printf("VT Encoder: Creating for codec=%s, %dx%d, fps=%d, bitrate=%d, pix_fmt=%d\n", codec, width, height, fps, bitrate_kbps, pix_fmt);
    fflush(stdout);
    
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

    OSStatus status = -1;
    OSType candidates[] = {
        kCVPixelFormatType_444YpCbCr8BiPlanarVideoRange, // 444v
        kCVPixelFormatType_422YpCbCr8BiPlanarVideoRange, // 422v
        kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange  // 420v
    };
    int startIdx = (pix_fmt == 1) ? 0 : 2;

    for (int i = startIdx; i < 3; i++) {
        encoder->input_format = candidates[i];
        
        CFMutableDictionaryRef sourceAttributes = CFDictionaryCreateMutable(kCFAllocatorDefault, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
        CFNumberRef formatNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberSInt32Type, &encoder->input_format);
        CFDictionarySetValue(sourceAttributes, kCVPixelBufferPixelFormatTypeKey, formatNum);
        CFRelease(formatNum);

        CFMutableDictionaryRef encoderSpecs = CFDictionaryCreateMutable(kCFAllocatorDefault, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
        // REQUIRE hardware acceleration as requested by user.
        CFDictionarySetValue(encoderSpecs, kVTVideoEncoderSpecification_RequireHardwareAcceleratedVideoEncoder, kCFBooleanTrue);

        status = VTCompressionSessionCreate(
            kCFAllocatorDefault,
            width, height,
            codecType,
            encoderSpecs,
            sourceAttributes,
            NULL,
            compressionCallback,
            (void*)handle,
            &encoder->session
        );

        CFRelease(sourceAttributes);
        CFRelease(encoderSpecs);

        if (status == noErr) {
            printf("VT Encoder: Successfully created session with format: %c%c%c%c\n", 
                (char)((encoder->input_format >> 24) & 0xFF),
                (char)((encoder->input_format >> 16) & 0xFF),
                (char)((encoder->input_format >> 8) & 0xFF),
                (char)(encoder->input_format & 0xFF));

            CFBooleanRef usingHW = NULL;
            VTSessionCopyProperty(encoder->session, kVTCompressionPropertyKey_UsingHardwareAcceleratedVideoEncoder, kCFAllocatorDefault, &usingHW);
            if (usingHW) {
                printf("VT Encoder: Hardware Acceleration: %s\n", (usingHW == kCFBooleanTrue) ? "YES" : "NO");
                CFRelease(usingHW);
            }
            fflush(stdout);
            break;
        }
    }

    if (status != noErr) {
        fprintf(stderr, "VTCompressionSessionCreate failed: %d\n", (int)status);
        fflush(stderr);
        free(encoder);
        return NULL;
    }

    // Set ProfileLevel
    if (codecType == kCMVideoCodecType_H264) {
        if (pix_fmt == 1) {
            // H.264 4:4:4 or 4:2:2 profiles
            const CFStringRef h264_profiles[] = {
                CFSTR("H264_High444Predictive_AutoLevel"),
                CFSTR("H264_High422_AutoLevel"),
                kVTProfileLevel_H264_High_AutoLevel
            };
            for (int i = 0; i < 3; i++) {
                status = VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_ProfileLevel, h264_profiles[i]);
                if (status == noErr) {
                    char buf[256];
                    CFStringGetCString(h264_profiles[i], buf, sizeof(buf), kCFStringEncodingUTF8);
                    printf("VT Encoder: Successfully applied H264 profile: %s (Index %d)\n", buf, i);
                    break;
                } else {
                    printf("VT Encoder: Failed H264 profile index %d: %d\n", i, (int)status);
                }
            }
        } else {
            status = VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_ProfileLevel, kVTProfileLevel_H264_High_AutoLevel);
            printf("VT Encoder: Setting H264_High_AutoLevel status: %d\n", (int)status);
        }
    } else {
        // HEVC
        if (pix_fmt == 1) {
            const CFStringRef hevc_profiles[] = {
                CFSTR("HEVC_Main44410_AutoLevel"),
                CFSTR("HEVC_Main42210_AutoLevel"),
                CFSTR("HEVC_Main444_AutoLevel"),
                kVTProfileLevel_HEVC_Main10_AutoLevel,
                kVTProfileLevel_HEVC_Main_AutoLevel
            };
            for (int i = 0; i < 5; i++) {
                status = VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_ProfileLevel, hevc_profiles[i]);
                if (status == noErr) {
                    char buf[256];
                    CFStringGetCString(hevc_profiles[i], buf, sizeof(buf), kCFStringEncodingUTF8);
                    printf("VT Encoder: Successfully applied HEVC profile: %s (Index %d)\n", buf, i);
                    break;
                } else {
                    printf("VT Encoder: Failed HEVC profile index %d: %d\n", i, (int)status);
                }
            }
        } else {
            status = VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_ProfileLevel, kVTProfileLevel_HEVC_Main_AutoLevel);
            printf("VT Encoder: Setting HEVC_Main_AutoLevel status: %d\n", (int)status);
        }
    }
    fflush(stdout);

    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_RealTime, kCFBooleanTrue);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_ColorPrimaries, kCVImageBufferColorPrimaries_ITU_R_709_2);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_TransferFunction, kCVImageBufferTransferFunction_ITU_R_709_2);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_YCbCrMatrix, kCVImageBufferYCbCrMatrix_ITU_R_709_2);
    
    int bitrate = bitrate_kbps * 1000;
    CFNumberRef bitrateNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &bitrate);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_AverageBitRate, bitrateNum);
    CFRelease(bitrateNum);

    int gop = fps;
    CFNumberRef gopNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &gop);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_MaxKeyFrameInterval, gopNum);
    CFRelease(gopNum);

    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_AllowFrameReordering, kCFBooleanFalse);
    
    CFNumberRef fpsNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &fps);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_ExpectedFrameRate, fpsNum);
    CFRelease(fpsNum);

    int zero = 0;
    CFNumberRef zeroNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &zero);
    VTSessionSetProperty(encoder->session, kVTCompressionPropertyKey_MaxFrameDelayCount, zeroNum);
    CFRelease(zeroNum);

    VTCompressionSessionPrepareToEncodeFrames(encoder->session);
    return encoder;
}

int vt_encoder_encode(VTEncoder* encoder, uint8_t* yuv_data, int force_keyframe) {
    CVPixelBufferRef pixelBuffer = NULL;
    OSStatus status = CVPixelBufferCreate(kCFAllocatorDefault, encoder->width, encoder->height, encoder->input_format, NULL, &pixelBuffer);
    if (status != kCVReturnSuccess || !pixelBuffer) return -1;

    CVBufferSetAttachment(pixelBuffer, kCVImageBufferYCbCrMatrixKey, kCVImageBufferYCbCrMatrix_ITU_R_709_2, kCVAttachmentMode_ShouldPropagate);
    CVBufferSetAttachment(pixelBuffer, kCVImageBufferColorPrimariesKey, kCVImageBufferColorPrimaries_ITU_R_709_2, kCVAttachmentMode_ShouldPropagate);
    CVBufferSetAttachment(pixelBuffer, kCVImageBufferTransferFunctionKey, kCVImageBufferTransferFunction_ITU_R_709_2, kCVAttachmentMode_ShouldPropagate);

    CVPixelBufferLockBaseAddress(pixelBuffer, 0);

    uint8_t* yDest = (uint8_t*)CVPixelBufferGetBaseAddressOfPlane(pixelBuffer, 0);
    uint8_t* uvDest = (uint8_t*)CVPixelBufferGetBaseAddressOfPlane(pixelBuffer, 1);
    size_t yStride = CVPixelBufferGetBytesPerRowOfPlane(pixelBuffer, 0);
    size_t uvStride = CVPixelBufferGetBytesPerRowOfPlane(pixelBuffer, 1);
    
    uint8_t* ySrc = yuv_data;
    uint8_t* uSrc = yuv_data + (encoder->width * encoder->height);
    uint8_t* vSrc;

    // Plane offset based on input format knowledge
    if (encoder->pix_fmt == 1) {
        // Agent is sending yuv444p (full size U and V planes)
        vSrc = uSrc + (encoder->width * encoder->height);
    } else {
        // Agent is sending yuv420p (quarter size U and V planes)
        vSrc = uSrc + (encoder->width * encoder->height / 4);
    }

    // Copy Y
    for (int i = 0; i < encoder->height; i++) {
        memcpy(yDest + i * yStride, ySrc + i * encoder->width, encoder->width);
    }

    // Interleave U and V into Plane 1 (CbCr order)
    if (encoder->input_format == kCVPixelFormatType_444YpCbCr8BiPlanarVideoRange) {
        for (int i = 0; i < encoder->height; i++) {
            uint8_t* lineDest = uvDest + i * uvStride;
            uint8_t* lineUSrc = uSrc + i * encoder->width;
            uint8_t* lineVSrc = vSrc + i * encoder->width;
            for (int j = 0; j < encoder->width; j++) {
                // Restore CbCr (U then V) order for 4:4:4
                lineDest[j*2] = lineUSrc[j];
                lineDest[j*2+1] = lineVSrc[j];
            }
        }
    } else if (encoder->input_format == kCVPixelFormatType_422YpCbCr8BiPlanarVideoRange) {
        for (int i = 0; i < encoder->height; i++) {
            uint8_t* lineDest = uvDest + i * uvStride;
            uint8_t* lineUSrc = uSrc + i * encoder->width;
            uint8_t* lineVSrc = vSrc + i * encoder->width;
            for (int j = 0; j < encoder->width / 2; j++) {
                // Restore CbCr (U then V) order for 4:2:2
                lineDest[j*2] = lineUSrc[j*2];
                lineDest[j*2+1] = lineVSrc[j*2];
            }
        }
    } else {
        // 4:2:0 BiPlanar
        for (int i = 0; i < encoder->height / 2; i++) {
            uint8_t* lineDest = uvDest + i * uvStride;
            uint8_t* lineUSrc = uSrc + i * (encoder->width / 2);
            uint8_t* lineVSrc = vSrc + i * (encoder->width / 2);
            for (int j = 0; j < encoder->width / 2; j++) {
                // Restore Cb, Cr order for 4:2:0
                lineDest[j*2] = lineUSrc[j];
                lineDest[j*2+1] = lineVSrc[j];
            }
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

    status = VTCompressionSessionEncodeFrame(encoder->session, pixelBuffer, pts, kCMTimeInvalid, frameProps, NULL, NULL);

    if (frameProps) CFRelease(frameProps);
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
