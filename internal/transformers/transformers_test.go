package transformers

import (
	"sort"
	"testing"
)

func TestRegistryMatchesNames(t *testing.T) {
	if len(Names) != len(Registry) {
		t.Fatalf("%d names, %d registry entries", len(Names), len(Registry))
	}
	for _, name := range Names {
		spec := Registry[name]
		if spec == nil || spec.Name != name {
			t.Errorf("%q is not registered correctly", name)
			continue
		}
		if builtin := name == "copy" || name == "null" || name == "json"; builtin != (spec.Fn == nil) {
			t.Errorf("%q: unexpected implementation", name)
		}
	}
}

func TestCountryCodeLen(t *testing.T) {
	for in, want := range map[string]int{
		"13105551234": 1, "74951234567": 1, "442079460958": 2, "4915112345678": 2, "33123456789": 2,
		"35312345678": 3, "380441234567": 3, "8613800138000": 2, "97141234567": 3, "0123456": 0, "2": 0, "20": 2,
	} {
		if got := countryCodeLen([]byte(in)); got != want {
			t.Errorf("countryCodeLen(%s) = %d, want %d", in, got, want)
		}
	}
}

func TestEmailDomainOf(t *testing.T) {
	for in, want := range map[string]string{
		"a@b.com": "b.com", "a@B-c.Example": "B-c.Example", "a@b@c.org": "c.org", "a@localhost": "localhost",
		"": "", "a": "", "@b.com": "", "a@": "", "a@.com": "", "a@b..com": "", "a@b.com.": "", "a@-b.com": "",
		"a@b-.com": "", "a@b_c.com": "", "a@b c.com": "", "a@exämple.com": "", "a@[1.2.3.4]": "", "a@b.com\n": "",
	} {
		if got := emailDomainOf(in); got != want {
			t.Errorf("emailDomainOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseTime(t *testing.T) {
	for _, in := range []string{
		"2024-01-15", "2024-01-15 10:30:00", "2024-01-15 10:30:00.123456", "2024-01-15 10:30:00+00",
		"2024-01-15 10:30:00.5-08", "2024-01-15 10:30:00+05:30", "2024-01-15T10:30:00Z", "2024-01-15T10:30:00.25+02:00",
		"2024-01-15T10:30:00",
	} {
		tm, layout, ok := parseTime(in)
		if !ok {
			t.Errorf("parseTime(%q) failed", in)
			continue
		}
		// Formatting with the detected layout reproduces the input exactly.
		if got := tm.Format(layout); got != in {
			t.Errorf("parseTime(%q) round-trips to %q", in, got)
		}
	}
	for _, in := range []string{"", "2024", "2024-13-01", "15/01/2024", "2024-01-15 10:30", "2024-01-15X10:30:00", "tomorrow"} {
		if _, _, ok := parseTime(in); ok {
			t.Errorf("parseTime(%q) should fail", in)
		}
	}
}

func TestAlphabets(t *testing.T) {
	for name, a := range map[string]string{"hex": hexDigits, "alnum": alnum, "base62": base62} {
		chars := []byte(a)
		sort.Slice(chars, func(i, j int) bool { return chars[i] < chars[j] })
		for i := 1; i < len(chars); i++ {
			if chars[i] == chars[i-1] {
				t.Errorf("%s alphabet repeats %q", name, chars[i])
			}
		}
	}
	if len(hexDigits) != 16 || len(alnum) != 36 || len(base62) != 62 {
		t.Error("alphabet sizes changed")
	}
}
