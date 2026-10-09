package transport

import (
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

func TestParseSidecarAcceptsBothForms(t *testing.T) {
	full := strings.Repeat("a", 64)
	tests := map[string]struct {
		body string
		want string
		ok   bool
	}{
		"named":      {full + "  qtbase.7z", full, true},
		"bare":       {full, full, true},
		"trailing":   {full + "\n", full, true},
		"upper case": {strings.ToUpper(full) + "  x.7z", full, true},
		"too short":  {"abc", "", false},
		"not hex":    {strings.Repeat("z", 64), "", false},
		"empty":      {"", "", false},
	}
	for why, tt := range tests {
		got, err := parseSidecar(algSHA256, []byte(tt.body))
		if tt.ok {
			if err != nil || got != tt.want {
				t.Errorf("%s: parseSidecar = %q, %v; want %q", why, got, err, tt.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: parseSidecar = %q, want an error", why, got)
			continue
		}
		assertFailure(t, err, exitcode.Integrity, errs.CodeChecksumMalformed)
	}
}

func TestParseSidecarLengthFollowsTheAlgorithm(t *testing.T) {
	sha1 := strings.Repeat("b", 40)
	if got, err := parseSidecar(algSHA1, []byte(sha1)); err != nil || got != sha1 {
		t.Errorf("sha1 sidecar: %q, %v", got, err)
	}
	if _, err := parseSidecar(algSHA1, []byte(strings.Repeat("b", 64))); err == nil {
		t.Error("a 64-character digest was accepted for sha1")
	}
}

func TestEqualDigest(t *testing.T) {
	if !equalDigest("abc", "abc") {
		t.Error("equal digests compared unequal")
	}
	if equalDigest("abc", "abd") || equalDigest("abc", "abcd") {
		t.Error("unequal digests compared equal")
	}
}

func FuzzParseSidecar(f *testing.F) {
	f.Add([]byte(strings.Repeat("a", 64)))
	f.Add([]byte(strings.Repeat("a", 64) + "  x.7z"))
	f.Add([]byte(""))
	f.Add([]byte(strings.Repeat("a", 40)))
	f.Fuzz(func(t *testing.T, body []byte) {
		got, err := parseSidecar(algSHA256, body)
		if err == nil && len(got) != algSHA256.hexLen() {
			t.Fatalf("accepted %q as a sha256 digest", got)
		}
	})
}
