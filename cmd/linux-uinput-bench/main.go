//go:build linux && cgo

package main

/*
#include <errno.h>
#include <fcntl.h>
#include <linux/input.h>
#include <linux/uinput.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/time.h>
#include <unistd.h>

static int llrdc_open_uinput(const char* path) {
	return open(path, O_WRONLY | O_NONBLOCK);
}

static int llrdc_ioctl(int fd, unsigned long req, int arg) {
	return ioctl(fd, req, arg);
}

static int llrdc_write_setup(int fd, const char* name, int abs_max_x, int abs_max_y) {
	struct uinput_user_dev device;
	memset(&device, 0, sizeof(device));
	snprintf(device.name, UINPUT_MAX_NAME_SIZE, "%s", name);
	device.id.bustype = BUS_USB;
	device.id.vendor = 0x1d6b;
	device.id.product = 0x0104;
	device.id.version = 1;
	device.absmin[ABS_X] = 0;
	device.absmax[ABS_X] = abs_max_x;
	device.absmin[ABS_Y] = 0;
	device.absmax[ABS_Y] = abs_max_y;
	return write(fd, &device, sizeof(device));
}

static int llrdc_emit(int fd, unsigned short event_type, unsigned short code, int value) {
	struct input_event event;
	memset(&event, 0, sizeof(event));
	gettimeofday(&event.time, NULL);
	event.type = event_type;
	event.code = code;
	event.value = value;
	return write(fd, &event, sizeof(event));
}

static int llrdc_create_device(int fd) {
	return ioctl(fd, UI_DEV_CREATE);
}

static int llrdc_destroy_device(int fd) {
	return ioctl(fd, UI_DEV_DESTROY);
}
*/
import "C"

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

const (
	defaultDevicePath = "/dev/uinput"
	axisMax           = 65535
	defaultHold       = 12 * time.Millisecond
)

type response struct {
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
	SentAtMs    int64  `json:"sentAtMs,omitempty"`
	Command     string `json:"command,omitempty"`
	DevicePath  string `json:"devicePath,omitempty"`
	AxisMax     int    `json:"axisMax,omitempty"`
	Button      string `json:"button,omitempty"`
	ButtonState string `json:"buttonState,omitempty"`
	X           int    `json:"x,omitempty"`
	Y           int    `json:"y,omitempty"`
}

type injector struct {
	fd int
}

func main() {
	devicePath := strings.TrimSpace(os.Getenv("LLRDC_UINPUT_DEVICE"))
	if devicePath == "" {
		devicePath = defaultDevicePath
	}

	fd, err := openInjector(devicePath)
	if err != nil {
		emit(response{OK: false, Error: err.Error(), DevicePath: devicePath})
		os.Exit(1)
	}
	defer func() {
		_ = fd.close()
	}()

	emit(response{
		OK:         true,
		Command:    "ready",
		DevicePath: devicePath,
		AxisMax:    axisMax,
	})

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		reply := fd.handle(line)
		emit(reply)
		if strings.EqualFold(reply.Command, "quit") {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		emit(response{OK: false, Error: err.Error(), Command: "scanner"})
		os.Exit(1)
	}
}

func openInjector(devicePath string) (*injector, error) {
	cPath := C.CString(devicePath)
	defer C.free(unsafePointer(cPath))

	fd := int(C.llrdc_open_uinput(cPath))
	if fd < 0 {
		return nil, fmt.Errorf("open %s", devicePath)
	}

	closeOnError := func(err error) (*injector, error) {
		_ = C.close(C.int(fd))
		return nil, err
	}

	for _, ioctl := range []struct {
		req C.ulong
		arg int
	}{
		{req: C.UI_SET_EVBIT, arg: C.EV_KEY},
		{req: C.UI_SET_EVBIT, arg: C.EV_ABS},
		{req: C.UI_SET_EVBIT, arg: C.EV_SYN},
		{req: C.UI_SET_PROPBIT, arg: C.INPUT_PROP_POINTER},
		{req: C.UI_SET_KEYBIT, arg: C.BTN_LEFT},
		{req: C.UI_SET_KEYBIT, arg: C.BTN_RIGHT},
		{req: C.UI_SET_KEYBIT, arg: C.BTN_MOUSE},
		{req: C.UI_SET_KEYBIT, arg: C.BTN_TOOL_MOUSE},
		{req: C.UI_SET_ABSBIT, arg: C.ABS_X},
		{req: C.UI_SET_ABSBIT, arg: C.ABS_Y},
	} {
		if C.llrdc_ioctl(C.int(fd), ioctl.req, C.int(ioctl.arg)) != 0 {
			return closeOnError(fmt.Errorf("configure uinput device: ioctl %d failed", ioctl.arg))
		}
	}

	name := C.CString("LLrdc Bench Pointer")
	defer C.free(unsafePointer(name))
	if rc := int(C.llrdc_write_setup(C.int(fd), name, C.int(axisMax), C.int(axisMax))); rc < 0 {
		return closeOnError(fmt.Errorf("write uinput device setup"))
	}
	if C.llrdc_create_device(C.int(fd)) != 0 {
		return closeOnError(fmt.Errorf("create uinput device"))
	}

	time.Sleep(150 * time.Millisecond)
	i := &injector{fd: fd}
	if err := i.emitEvent(int(C.EV_KEY), int(C.BTN_TOOL_MOUSE), 1); err != nil {
		return closeOnError(err)
	}
	if err := i.sync(); err != nil {
		return closeOnError(err)
	}
	return i, nil
}

func (i *injector) close() error {
	if i == nil || i.fd <= 0 {
		return nil
	}
	_ = C.llrdc_destroy_device(C.int(i.fd))
	if rc := C.close(C.int(i.fd)); rc != 0 {
		return fmt.Errorf("close injector")
	}
	return nil
}

func (i *injector) handle(line string) response {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return response{OK: false, Error: "empty command"}
	}
	cmd := strings.ToLower(fields[0])
	switch cmd {
	case "quit", "exit":
		return response{OK: true, Command: "quit"}
	case "move":
		if len(fields) != 5 {
			return response{OK: false, Error: "usage: move <x> <y> <width> <height>", Command: "move"}
		}
		x, y, width, height, err := parseCoords(fields[1:])
		if err != nil {
			return response{OK: false, Error: err.Error(), Command: "move"}
		}
		sentAt, err := i.moveAbsolute(x, y, width, height)
		if err != nil {
			return response{OK: false, Error: err.Error(), Command: "move"}
		}
		return response{OK: true, Command: "move", SentAtMs: sentAt, X: x, Y: y}
	case "click":
		hold := defaultHold
		if len(fields) > 1 {
			parsed, err := strconv.Atoi(fields[1])
			if err != nil {
				return response{OK: false, Error: fmt.Sprintf("invalid hold ms: %v", err), Command: "click"}
			}
			hold = time.Duration(parsed) * time.Millisecond
		}
		sentAt, err := i.clickLeft(hold)
		if err != nil {
			return response{OK: false, Error: err.Error(), Command: "click"}
		}
		return response{OK: true, Command: "click", SentAtMs: sentAt, Button: "left"}
	case "click-at":
		if len(fields) < 5 || len(fields) > 6 {
			return response{OK: false, Error: "usage: click-at <x> <y> <width> <height> [hold_ms]", Command: "click-at"}
		}
		x, y, width, height, err := parseCoords(fields[1:5])
		if err != nil {
			return response{OK: false, Error: err.Error(), Command: "click-at"}
		}
		hold := defaultHold
		if len(fields) == 6 {
			parsed, err := strconv.Atoi(fields[5])
			if err != nil {
				return response{OK: false, Error: fmt.Sprintf("invalid hold ms: %v", err), Command: "click-at"}
			}
			hold = time.Duration(parsed) * time.Millisecond
		}
		sentAt, err := i.moveAbsolute(x, y, width, height)
		if err != nil {
			return response{OK: false, Error: err.Error(), Command: "click-at"}
		}
		sentAt, err = i.clickLeft(hold)
		if err != nil {
			return response{OK: false, Error: err.Error(), Command: "click-at"}
		}
		return response{OK: true, Command: "click-at", SentAtMs: sentAt, Button: "left", X: x, Y: y}
	default:
		return response{OK: false, Error: "unknown command", Command: cmd}
	}
}

func (i *injector) moveAbsolute(x, y, width, height int) (int64, error) {
	absX := scaleAxis(x, width)
	absY := scaleAxis(y, height)
	if err := i.emitEvent(int(C.EV_ABS), int(C.ABS_X), absX); err != nil {
		return 0, err
	}
	if err := i.emitEvent(int(C.EV_ABS), int(C.ABS_Y), absY); err != nil {
		return 0, err
	}
	sentAt := time.Now().UnixMilli()
	if err := i.sync(); err != nil {
		return 0, err
	}
	return sentAt, nil
}

func (i *injector) clickLeft(hold time.Duration) (int64, error) {
	if err := i.emitEvent(int(C.EV_KEY), int(C.BTN_LEFT), 1); err != nil {
		return 0, err
	}
	sentAt := time.Now().UnixMilli()
	if err := i.sync(); err != nil {
		return 0, err
	}
	time.Sleep(hold)
	if err := i.emitEvent(int(C.EV_KEY), int(C.BTN_LEFT), 0); err != nil {
		return 0, err
	}
	if err := i.sync(); err != nil {
		return 0, err
	}
	return sentAt, nil
}

func (i *injector) emitEvent(eventType, code, value int) error {
	if rc := int(C.llrdc_emit(C.int(i.fd), C.ushort(eventType), C.ushort(code), C.int(value))); rc < 0 {
		return fmt.Errorf("emit event type=%d code=%d", eventType, code)
	}
	return nil
}

func (i *injector) sync() error {
	return i.emitEvent(int(C.EV_SYN), int(C.SYN_REPORT), 0)
}

func parseCoords(args []string) (int, int, int, int, error) {
	if len(args) != 4 {
		return 0, 0, 0, 0, fmt.Errorf("expected 4 coordinate arguments")
	}
	values := make([]int, 4)
	for idx, raw := range args {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return 0, 0, 0, 0, fmt.Errorf("parse integer %q: %w", raw, err)
		}
		values[idx] = value
	}
	if values[2] <= 1 || values[3] <= 1 {
		return 0, 0, 0, 0, fmt.Errorf("width and height must be greater than 1")
	}
	return values[0], values[1], values[2], values[3], nil
}

func scaleAxis(pos, size int) int {
	if pos < 0 {
		pos = 0
	}
	maxPos := size - 1
	if pos > maxPos {
		pos = maxPos
	}
	return int((float64(pos) / float64(maxPos)) * float64(axisMax))
}

func emit(reply response) {
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(reply)
}

func unsafePointer[T any](ptr *T) unsafe.Pointer {
	return unsafe.Pointer(ptr)
}
