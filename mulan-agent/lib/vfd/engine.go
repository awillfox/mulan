package vfd

import (
	"log"
	"strings"

	"go.bug.st/serial"
)

type Engine struct {
	port serial.Port
}

func New(portName string) (*Engine, error) {
	port, err := serial.Open(portName, &serial.Mode{
		BaudRate: 9600,
		DataBits: 8,
		StopBits: serial.OneStopBit,
		Parity:   serial.NoParity,
	})
	if err != nil {
		return nil, err
	}

	log.Printf("VFD started on %s", portName)
	return &Engine{port: port}, nil
}

func (e *Engine) Clear() {
	e.port.Write([]byte{0x0C})
}

func (e *Engine) Write(text string) {
	e.Clear()
	e.port.Write([]byte{0x0B})
	e.port.Write([]byte(text))
}

// WriteLines writes two lines on a 20-char-wide VFD.
// Line 1 is padded to exactly 20 chars so the cursor wraps to line 2.
func (e *Engine) WriteLines(line1, line2 string) {
	if len(line1) < 20 {
		line1 += strings.Repeat(" ", 20-len(line1))
	} else if len(line1) > 20 {
		line1 = line1[:20]
	}
	if len(line2) > 20 {
		line2 = line2[:20]
	}
	e.Clear()
	e.port.Write([]byte{0x0B})
	e.port.Write([]byte(line1 + line2))
}

func (e *Engine) Close() {
	e.Clear()
	e.port.Close()
	log.Println("VFD cleared, port closed")
}
