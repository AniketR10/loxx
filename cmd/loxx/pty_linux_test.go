package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// openPTY opens a new pseudo-terminal: master is the side the test types into
// and reads from, tty is the terminal the program runs in.
func openPTY() (master, tty *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	var unlock int32
	if err := ioctl(master, syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("unlocking pty: %w", err)
	}
	var n uint32
	if err := ioctl(master, syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("getting pty number: %w", err)
	}
	tty, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	return master, tty, nil
}
