# Docker Desktop / WSL2 Clock Drift Bug on Windows

## The Issue
When running Docker Desktop on Windows (which uses a WSL2 backend), the virtual machine's system clock can experience severe time synchronization drift relative to the Windows host clock. In testing, this drift was observed to be around 31 seconds. 

## Impact on FFmpeg and WebRTC Streaming
This massive clock drift directly impacts frame capture tools like FFmpeg's `x11grab`, which rely on the system clock to generate accurate Presentation Time Stamps (PTS) and Decoding Time Stamps (DTS).
When the timestamps from the virtualized X11 display jump or drift significantly:
1. **Using Wallclock Timestamps (`-use_wallclock_as_timestamps 1`):** FFmpeg attempts to use the drifted system time. Since the time is out of sync or jumping, FFmpeg assumes frames are arriving way too late or early and drops massive amounts of frames to "catch up." This results in the stream freezing or dropping to 0-1 FPS.
2. **DTS Out of Order Errors:** Without adjustments, the time jitter between X11 and the host clock causes `x11grab` to emit frames with non-monotonically increasing timestamps, causing FFmpeg to throw `DTS out of order` errors and stall the encoder pipeline.

## The Workaround
To perfectly stream WebRTC at 60 FPS in this environment, we must decouple FFmpeg's timing from the drifted system clock:
- Avoid `-use_wallclock_as_timestamps 1`.
- Use `-fflags nobuffer+genpts` in FFmpeg's global flags to continuously generate monotonically increasing timestamps.
- Use the `setpts=N/FRAME_RATE/TB` video filter to rewrite the presentation timestamps based purely on the frame count and constant framerate, completely ignoring the drifted X11 clock values.

This ensures the WebRTC client receives a perfectly stable, constant frame rate video stream, regardless of how badly the WSL2 VM clock has drifted.
