package printer

import (
	"fmt"
	"net"

	qrcode "github.com/skip2/go-qrcode"
)

// qrRaster encodes content as a QR code and returns it as a 1-bit ESC/POS
// raster bitmap: the packed bytes, the row stride, and the pixel height.
//
// It scales by whole modules rather than resampling an image. A thermal head
// prints hard black or nothing, so a fractional module boundary would land mid
// dot and blur the finder patterns — integer scaling keeps every module an
// exact block of dots, which is what makes the result reliably scannable.
func qrRaster(content string, targetWidth int) (bitmap []byte, bytesPerRow, height int, err error) {
	// Medium recovery (~15%) is the usual choice for payment QRs: enough to
	// survive thermal-paper smudging without inflating the symbol.
	q, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("encode qr: %w", err)
	}

	// Bitmap() includes the mandatory 4-module quiet zone. true = dark module.
	modules := q.Bitmap()
	n := len(modules)
	if n == 0 {
		return nil, 0, 0, fmt.Errorf("encode qr: empty bitmap")
	}

	scale := targetWidth / n
	if scale < 1 {
		scale = 1
	}
	width := n * scale
	height = width

	// Rows are byte-packed, so round the stride up to a whole byte. The extra
	// dots stay white and simply widen the quiet zone.
	bytesPerRow = (width + 7) / 8
	bitmap = make([]byte, bytesPerRow*height)

	for y := 0; y < height; y++ {
		row := modules[y/scale]
		for x := 0; x < width; x++ {
			if row[x/scale] {
				bitmap[y*bytesPerRow+x/8] |= 1 << (7 - uint(x%8))
			}
		}
	}
	return bitmap, bytesPerRow, height, nil
}

// printQR sends content as a QR code raster image (GS v 0), the same command
// path printLogo uses.
func printQR(conn net.Conn, content string, targetWidth int) error {
	bitmap, bytesPerRow, height, err := qrRaster(content, targetWidth)
	if err != nil {
		return err
	}
	conn.Write([]byte{
		0x1D, 0x76, 0x30, 0x00,
		byte(bytesPerRow & 0xFF), byte(bytesPerRow >> 8),
		byte(height & 0xFF), byte(height >> 8),
	})
	conn.Write(bitmap)
	return nil
}
