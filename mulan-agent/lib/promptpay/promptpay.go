// Package promptpay builds Thai PromptPay (Thai QR Payment) payloads.
//
// The payload is EMVCo merchant-presented QR: a flat list of tag-length-value
// triples, where length is a 2-digit decimal character count, terminated by a
// CRC-16/CCITT-FALSE over everything up to and including the literal "6304".
package promptpay

import (
	"errors"
	"fmt"
	"strings"
)

// EMVCo tags used here. Anything not listed is not emitted — PromptPay
// consumers only require this subset.
const (
	tagPayloadFormat = "00" // always "01"
	tagPOIMethod     = "01" // 11 = static (reusable), 12 = dynamic (one-time)
	tagMerchantInfo  = "29" // PromptPay account block
	tagCurrency      = "53" // ISO 4217 numeric
	tagAmount        = "54" // decimal string, e.g. "23.00"
	tagCountry       = "58"
	tagCRC           = "63"

	// Sub-tags inside tag 29.
	subAID      = "00" // PromptPay application id
	subMobile   = "01" // mobile number, 0066-prefixed
	subNationID = "02" // national id / tax id, 13 digits
	subEWallet  = "03" // e-wallet id, 15 digits

	aidPromptPay = "A000000677010111"
	currencyTHB  = "764"
	countryTH    = "TH"

	poiStatic  = "11"
	poiDynamic = "12"
)

// tlv renders one tag-length-value triple. Length is the character count of
// value, zero-padded to 2 digits — values of 100+ chars cannot be expressed and
// are rejected by the callers below (none get close).
func tlv(tag, value string) string {
	return fmt.Sprintf("%s%02d%s", tag, len(value), value)
}

// crc16CCITTFalse is the checksum EMVCo mandates for tag 63: poly 0x1021,
// init 0xFFFF, no reflection, no final xor.
func crc16CCITTFalse(s string) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range []byte(s) {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// normalizeTarget classifies the configured PromptPay id and returns the
// sub-tag plus the value in the form the spec wants.
//
// Mobile numbers are stored locally as 0XXXXXXXXX but must be transmitted as
// 13 digits: country code 0066 + the number without its leading 0.
func normalizeTarget(id string) (subTag, value string, err error) {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, id)

	switch {
	case len(digits) == 10 && strings.HasPrefix(digits, "0"):
		return subMobile, "0066" + digits[1:], nil
	case len(digits) == 13:
		// Ambiguous by length alone: a 13-digit value is a national id, while a
		// mobile already in 0066 form is also 13. The 0066 prefix disambiguates.
		if strings.HasPrefix(digits, "0066") {
			return subMobile, digits, nil
		}
		return subNationID, digits, nil
	case len(digits) == 15:
		return subEWallet, digits, nil
	}
	return "", "", fmt.Errorf("promptpay: unrecognised id %q (want 10-digit mobile, 13-digit national id, or 15-digit e-wallet)", id)
}

// ErrNoID is returned when no PromptPay id is configured.
var ErrNoID = errors.New("promptpay: no id configured")

// ValidateID reports whether id is a form this package can encode. An empty id
// is accepted (it means "QR printing disabled"), so callers that require one
// must check for empty themselves.
func ValidateID(id string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	_, _, err := normalizeTarget(id)
	return err
}

// Payload builds a payable PromptPay QR string for the given id and amount.
//
// amountSatang is the order total in satang (the money unit used everywhere
// else in this codebase); it is emitted as a THB decimal string. Pass 0 to omit
// the amount entirely, which produces a static QR the payer types into.
func Payload(id string, amountSatang int64) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", ErrNoID
	}
	if amountSatang < 0 {
		return "", fmt.Errorf("promptpay: negative amount %d", amountSatang)
	}
	poi := poiStatic
	if amountSatang > 0 {
		poi = poiDynamic
	}
	return payload(id, amountSatang, poi)
}

// payload is the real builder. poi is a parameter so tests can reproduce
// bank-issued QRs byte-for-byte (banks emit "11" even when an amount is
// present, which is spec-loose but universally accepted).
func payload(id string, amountSatang int64, poi string) (string, error) {
	subTag, target, err := normalizeTarget(id)
	if err != nil {
		return "", err
	}

	merchant := tlv(subAID, aidPromptPay) + tlv(subTag, target)

	var b strings.Builder
	b.WriteString(tlv(tagPayloadFormat, "01"))
	b.WriteString(tlv(tagPOIMethod, poi))
	b.WriteString(tlv(tagMerchantInfo, merchant))
	b.WriteString(tlv(tagCurrency, currencyTHB))
	if amountSatang > 0 {
		amount := fmt.Sprintf("%d.%02d", amountSatang/100, amountSatang%100)
		b.WriteString(tlv(tagAmount, amount))
	}
	b.WriteString(tlv(tagCountry, countryTH))

	// The CRC covers the tag and length of field 63 itself, so append "6304"
	// before checksumming.
	body := b.String() + tagCRC + "04"
	return fmt.Sprintf("%s%04X", body, crc16CCITTFalse(body)), nil
}
