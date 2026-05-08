package service

import (
	"strings"
	"testing"
)

func TestGenerateCode(t *testing.T) {
	const (
		want      = 8
		runs      = 100
		uniqueMin = 95
	)
	seen := make(map[string]struct{}, runs)
	for i := 0; i < runs; i++ {
		c, err := generateCode()
		if err != nil {
			t.Fatalf("generateCode: %v", err)
		}
		if len(c) != want {
			t.Fatalf("len = %d want %d", len(c), want)
		}
		for _, ch := range c {
			if !strings.ContainsRune(codeChars, ch) {
				t.Fatalf("char %q not in alphabet", ch)
			}
		}
		seen[c] = struct{}{}
	}
	if len(seen) < uniqueMin {
		t.Fatalf("low entropy: %d unique of %d", len(seen), runs)
	}
}

func TestCodeCharsAvoidsAmbiguous(t *testing.T) {
	bad := "01OI"
	for _, ch := range bad {
		if strings.ContainsRune(codeChars, ch) {
			t.Errorf("ambiguous char %q in alphabet", ch)
		}
	}
}
