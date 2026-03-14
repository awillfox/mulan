//go:build windows

package cashdrawer

import (
	"fmt"
	"log"
	"syscall"
	"time"
)

const (
	ioPort    uintptr = 0x482
	assertVal uintptr = 0x10
)

var (
	writePort *syscall.Proc
	initErr   error
)

// Init loads inpoutx64.dll from the given path and verifies the driver is running.
// Must be called once after config is loaded.
func Init(dllPath string) {
	dll, err := syscall.LoadDLL(dllPath)
	if err != nil {
		initErr = fmt.Errorf("failed to load %s: %w", dllPath, err)
		log.Printf("cash drawer unavailable: %v", initErr)
		return
	}

	if isOpen, err := dll.FindProc("IsInpOutDriverOpen"); err == nil {
		r, _, _ := isOpen.Call()
		if r == 0 {
			initErr = fmt.Errorf("inpoutx64 kernel driver is not running")
			log.Printf("cash drawer unavailable: %v", initErr)
			return
		}
		log.Println("cash drawer: inpoutx64 driver confirmed open")
	}

	proc, err := dll.FindProc("Out32")
	if err != nil {
		initErr = fmt.Errorf("Out32 not found in %s: %w", dllPath, err)
		log.Printf("cash drawer unavailable: %v", initErr)
		return
	}

	writePort = proc
	log.Printf("cash drawer ready (%s, port 0x%03X)", dllPath, ioPort)
}

// Open pulses bit4 of I/O port 0x482 to kick the GS-410B cash drawer.
func Open() error {
	if initErr != nil {
		return initErr
	}
	if writePort == nil {
		return fmt.Errorf("cash drawer not initialised — call Init() first")
	}
	log.Printf("cash drawer: asserting 0x%02X on port 0x%03X", assertVal, ioPort)
	writePort.Call(ioPort, assertVal)
	time.Sleep(100 * time.Millisecond)
	time.Sleep(500 * time.Millisecond)
	writePort.Call(ioPort, 0x00)
	log.Println("cash drawer: pulse complete")
	return nil
}
