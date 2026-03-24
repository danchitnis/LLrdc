package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"syscall"
	"unsafe"
)

// Linux input constants
const (
	EV_SYN = 0x00
	EV_KEY = 0x01
	EV_REL = 0x02
	EV_ABS = 0x03

	BTN_LEFT   = 0x110
	BTN_RIGHT  = 0x111
	BTN_MIDDLE = 0x112

	ABS_X = 0x00
	ABS_Y = 0x01

	SYN_REPORT = 0

	UI_SET_EVBIT  = 0x40045564
	UI_SET_KEYBIT = 0x40045565
	UI_SET_ABSBIT = 0x40045567
	UI_DEV_CREATE = 0x5501
	UI_DEV_DESTROY = 0x5502
	UI_DEV_SETUP  = 0x405c5503
)

type uinputSetup struct {
	id struct {
		bustype uint16
		vendor  uint16
		product uint16
		version uint16
	}
	name [80]byte
	ff_effects_max uint32
}

type absSetup struct {
	code    uint16
	_       uint16 // padding
	absinfo struct {
		value      int32
		minimum    int32
		maximum    int32
		fuzz       int32
		flat       int32
		resolution int32
	}
}

const UI_ABS_SETUP = 0x401c5504

type UinputDevice struct {
	file *os.File
}

func CreateUinputDevice() (*UinputDevice, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY|syscall.O_NONBLOCK, 0660)
	if err != nil {
		return nil, fmt.Errorf("failed to open /dev/uinput: %v", err)
	}

	// Enable Events
	ioctl(f.Fd(), UI_SET_EVBIT, EV_KEY)
	ioctl(f.Fd(), UI_SET_EVBIT, EV_ABS)
	
	// Enable Buttons
	ioctl(f.Fd(), UI_SET_KEYBIT, BTN_LEFT)
	ioctl(f.Fd(), UI_SET_KEYBIT, BTN_RIGHT)
	ioctl(f.Fd(), UI_SET_KEYBIT, BTN_MIDDLE)

	// Enable Absolute Axes
	ioctl(f.Fd(), UI_SET_ABSBIT, ABS_X)
	ioctl(f.Fd(), UI_SET_ABSBIT, ABS_Y)

	// Setup device
	var setup uinputSetup
	setup.id.bustype = 0x03 // USB
	setup.id.vendor = 0x1234
	setup.id.product = 0x5678
	copy(setup.name[:], "LLrdc Virtual Mouse")

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), UI_DEV_SETUP, uintptr(unsafe.Pointer(&setup))); errno != 0 {
		return nil, fmt.Errorf("failed UI_DEV_SETUP: %v", errno)
	}

	// Setup ABS ranges (0-1280, 0-720)
	setAbs(f.Fd(), ABS_X, 0, 1280)
	setAbs(f.Fd(), ABS_Y, 0, 720)

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), UI_DEV_CREATE, 0); errno != 0 {
		return nil, fmt.Errorf("failed UI_DEV_CREATE: %v", errno)
	}

	log.Println("Uinput virtual mouse device created successfully.")
	return &UinputDevice{file: f}, nil
}

func (d *UinputDevice) MoveAbs(x, y int) {
	d.writeEvent(EV_ABS, ABS_X, int32(x))
	d.writeEvent(EV_ABS, ABS_Y, int32(y))
	d.writeEvent(EV_SYN, SYN_REPORT, 0)
}

func (d *UinputDevice) Button(code uint16, down bool) {
	val := int32(0)
	if down {
		val = 1
	}
	d.writeEvent(EV_KEY, code, val)
	d.writeEvent(EV_SYN, SYN_REPORT, 0)
}

func (d *UinputDevice) writeEvent(typ, code uint16, val int32) {
	buf := new(bytes.Buffer)
	// 64-bit timeval: 8 bytes sec, 8 bytes usec
	binary.Write(buf, binary.LittleEndian, int64(0)) // sec
	binary.Write(buf, binary.LittleEndian, int64(0)) // usec
	binary.Write(buf, binary.LittleEndian, typ)
	binary.Write(buf, binary.LittleEndian, code)
	binary.Write(buf, binary.LittleEndian, val)
	
	d.file.Write(buf.Bytes())
}

func (d *UinputDevice) Close() {
	syscall.Syscall(syscall.SYS_IOCTL, d.file.Fd(), UI_DEV_DESTROY, 0)
	d.file.Close()
}

func ioctl(fd uintptr, request uintptr, arg int) {
	syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(arg))
}

func setAbs(fd uintptr, code uint16, min, max int32) {
	var abs absSetup
	abs.code = code
	abs.absinfo.minimum = min
	abs.absinfo.maximum = max
	syscall.Syscall(syscall.SYS_IOCTL, fd, UI_ABS_SETUP, uintptr(unsafe.Pointer(&abs)))
}
