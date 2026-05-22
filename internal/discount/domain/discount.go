package domain

// Discount types. A discount is either a flat THB amount off or a
// percentage off.
const (
	TypeFixed   = "fixed"
	TypePercent = "percent"
)

// ValidType reports whether t is a recognised discount type.
func ValidType(t string) bool {
	return t == TypeFixed || t == TypePercent
}
