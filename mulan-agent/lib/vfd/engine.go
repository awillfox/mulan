package vfd

import (
	"log"

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

func (e *Engine) Close() {
	e.Clear()
	e.port.Close()
	log.Println("VFD cleared, port closed")
}
