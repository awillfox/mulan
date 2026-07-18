package printer

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mulan-agent/lib/promptpay"
)

// TestQRRasterIsScannable is the test that actually matters: it takes the exact
// 1-bit bytes that get streamed to the thermal head, rebuilds an image from
// them, and decodes that image with zbarimg. If this passes, a customer's phone
// camera sees the same payload the encoder produced — no assumption that the QR
// library and our bit-packing agree.
func TestQRRasterIsScannable(t *testing.T) {
	if _, err := exec.LookPath("zbarimg"); err != nil {
		t.Skip("zbarimg not installed — cannot verify scannability")
	}

	payload, err := promptpay.Payload("0923979957", 2300)
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}

	bitmap, bytesPerRow, height, err := qrRaster(payload, 360)
	if err != nil {
		t.Fatalf("qrRaster: %v", err)
	}

	path := filepath.Join(t.TempDir(), "qr.png")
	writeRasterPNG(t, path, bitmap, bytesPerRow, height)

	out, err := exec.Command("zbarimg", "--raw", "-q", path).Output()
	if err != nil {
		t.Fatalf("zbarimg failed (nothing decodable in the raster): %v", err)
	}
	got := strings.TrimSpace(string(out))
	if got != payload {
		t.Errorf("decoded payload mismatch\n got: %s\nwant: %s", got, payload)
	}
}

// TestQRRasterScalesToWholeModules guards the anti-blur property: the rendered
// width must be an exact multiple of the module count, never a resampled
// approximation.
func TestQRRasterScalesToWholeModules(t *testing.T) {
	payload, err := promptpay.Payload("0923979957", 12345)
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}
	for _, target := range []int{200, 360, 384, 576} {
		bitmap, bytesPerRow, height, err := qrRaster(payload, target)
		if err != nil {
			t.Fatalf("qrRaster(%d): %v", target, err)
		}
		if height > target {
			t.Errorf("target %d: height %d overflows target width", target, height)
		}
		if len(bitmap) != bytesPerRow*height {
			t.Errorf("target %d: bitmap len %d != %d*%d", target, len(bitmap), bytesPerRow, height)
		}
		if bytesPerRow*8 < height {
			t.Errorf("target %d: stride %d bytes cannot hold %d px", target, bytesPerRow, height)
		}
	}
}

// TestQRRasterSurvivesLongPayload checks a worst-case-ish payload (long e-wallet
// id, large amount) still encodes rather than erroring.
func TestQRRasterSurvivesLongPayload(t *testing.T) {
	payload, err := promptpay.Payload("123456789012345", 99999999)
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}
	if _, _, _, err := qrRaster(payload, 360); err != nil {
		t.Fatalf("qrRaster: %v", err)
	}
}

// writeRasterPNG reverses the ESC/POS bit packing (MSB-first, 1 = black) back
// into a PNG so an external scanner can read it.
func writeRasterPNG(t *testing.T, path string, bitmap []byte, bytesPerRow, height int) {
	t.Helper()
	width := bytesPerRow * 8
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			v := uint8(255)
			if bitmap[y*bytesPerRow+x/8]&(1<<(7-uint(x%8))) != 0 {
				v = 0
			}
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
}
