package serviceauth

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestProtectedACLPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, sddl                 string
		ancestor, private, allowed bool
	}{
		{"admin private", "O:BAG:BAD:P(A;;FA;;;SY)(A;;FA;;;BA)", false, true, true},
		{"users read", "O:BAG:BAD:P(A;;FA;;;BA)(A;;FRFX;;;BU)", false, false, true},
		{"key users read", "O:BAG:BAD:P(A;;FA;;;BA)(A;;FR;;;BU)", false, true, false},
		{"users write", "O:BAG:BAD:P(A;;FA;;;BA)(A;;FW;;;BU)", false, false, false},
		{"user owned", "O:BUG:BAD:P(A;;FA;;;BA)", false, false, false},
		{"null DACL", "O:BAG:BAD:NO_ACCESS_CONTROL", false, false, false},
		{"ancestor delete child", "O:BAG:BAD:P(A;;FA;;;BA)(A;;0x40;;;BU)", true, false, false},
		{"ancestor add sibling", "O:BAG:BAD:P(A;;FA;;;BA)(A;;0x4;;;BU)", true, false, true},
		{"directory add code", "O:BAG:BAD:P(A;;FA;;;BA)(A;;0x4;;;BU)", false, false, false},
		{"inherit only", "O:BAG:BAD:P(A;;FA;;;BA)(A;CIIO;FA;;;CO)", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString(tc.sddl)
			if err != nil {
				t.Fatal(err)
			}
			if err := checkDescriptor(sd, tc.ancestor, tc.private); (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v, error=%v", tc.allowed, err)
			}
		})
	}
}

func TestRejectsUntrustedKeyDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// Temp paths are writable by the interactive account, never suitable for a
	// SYSTEM service key, even if that account happens to be an administrator.
	if _, err := EnsureKey(path); err == nil {
		t.Fatal("created privileged key in user temp directory")
	}
}
