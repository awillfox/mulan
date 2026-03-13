package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.bug.st/serial"
)

func main() {
	port, err := serial.Open("COM3", &serial.Mode{
		BaudRate: 9600,
		DataBits: 8,
		StopBits: serial.OneStopBit,
		Parity:   serial.NoParity,
	})
	if err != nil {
		log.Fatalf("failed to open COM3: %v", err)
	}
	defer func() {
		vfdClear(port)
		port.Close()
		log.Println("VFD cleared, port closed")
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	messages := []string{"Hua Mulan", "Mulan Project"}
	i := 0

	log.Println("mulan-agent started, writing to VFD on COM3")
	for {
		msg := messages[i%len(messages)]
		vfdWrite(port, msg)
		log.Printf("VFD: %s", msg)
		i++

		select {
		case <-sig:
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func vfdClear(port serial.Port) {
	port.Write([]byte{0x0C})
}

func vfdWrite(port serial.Port, text string) {
	vfdClear(port)
	port.Write([]byte{0x0B})
	port.Write([]byte(text))
}
