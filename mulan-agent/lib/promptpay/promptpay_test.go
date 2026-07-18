package promptpay

import "testing"

// realBankQR is a genuine PromptPay payload decoded from a bank-app screenshot:
// mobile 092-397-9957, amount 23.00 THB. It is the ground truth for the TLV
// layout and the CRC — if this test passes, the encoder agrees with what a Thai
// bank actually emits, not just with our reading of the spec.
//
// The bank sets tag 01 to "12" (dynamic/one-time), consistent with carrying a
// fixed amount, so this is also exactly what Payload produces for a priced QR.
const realBankQR = "00020101021229370016A000000677010111011300669239799575303764540523.005802TH6304C129"

func TestPayloadMatchesRealBankQR(t *testing.T) {
	got, err := payload("0923979957", 2300, poiDynamic)
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	if got != realBankQR {
		t.Errorf("payload mismatch\n got: %s\nwant: %s", got, realBankQR)
	}
}

func TestCRCOfRealBankQR(t *testing.T) {
	body := realBankQR[:len(realBankQR)-4]
	if got, want := crc16CCITTFalse(body), uint16(0xC129); got != want {
		t.Errorf("crc = %04X, want %04X", got, want)
	}
}

func TestPayloadDynamicWhenAmountPresent(t *testing.T) {
	got, err := Payload("0923979957", 2300)
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}
	// Same as the bank QR but POI 12 and therefore a different CRC.
	if got[:8] != "00020101" {
		t.Errorf("format indicator wrong: %q", got[:8])
	}
	if got[8:12] != "0212" {
		t.Errorf("POI = %q, want dynamic 0212", got[8:12])
	}
	if !contains(got, "540523.00") {
		t.Errorf("amount tag missing from %s", got)
	}
}

func TestPayloadStaticWhenNoAmount(t *testing.T) {
	got, err := Payload("0923979957", 0)
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}
	if got[8:12] != "0211" {
		t.Errorf("POI = %q, want static 0211", got[8:12])
	}
	if contains(got, "5405") {
		t.Errorf("amount tag should be absent from %s", got)
	}
}

func TestAmountFormatting(t *testing.T) {
	cases := []struct {
		satang int64
		want   string
	}{
		{2300, "540523.00"},
		{5, "54040.05"},
		{100, "54041.00"},
		{123456, "54071234.56"},
		{99, "54040.99"},
	}
	for _, c := range cases {
		got, err := Payload("0923979957", c.satang)
		if err != nil {
			t.Fatalf("Payload(%d): %v", c.satang, err)
		}
		if !contains(got, c.want) {
			t.Errorf("Payload(%d) = %s, missing %s", c.satang, got, c.want)
		}
	}
}

func TestNormalizeTarget(t *testing.T) {
	cases := []struct {
		id      string
		wantTag string
		wantVal string
	}{
		{"0923979957", subMobile, "0066923979957"},
		{"092-397-9957", subMobile, "0066923979957"},
		{"092 397 9957", subMobile, "0066923979957"},
		{"0066923979957", subMobile, "0066923979957"},
		{"1234567890123", subNationID, "1234567890123"},
		{"123456789012345", subEWallet, "123456789012345"},
	}
	for _, c := range cases {
		tag, val, err := normalizeTarget(c.id)
		if err != nil {
			t.Fatalf("normalizeTarget(%q): %v", c.id, err)
		}
		if tag != c.wantTag || val != c.wantVal {
			t.Errorf("normalizeTarget(%q) = (%s,%s), want (%s,%s)", c.id, tag, val, c.wantTag, c.wantVal)
		}
	}
}

func TestRejectsBadInput(t *testing.T) {
	if _, err := Payload("", 2300); err != ErrNoID {
		t.Errorf("empty id: err = %v, want ErrNoID", err)
	}
	for _, id := range []string{"123", "12345678901234567890", "abc"} {
		if _, err := Payload(id, 2300); err == nil {
			t.Errorf("Payload(%q) should have failed", id)
		}
	}
	if _, err := Payload("0923979957", -1); err == nil {
		t.Error("negative amount should have failed")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
