package main

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// openPTY opens a pty; macOS names it with TIOCPTYGNAME (no TIOCGPTN).
func openPTY() (master, tty *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	if err := ioctl(master, syscall.TIOCPTYGRANT, 0); err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("granting pty: %w", err)
	}
	if err := ioctl(master, syscall.TIOCPTYUNLK, 0); err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("unlocking pty: %w", err)
	}
	var name [128]byte // TIOCPTYGNAME writes a NUL-terminated path of up to 128 bytes
	if err := ioctl(master, syscall.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("getting pty name: %w", err)
	}
	path, _, _ := bytes.Cut(name[:], []byte{0})
	tty, err = os.OpenFile(string(path), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	return master, tty, nil
}
