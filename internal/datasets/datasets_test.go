package datasets

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// The dictionaries are part of the v1 algorithm. If this test fails, a
// dictionary changed and every name-like fake value changed with it: revert
// the change, or ship it as a new algorithm version.
func TestDatasetsAreFrozen(t *testing.T) {
	for _, d := range []struct {
		name   string
		raw    string
		list   []string
		count  int
		sha256 string
	}{
		{"first_names", firstNames, FirstNames, 200, "182982aafb16e656e8c711747567f23e3bd8ad5238988e397999d218c51eab8a"},
		{"last_names", lastNames, LastNames, 200, "8795a2f48491b53d6ae1f75fc08f0dd1c2444a040669c3cef048b6647a5f6721"},
		{"company_names", companyNames, CompanyNames, 100, "6dcb2a4b3e9046be193de8f11ff40e57992ce058f3822df82b3aa6a324b0dbff"},
		{"company_suffixes", companySuffixes, CompanySuffixes, 25, "308f16ad101aa0ae5f9ccd8112e79ba6a99529a575211145c511f0d99d0719ff"},
	} {
		if len(d.list) != d.count {
			t.Errorf("%s: %d entries, want %d", d.name, len(d.list), d.count)
		}
		sum := sha256.Sum256([]byte(d.raw))
		if got := hex.EncodeToString(sum[:]); got != d.sha256 {
			t.Errorf("%s: sha256 %s, want %s", d.name, got, d.sha256)
		}
		seen := map[string]bool{}
		for _, v := range d.list {
			if seen[v] {
				t.Errorf("%s: duplicate %q", d.name, v)
			}
			seen[v] = true
			if v != strings.TrimSpace(v) || v == "" {
				t.Errorf("%s: bad entry %q", d.name, v)
			}
		}
	}
}
