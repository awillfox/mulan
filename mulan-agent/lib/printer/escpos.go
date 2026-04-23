package printer

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"log"
	"net"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/encoding/charmap"
)

const printerWidth = 42

// ESC/POS commands
var (
	cmdInit        = []byte{0x1B, 0x40}
	cmdAlignLeft   = []byte{0x1B, 0x61, 0x00}
	cmdAlignCenter = []byte{0x1B, 0x61, 0x01}
	cmdBoldOn      = []byte{0x1B, 0x45, 0x01}
	cmdBoldOff     = []byte{0x1B, 0x45, 0x00}
	cmdCut         = []byte{0x1D, 0x56, 0x41, 0x10} // partial cut + feed
)

// Printer sends ESC/POS commands to a network receipt printer via raw TCP.
type Printer struct {
	addr      string
	logoBytes []byte // PNG bytes, nil = no logo
}

// New creates a Printer targeting addr. Does a quick dial test to fail fast.
// logoBytes is optional PNG image data; pass nil to skip the logo.
func New(addr string, logoBytes []byte) (*Printer, error) {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	conn.Close()
	log.Printf("receipt printer ready (tcp %s)", addr)
	return &Printer{addr: addr, logoBytes: logoBytes}, nil
}

// OrderItem is one line item on the receipt.
type OrderItem struct {
	Name  string
	Qty   int
	Price float64 // per unit, THB
}

// PrintReceipt opens a fresh TCP connection, prints a full receipt, and cuts.
func (p *Printer) PrintReceipt(storeName string, items []OrderItem, subtotal, vat, vatPercent, total float64) error {
	conn, err := net.DialTimeout("tcp", p.addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial printer: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	enc := charmap.Windows874.NewEncoder()
	encode := func(s string) []byte {
		b, err := enc.String(s)
		if err != nil {
			return []byte(s)
		}
		return []byte(b)
	}

	write := func(b []byte) { conn.Write(b) }
	writeln := func(s string) {
		conn.Write(encode(s))
		conn.Write([]byte{0x0A})
	}
	divider := func() { writeln(strings.Repeat("-", printerWidth)) }

	now := time.Now()

	write(cmdInit)
	time.Sleep(50 * time.Millisecond)

	// Logo
	if len(p.logoBytes) > 0 {
		write(cmdAlignCenter)
		if err := printLogo(conn, p.logoBytes, 400); err != nil {
			log.Printf("logo print skipped: %v", err)
		}
		writeln("")
	}

	// Header
	write(cmdAlignCenter)
	write(cmdBoldOn)
	writeln(centerText(storeName, printerWidth))
	write(cmdBoldOff)
	divider()

	// Date/time
	write(cmdAlignLeft)
	writeln(now.Format("2006-01-02  15:04:05"))
	divider()

	// Column header
	writeln(runesPadRight("Item", printerWidth-16) + fmt.Sprintf("%4s %11s", "Qty", "Amount"))
	divider()

	// Items — display-width padding so Thai combining chars align correctly
	for _, it := range items {
		name := displayTruncate(it.Name, printerWidth-16)
		amount := it.Price * float64(it.Qty)
		writeln(runesPadRight(name, printerWidth-16) + fmt.Sprintf("%4d %11.2f", it.Qty, amount))
	}

	// Subtotal, VAT, Total — skip Subtotal/VAT lines when VAT is 0.
	divider()
	if vat > 0 {
		writeln(runesPadRight("Subtotal", printerWidth-16) + fmt.Sprintf("%4s %9.2f ฿", "", subtotal))
		vatLabel := fmt.Sprintf("VAT %g%%", vatPercent)
		writeln(runesPadRight(vatLabel, printerWidth-16) + fmt.Sprintf("%4s %9.2f ฿", "", vat))
		divider()
	}
	write(cmdBoldOn)
	writeln(runesPadRight("TOTAL", printerWidth-16) + fmt.Sprintf("%4s %9.2f ฿", "", total))
	write(cmdBoldOff)
	divider()

	// Footer
	write(cmdAlignCenter)
	writeln("Thank you! Come again!")
	writeln("")
	writeln("")

	// Cut
	write(cmdCut)

	log.Printf("receipt printed: %d items, subtotal %.2f, VAT %.2f, total %.2f THB", len(items), subtotal, vat, total)
	return nil
}

// printLogo decodes PNG bytes, scales to maxWidth dots, converts to 1-bit,
// and sends as an ESC/POS raster bitmap (GS v 0).
func printLogo(conn net.Conn, pngBytes []byte, maxWidth int) error {
	src, _, err := image.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return fmt.Errorf("decode logo: %w", err)
	}

	// Scale down to maxWidth preserving aspect ratio
	srcW := src.Bounds().Dx()
	srcH := src.Bounds().Dy()
	dstW := srcW
	dstH := srcH
	if dstW > maxWidth {
		dstH = dstH * maxWidth / dstW
		dstW = maxWidth
	}
	// Round width up to nearest byte boundary (8 dots)
	dstW = (dstW + 7) &^ 7

	// Nearest-neighbour scale + threshold to 1-bit
	bytesPerRow := dstW / 8
	bitmap := make([]byte, bytesPerRow*dstH)

	for y := 0; y < dstH; y++ {
		srcY := src.Bounds().Min.Y + y*srcH/dstH
		for x := 0; x < dstW; x++ {
			srcX := src.Bounds().Min.X + x*srcW/dstW
			r, g, b, a := src.At(srcX, srcY).RGBA()
			// Treat transparent pixels as white
			if a < 0x8000 {
				continue
			}
			gray := color.Gray{Y: uint8((r*299 + g*587 + b*114) / 1000 >> 8)}
			if gray.Y < 128 { // dark → print dot
				bitmap[y*bytesPerRow+x/8] |= 1 << (7 - uint(x%8))
			}
		}
	}

	// GS v 0: raster bit image
	// 0x1D 0x76 0x30 m xL xH yL yH data
	xL := byte(bytesPerRow & 0xFF)
	xH := byte(bytesPerRow >> 8)
	yL := byte(dstH & 0xFF)
	yH := byte(dstH >> 8)
	conn.Write([]byte{0x1D, 0x76, 0x30, 0x00, xL, xH, yL, yH})
	conn.Write(bitmap)
	return nil
}

// displayWidth counts printable columns in s, skipping non-spacing marks
// (Thai vowels/tone marks like ี ้ ่ ั that sit above/below a base character).
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if !unicode.Is(unicode.Mn, r) {
			w++
		}
	}
	return w
}

// runesPadRight pads s to width display columns.
func runesPadRight(s string, width int) string {
	dw := displayWidth(s)
	if dw >= width {
		return displayTruncate(s, width)
	}
	return s + strings.Repeat(" ", width-dw)
}

// displayTruncate cuts s to at most max display columns.
func displayTruncate(s string, max int) string {
	w := 0
	for i, r := range s {
		if !unicode.Is(unicode.Mn, r) {
			if w == max {
				return s[:i]
			}
			w++
		}
	}
	return s
}

func centerText(s string, width int) string {
	dw := displayWidth(s)
	if dw >= width {
		return s
	}
	pad := (width - dw) / 2
	return strings.Repeat(" ", pad) + s
}
