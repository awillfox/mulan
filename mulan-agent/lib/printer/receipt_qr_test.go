package printer

import (
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mulan-agent/lib/promptpay"
)

// fakePrinter is a TCP server that stands in for the thermal printer and
// records every byte the receipt path writes to it.
type fakePrinter struct {
	ln   net.Listener
	mu   sync.Mutex
	data []byte
	done chan struct{}
}

func newFakePrinter(t *testing.T) *fakePrinter {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakePrinter{ln: ln, done: make(chan struct{})}
	go func() {
		defer close(f.done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				b, _ := io.ReadAll(conn)
				f.mu.Lock()
				f.data = append(f.data, b...)
				f.mu.Unlock()
			}()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakePrinter) addr() string { return f.ln.Addr().String() }

func (f *fakePrinter) bytes() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.data...)
}

func testItems() []OrderItem {
	return []OrderItem{{Name: "Americano", Qty: 1, Price: 23.0}}
}

// TestReceiptEmbedsScannableQR drives the real PrintReceipt against a fake
// printer, pulls every GS v 0 raster block out of the captured stream, and
// scans them. One of them must decode to the PromptPay payload for the order
// total — proving the QR reaches the print head correctly, not merely that the
// encoder works in isolation.
func TestReceiptEmbedsScannableQR(t *testing.T) {
	if _, err := exec.LookPath("zbarimg"); err != nil {
		t.Skip("zbarimg not installed")
	}
	fp := newFakePrinter(t)
	p, err := New(fp.addr(), nil, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pay := Payment{Method: "qr"}
	err = p.PrintReceipt("Test Shop", "", testItems(), 23, 0, 0, 0, 0, 23, pay, MemberInfo{}, "", "0923979957")
	if err != nil {
		t.Fatalf("PrintReceipt: %v", err)
	}
	time.Sleep(150 * time.Millisecond) // let the fake printer drain the conn

	want, err := promptpay.Payload("0923979957", 2300)
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}

	if got := scanRasters(t, fp.bytes()); !containsStr(got, want) {
		t.Errorf("no raster in the receipt decoded to the PromptPay payload\nwant: %s\ndecoded: %v", want, got)
	}
}

// TestReceiptOmitsQRForCash guards the condition: a cash sale must not print a
// payment QR even when a PromptPay id is configured.
func TestReceiptOmitsQRForCash(t *testing.T) {
	if _, err := exec.LookPath("zbarimg"); err != nil {
		t.Skip("zbarimg not installed")
	}
	fp := newFakePrinter(t)
	p, err := New(fp.addr(), nil, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pay := Payment{Method: "cash", Tendered: 50, Change: 27}
	if err := p.PrintReceipt("Test Shop", "", testItems(), 23, 0, 0, 0, 0, 23, pay, MemberInfo{}, "", "0923979957"); err != nil {
		t.Fatalf("PrintReceipt: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	for _, s := range scanRasters(t, fp.bytes()) {
		if strings.HasPrefix(s, "0002010102") {
			t.Errorf("cash receipt contains a PromptPay QR: %s", s)
		}
	}
}

// TestReceiptOmitsQRWhenUnconfigured guards the other half: QR payment but no
// PromptPay id means no QR block.
func TestReceiptOmitsQRWhenUnconfigured(t *testing.T) {
	if _, err := exec.LookPath("zbarimg"); err != nil {
		t.Skip("zbarimg not installed")
	}
	fp := newFakePrinter(t)
	p, err := New(fp.addr(), nil, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pay := Payment{Method: "qr"}
	if err := p.PrintReceipt("Test Shop", "", testItems(), 23, 0, 0, 0, 0, 23, pay, MemberInfo{}, "", ""); err != nil {
		t.Fatalf("PrintReceipt: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	for _, s := range scanRasters(t, fp.bytes()) {
		if strings.HasPrefix(s, "0002010102") {
			t.Errorf("receipt contains a QR despite no configured id: %s", s)
		}
	}
}

// scanRasters walks an ESC/POS byte stream, extracts every GS v 0 raster image
// (text lines are rasterized too, so there are many), and returns whatever
// zbarimg can decode from them.
func scanRasters(t *testing.T, stream []byte) []string {
	t.Helper()
	var decoded []string
	dir := t.TempDir()
	n := 0
	for i := 0; i+8 <= len(stream); {
		if !(stream[i] == 0x1D && stream[i+1] == 0x76 && stream[i+2] == 0x30) {
			i++
			continue
		}
		bytesPerRow := int(stream[i+4]) | int(stream[i+5])<<8
		height := int(stream[i+6]) | int(stream[i+7])<<8
		start := i + 8
		size := bytesPerRow * height
		if size <= 0 || start+size > len(stream) {
			i++
			continue
		}
		n++
		path := filepath.Join(dir, "raster", "")
		path = filepath.Join(dir, "r"+itoa(n)+".png")
		writeRasterPNG(t, path, stream[start:start+size], bytesPerRow, height)
		if out, err := exec.Command("zbarimg", "--raw", "-q", path).Output(); err == nil {
			if s := strings.TrimSpace(string(out)); s != "" {
				decoded = append(decoded, s)
			}
		}
		i = start + size
	}
	return decoded
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
