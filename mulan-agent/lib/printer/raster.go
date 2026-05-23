package printer

import (
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"net"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// thaiFontBytes is bundled into the binary so deploys don't need to ship a
// font file alongside the agent. IBM Plex Sans Thai Looped covers both Thai
// (with looped consonants — the standard Thai receipt/textbook style) and
// the full Basic Latin range including digits and punctuation, so a single
// face renders mixed lines like "ชาดำ   2   150.00".
//
//go:embed fonts/IBMPlexSansThaiLooped-Regular.ttf
var thaiFontBytes []byte

const (
	// 80mm thermal receipt printers (VOZY G80 etc.) have a 576-dot print
	// width. Width must be a multiple of 8 for the GS v 0 raster format.
	rasterWidth = 576

	// Proportional rendering — every line uses the font's natural advance
	// widths so Latin words look like a normal book rather than a 1980s
	// terminal. Column alignment is handled by the two-column helper
	// (renderTwoColGray) which measures the right segment and positions
	// it at the print edge.
	rasterFontSize   = 22
	rasterDPI        = 96
	rasterLineHeight = 36
	rasterBaselineY  = 28
)

var (
	faceOnce   sync.Once
	cachedFace font.Face
	faceErr    error
)

func loadFace() (font.Face, error) {
	faceOnce.Do(func() {
		t, err := opentype.Parse(thaiFontBytes)
		if err != nil {
			faceErr = fmt.Errorf("parse thai font: %w", err)
			return
		}
		cachedFace, faceErr = opentype.NewFace(t, &opentype.FaceOptions{
			Size:    rasterFontSize,
			DPI:     rasterDPI,
			Hinting: font.HintingFull,
		})
	})
	return cachedFace, faceErr
}

// newBlankRow returns a white-filled Gray image sized for one receipt row,
// ready for text drawing.
func newBlankRow() *image.Gray {
	img := image.NewGray(image.Rect(0, 0, rasterWidth, rasterLineHeight))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	return img
}

func newDrawer(img *image.Gray, face font.Face) *font.Drawer {
	return &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.Black),
		Face: face,
	}
}

// renderLineGray rasterizes s left-aligned at proportional advance.
// IBM Plex Sans Thai Looped sets combining marks (Mn) with zero advance
// and negative left bearing, so DrawString overlays them correctly on the
// preceding base glyph without any manual Thai shaping logic.
func renderLineGray(s string) (*image.Gray, error) {
	face, err := loadFace()
	if err != nil {
		return nil, err
	}
	img := newBlankRow()
	d := newDrawer(img, face)
	d.Dot = fixed.P(0, rasterBaselineY)
	d.DrawString(s)
	return img, nil
}

// renderCenterGray draws s horizontally centred within the print head width.
// Used for header text since ESC a (alignment) commands don't apply to
// raster bitmaps — alignment must be baked into the image.
func renderCenterGray(s string) (*image.Gray, error) {
	face, err := loadFace()
	if err != nil {
		return nil, err
	}
	img := newBlankRow()
	d := newDrawer(img, face)
	w := d.MeasureString(s)
	x := (fixed.I(rasterWidth) - w) / 2
	if x < 0 {
		x = 0
	}
	d.Dot = fixed.Point26_6{X: x, Y: fixed.I(rasterBaselineY)}
	d.DrawString(s)
	return img, nil
}

// renderTwoColGray draws left at the print edge and right flush against the
// far edge. If their widths overlap, left is truncated (right always wins —
// money columns must stay readable for cashier reconciliation).
func renderTwoColGray(left, right string) (*image.Gray, error) {
	face, err := loadFace()
	if err != nil {
		return nil, err
	}
	img := newBlankRow()
	d := newDrawer(img, face)

	rightW := d.MeasureString(right)
	rightX := fixed.I(rasterWidth) - rightW
	if rightX < 0 {
		rightX = 0
	}
	d.Dot = fixed.Point26_6{X: rightX, Y: fixed.I(rasterBaselineY)}
	d.DrawString(right)

	// Truncate left so it can't visually collide with right. Reserve a
	// 12-dot gutter so digits don't touch the previous segment.
	leftLimit := rightX - fixed.I(12)
	d.Dot = fixed.P(0, rasterBaselineY)
	for _, r := range left {
		w := d.MeasureString(string(r))
		if d.Dot.X+w > leftLimit {
			break
		}
		d.DrawString(string(r))
	}
	return img, nil
}

// sendRasterLine converts a Gray image to a 1-bit ESC/POS raster bitmap
// (GS v 0) and writes it to conn. Pixels darker than 128 print as dots.
func sendRasterLine(conn net.Conn, gray *image.Gray) error {
	w := gray.Bounds().Dx()
	h := gray.Bounds().Dy()
	bytesPerRow := (w + 7) / 8
	bitmap := make([]byte, bytesPerRow*h)

	for y := 0; y < h; y++ {
		row := y * bytesPerRow
		for x := 0; x < w; x++ {
			if gray.GrayAt(x, y).Y < 128 {
				bitmap[row+x/8] |= 1 << (7 - uint(x%8))
			}
		}
	}

	xL := byte(bytesPerRow & 0xFF)
	xH := byte(bytesPerRow >> 8)
	yL := byte(h & 0xFF)
	yH := byte(h >> 8)
	if _, err := conn.Write([]byte{0x1D, 0x76, 0x30, 0x00, xL, xH, yL, yH}); err != nil {
		return err
	}
	if _, err := conn.Write(bitmap); err != nil {
		return err
	}
	return nil
}

// renderAndSendLine rasterizes s left-aligned and prints it.
func renderAndSendLine(conn net.Conn, s string) error {
	img, err := renderLineGray(s)
	if err != nil {
		return err
	}
	return sendRasterLine(conn, img)
}

// renderAndSendCenter rasterizes s centred and prints it.
func renderAndSendCenter(conn net.Conn, s string) error {
	img, err := renderCenterGray(s)
	if err != nil {
		return err
	}
	return sendRasterLine(conn, img)
}

// renderAndSendRow draws a two-column row: left flush left, right flush right.
// Used for item rows + totals so numeric columns line up at the print edge.
func renderAndSendRow(conn net.Conn, left, right string) error {
	img, err := renderTwoColGray(left, right)
	if err != nil {
		return err
	}
	return sendRasterLine(conn, img)
}

// renderAndSendDivider prints a thin horizontal line at row centre.
// Cheaper than rasterising a row of dashes; also looks crisper.
func renderAndSendDivider(conn net.Conn) error {
	img := newBlankRow()
	y := rasterLineHeight / 2
	for x := 4; x < rasterWidth-4; x++ {
		img.SetGray(x, y, color.Gray{Y: 0})
	}
	return sendRasterLine(conn, img)
}
