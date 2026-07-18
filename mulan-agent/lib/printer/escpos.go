package printer

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"log"
	"math"
	"net"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/encoding/charmap"

	"mulan-agent/lib/promptpay"
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

// cmdSelectCodepage builds the "ESC t n" sequence which switches the
// printer's active character-code table. For Thai we send a TIS-620-compatible
// table so the printer knows the incoming bytes are Thai and composes the
// vowels/tone marks above the base glyph instead of advancing horizontally
// (the classic "overlapping" symptom). Common Thai code-page indices:
//
//	21 — Thai Character Code 42 (Epson default factory setting on some units)
//	26 — Thai Character Code 18 / TIS-620 (most Epson TM-T models)
//	30 — Thai Character Code 16
//
// Default 26 works for most Epson TM-T series. Override via the
// RECEIPT_PRINTER_CODEPAGE env var if your printer uses a different index.
func cmdSelectCodepage(n byte) []byte {
	return []byte{0x1B, 0x74, n}
}

// Printer sends ESC/POS commands to a network receipt printer via raw TCP.
type Printer struct {
	addr      string
	logoBytes []byte // PNG bytes, nil = no logo
	codepage  byte   // ESC t value, 0 = don't send (use printer default)
}

// New creates a Printer targeting addr. Does a quick dial test to fail fast.
// logoBytes is optional PNG image data; pass nil to skip the logo.
// codepage is the ESC/POS character code table index sent after init —
// pass 0 to leave the printer on its current setting.
func New(addr string, logoBytes []byte, codepage byte) (*Printer, error) {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	conn.Close()
	if codepage != 0 {
		log.Printf("receipt printer ready (tcp %s, codepage %d)", addr, codepage)
	} else {
		log.Printf("receipt printer ready (tcp %s)", addr)
	}
	return &Printer{addr: addr, logoBytes: logoBytes, codepage: codepage}, nil
}

// writeInit sends ESC @ then optionally the configured codepage. Thai
// composition is left to the printer's native rendering (the Epson FS C
// command breaks Thai on VOZY G80 and other Chinese ESC/POS clones).
func (p *Printer) writeInit(conn net.Conn) {
	conn.Write(cmdInit)
	if p.codepage != 0 {
		conn.Write(cmdSelectCodepage(p.codepage))
	}
}

// OrderItemOption is one selected option for an order line.
type OrderItemOption struct {
	Name       string
	PriceDelta float64 // THB
}

// OrderItem is one line item on the receipt.
type OrderItem struct {
	Name           string
	Qty            int
	Price          float64 // per unit base price, THB
	Options        []OrderItemOption
	BaseOptionName string // chosen base option, e.g. "Iced" (empty = none)
}

func (it OrderItem) UnitPrice() float64 {
	p := it.Price
	for _, o := range it.Options {
		p += o.PriceDelta
	}
	return p
}

// displayName returns the line label with the chosen base option in
// parentheses, e.g. "Americano (Iced)". The base option's price is already
// baked into Price, so it is never printed as a separate sub-line.
func (it OrderItem) displayName() string {
	if it.BaseOptionName != "" {
		return it.Name + " (" + it.BaseOptionName + ")"
	}
	return it.Name
}

// MemberInfo is the optional loyalty block printed on the receipt footer.
type MemberInfo struct {
	Present bool
	Name    string
	Phone   string
	Earned  int64 // points earned this order
	Balance int64 // running total after this order
}

// PrintOrderBill opens a fresh TCP connection, prints a kitchen-style order
// bill (item list, no prices), and cuts.
func (p *Printer) PrintOrderBill(orderCode string, items []OrderItem) error {
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
	// Helpers below render text into raster bitmaps via IBM Plex Sans Thai
	// Looped, so the whole receipt is one consistent font with proper Thai
	// composition. ESC alignment commands don't apply to raster, so each
	// helper bakes its alignment into the bitmap.
	writeln := func(s string) {
		if err := renderAndSendLine(conn, s); err != nil {
			conn.Write(encode(s))
			conn.Write([]byte{0x0A})
		}
	}
	writeCenter := func(s string) {
		if err := renderAndSendCenter(conn, s); err != nil {
			conn.Write(encode(s))
			conn.Write([]byte{0x0A})
		}
	}
	divider := func() { renderAndSendDivider(conn) }

	p.writeInit(conn)
	time.Sleep(50 * time.Millisecond)

	// Header
	writeCenter("ORDER BILL")
	divider()

	// Order code + time
	if orderCode != "" {
		writeln("Order: " + orderCode)
	}
	writeln(time.Now().Format("2006-01-02  15:04:05"))
	divider()

	// Items — qty x name, no price; options indented as sub-lines
	for _, it := range items {
		writeln(fmt.Sprintf("%d x %s", it.Qty, it.displayName()))
		for _, o := range it.Options {
			writeln("    - " + o.Name)
		}
	}

	writeln("")
	writeln("")
	write(cmdCut)

	log.Printf("order bill printed: code=%s, %d items", orderCode, len(items))
	return nil
}

// PrintReceipt opens a fresh TCP connection, prints a full receipt, and cuts.
// Payment describes how an order was settled, printed in the receipt's
// payment section. Tendered/Change are only meaningful for cash.
type Payment struct {
	Method   string  // "cash" | "card" | "qr" (empty treated as cash)
	Tendered float64 // THB cash handed over
	Change   float64 // THB change returned
}

// label returns the human-readable payment method for the receipt.
func (pm Payment) label() string {
	switch pm.Method {
	case "card":
		return "Credit Card"
	case "qr":
		return "QR Payment"
	default:
		return "Cash"
	}
}

// PrintReceipt prints a full receipt. subtotal is the gross (VAT-inclusive)
// total, discount is the combined normal THB taken off, subsidy is the
// sponsor-covered THB (customer pays less but the shop is made whole), vat is
// the inclusive VAT portion of the shop-received amount, and total is what the
// customer actually pays.
// promptPayID, when non-empty and the order was paid by QR, adds a payable
// PromptPay QR for total at the foot of the receipt.
func (p *Printer) PrintReceipt(storeName, footer string, items []OrderItem, subtotal, discount, subsidy, vat, vatPercent, total float64, pay Payment, member MemberInfo, wifiUsername, promptPayID string) error {
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
		if err := renderAndSendLine(conn, s); err != nil {
			conn.Write(encode(s))
			conn.Write([]byte{0x0A})
		}
	}
	writeCenter := func(s string) {
		if err := renderAndSendCenter(conn, s); err != nil {
			conn.Write(encode(s))
			conn.Write([]byte{0x0A})
		}
	}
	writeRow := func(left, right string) {
		if err := renderAndSendRow(conn, left, right); err != nil {
			conn.Write(encode(left + "  " + right))
			conn.Write([]byte{0x0A})
		}
	}
	divider := func() { renderAndSendDivider(conn) }

	now := time.Now()

	p.writeInit(conn)
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
	writeCenter(storeName)
	divider()

	// Date/time
	writeln(now.Format("2006-01-02  15:04:05"))
	divider()

	// Column header
	writeRow("Item", "Qty       Amount")
	divider()

	// Items — proportional text on left, right-aligned amounts pinned to
	// the print edge so column reading stays clean across all rows.
	for _, it := range items {
		amount := it.UnitPrice() * float64(it.Qty)
		writeRow(it.displayName(), fmt.Sprintf("%d   %9.2f", it.Qty, amount))
		for _, o := range it.Options {
			delta := ""
			if o.PriceDelta > 0 {
				delta = fmt.Sprintf("%9.2f", o.PriceDelta)
			} else if o.PriceDelta < 0 {
				delta = fmt.Sprintf("-%9.2f", -o.PriceDelta)
			}
			writeRow("  + "+o.Name, delta)
		}
	}

	// Subtotal, Discount, VAT, Total. Subtotal/VAT lines print when there's
	// VAT or a discount to break down; otherwise just the TOTAL.
	divider()
	if vat > 0 || discount > 0 || subsidy > 0 {
		writeRow("Subtotal", fmt.Sprintf("%9.2f ฿", subtotal))
		if discount > 0 {
			writeRow("Discount", fmt.Sprintf("-%9.2f ฿", discount))
		}
		if subsidy > 0 {
			writeRow("Subsidy", fmt.Sprintf("-%9.2f ฿", subsidy))
		}
		if vat > 0 {
			writeRow(fmt.Sprintf("VAT %g%% incl", vatPercent), fmt.Sprintf("%9.2f ฿", vat))
		}
		divider()
	}
	writeRow("TOTAL", fmt.Sprintf("%9.2f ฿", total))
	divider()

	// Payment section — method, plus cash tendered/change when paid in cash.
	writeRow("Payment", pay.label())
	if pay.Method == "" || pay.Method == "cash" {
		if pay.Tendered > 0 {
			writeRow("Cash received", fmt.Sprintf("%9.2f ฿", pay.Tendered))
		}
		if pay.Change > 0 {
			writeRow("Change", fmt.Sprintf("%9.2f ฿", pay.Change))
		}
	}
	divider()

	// Member loyalty footer — only shown when a member was identified.
	if member.Present {
		divider()
		name := member.Name
		if name == "" {
			name = member.Phone
		}
		writeRow("Member", name)
		if member.Name != "" {
			writeln("  " + member.Phone)
		}
		writeRow("Points earned", fmt.Sprintf("%d pts", member.Earned))
		writeRow("Points balance", fmt.Sprintf("%d pts", member.Balance))
	}

	// Guest WiFi block — only when an account was assigned.
	if wifiUsername != "" {
		divider()
		writeCenter("Free WiFi")
		writeCenter("SSID: THCoffee_Guest")
		writeRow("Username", wifiUsername)
		writeCenter("(valid 2 hours)")
	}

	// PromptPay QR — only for QR-paid orders, and only when the terminal has a
	// PromptPay id configured. The amount is embedded, so the customer scans
	// and confirms without typing anything.
	//
	// A failure here is logged and skipped rather than aborting: the rest of
	// the receipt is still a valid record of the sale, and returning an error
	// would leave the cashier with no slip at all.
	if pay.Method == "qr" && promptPayID != "" {
		satang := int64(math.Round(total * 100))
		if payload, err := promptpay.Payload(promptPayID, satang); err != nil {
			log.Printf("promptpay qr skipped: %v", err)
		} else {
			divider()
			writeCenter("Scan to pay with PromptPay")
			writeln("")
			write(cmdAlignCenter)
			if err := printQR(conn, payload, 360); err != nil {
				log.Printf("promptpay qr print failed: %v", err)
			}
			write(cmdAlignLeft)
			writeln("")
			writeCenter(fmt.Sprintf("%.2f THB", total))
		}
	}

	// Footer — configurable in manager settings. Falls back to a default
	// when the shop hasn't set one.
	footerText := footer
	if footerText == "" {
		footerText = "Thank you! Come again!"
	}
	divider()
	writeCenter(footerText)
	writeln("")
	writeln("")

	// Cut
	write(cmdCut)

	log.Printf("receipt printed: %d items, subtotal %.2f, discount %.2f, subsidy %.2f, VAT %.2f, total %.2f THB", len(items), subtotal, discount, subsidy, vat, total)
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
