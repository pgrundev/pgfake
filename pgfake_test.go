package pgfake

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

const testKey = "test-key-1"

func newFaker(t testing.TB, key string, rules map[string]Rule) *Faker {
	t.Helper()
	f, err := New(Options{Key: []byte(key), Rules: rules})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// fake transforms a text value with no context, failing the test on error.
func fake(t testing.TB, f *Faker, rule Rule, value string) string {
	t.Helper()
	out, err := f.Transform(ValueContext{}, value, rule)
	if err != nil {
		t.Fatalf("%s: %v", rule.Transformer, err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("%s: got %T, want string", rule.Transformer, out)
	}
	return s
}

// samples has one realistic input per value transformer.
var samples = map[string]string{
	"hash":       "Alex Johnson",
	"string":     "AB-1234 xy",
	"email":      "alex@gmail.com",
	"username":   "alexj",
	"first_name": "Alex",
	"last_name":  "Johnson",
	"full_name":  "Alex Johnson",
	"phone":      "+1 310 555 1234",
	"company":    "Acme Inc.",
	"uuid":       "550e8400-e29b-41d4-a716-446655440000",
	"ip":         "203.0.113.7",
	"date":       "1987-04-12",
	"token":      "tok_Hs83hdKq02LmNo",
	"secret":     "sk_live_Hs83hdKq02",
}

var (
	emailRE    = regexp.MustCompile(`^[a-z0-9_]{1,64}@[A-Za-z0-9.-]+$`)
	uuidV4RE   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	hexRE      = regexp.MustCompile(`^[0-9a-f]+$`)
	base62RE   = regexp.MustCompile(`^[0-9A-Za-z]+$`)
	suffixRE   = regexp.MustCompile(`-[0-9a-f]{12}$`)
	usernameRE = regexp.MustCompile(`^[a-z]+_[0-9a-f]{16}$`)
	nameRE     = regexp.MustCompile(`^[A-Z][a-z]+$`)
	fullNameRE = regexp.MustCompile(`^[A-Z][a-z]+ [A-Z][a-z]+$`)
	companyRE  = regexp.MustCompile(`^[A-Z][a-z]+ [A-Z][A-Za-z.]+$`)
)

func TestTransformersList(t *testing.T) {
	want := "copy null hash string email username first_name last_name full_name phone company uuid ip date token secret json"
	if got := strings.Join(Transformers(), " "); got != want {
		t.Fatalf("Transformers() = %s", got)
	}
	f := newFaker(t, testKey, nil)
	for _, name := range Transformers() {
		if _, err := f.Transform(ValueContext{}, nil, Rule{Transformer: name}); err != nil {
			t.Errorf("%s is listed but not usable: %v", name, err)
		}
	}
}

func TestDeterministic(t *testing.T) {
	a := newFaker(t, testKey, nil)
	b := newFaker(t, testKey, nil) // "another server"
	for name, in := range samples {
		rule := Rule{Transformer: name, Scope: "s"}
		first := fake(t, a, rule, in)
		for i := 0; i < 3; i++ {
			if got := fake(t, a, rule, in); got != first {
				t.Errorf("%s: run %d gave %q, first run gave %q", name, i, got, first)
			}
		}
		if got := fake(t, b, rule, in); got != first {
			t.Errorf("%s: second Faker gave %q, want %q", name, got, first)
		}
		if first == in {
			t.Errorf("%s: output equals input", name)
		}
	}
}

func TestDifferentKeyDifferentOutput(t *testing.T) {
	a := newFaker(t, "test-key-1", nil)
	b := newFaker(t, "test-key-2", nil)
	for name, in := range samples {
		rule := Rule{Transformer: name, Scope: "s"}
		if x, y := fake(t, a, rule, in), fake(t, b, rule, in); x == y {
			t.Errorf("%s: both keys gave %q", name, x)
		}
	}
}

func TestDifferentInputDifferentOutput(t *testing.T) {
	f := newFaker(t, testKey, nil)
	for _, name := range []string{"hash", "email", "username", "uuid", "token", "secret"} {
		rule := Rule{Transformer: name}
		seen := map[string]int{}
		for i := 0; i < 20000; i++ {
			out := fake(t, f, rule, fmt.Sprintf("user%d@corp.example", i))
			if j, dup := seen[out]; dup {
				t.Fatalf("%s: inputs %d and %d collide", name, i, j)
			}
			seen[out] = i
		}
	}
}

func TestScope(t *testing.T) {
	f := newFaker(t, testKey, nil)
	const in = "alex@gmail.com"
	get := func(vc ValueContext, scope string) string {
		t.Helper()
		out, err := f.Transform(vc, in, Rule{Transformer: "email", Scope: scope})
		if err != nil {
			t.Fatal(err)
		}
		return out.(string)
	}
	users := ValueContext{Schema: "public", Table: "users", Column: "email"}
	orders := ValueContext{Schema: "public", Table: "orders", Column: "customer_email"}
	audit := ValueContext{Schema: "audit", Table: "audit_logs", Column: "user_email"}

	// Same explicit scope: one identity everywhere, regardless of location.
	want := get(users, "user_email")
	for _, vc := range []ValueContext{orders, audit, {}} {
		if got := get(vc, "user_email"); got != want {
			t.Errorf("scope user_email in %+v = %q, want %q", vc, got, want)
		}
	}
	// Different scopes: different identities.
	if get(users, "other") == want {
		t.Error("different scopes gave the same output")
	}
	// No scope: each column is its own scope.
	if get(users, "") == get(orders, "") {
		t.Error("unrelated columns share an identity by default")
	}
	if get(users, "") == want {
		t.Error("default scope equals an unrelated explicit scope")
	}
	// The default scope is the qualified column name, with "public" implied,
	// so another column can join it explicitly.
	if got := get(ValueContext{Table: "users", Column: "email"}, ""); got != get(users, "") {
		t.Error("empty schema should mean public")
	}
	if got := get(orders, "public.users.email"); got != get(users, "") {
		t.Error("explicit scope should be able to name a column's default scope")
	}
	// Scopes are per transformer.
	u, _ := f.Transform(users, in, Rule{Transformer: "username", Scope: "user_email"})
	h, _ := f.Transform(users, in, Rule{Transformer: "token", Scope: "user_email"})
	h2, _ := f.Transform(users, in, Rule{Transformer: "hash", Scope: "user_email", Length: 32})
	if u == want || h == h2 {
		t.Error("transformers must not share streams")
	}
}

// The namespace format is the compatibility contract, so it is checked
// against plain crypto/hmac rather than against pgfake's own code.
func TestNamespaceFormat(t *testing.T) {
	f := newFaker(t, testKey, nil)
	sum := func(msg string) string {
		h := hmac.New(sha256.New, []byte(testKey))
		h.Write([]byte(msg))
		return hex.EncodeToString(h.Sum(nil))
	}
	if got, want := fake(t, f, Rule{Transformer: "hash", Scope: "email"}, "alex@gmail.com"),
		sum("pgfake:v1:hash:email\x00alex@gmail.com"); got != want {
		t.Errorf("explicit scope: got %s, want %s", got, want)
	}
	out, err := f.Transform(ValueContext{Table: "users", Column: "email"}, "alex@gmail.com", Rule{Transformer: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	if want := sum("pgfake:v1:hash:public.users.email\x00alex@gmail.com"); out != want {
		t.Errorf("default scope: got %s, want %s", out, want)
	}
	if got, want := fake(t, f, Rule{Transformer: "email", Scope: "email"}, "alex@gmail.com"),
		"user_"+sum("pgfake:v1:email:email\x00alex@gmail.com")[:20]+"@example.test"; got != want {
		t.Errorf("email: got %s, want %s", got, want)
	}
}

func TestNullPreserved(t *testing.T) {
	f := newFaker(t, testKey, nil)
	for _, name := range Transformers() {
		rule := Rule{Transformer: name}
		for _, pgtype := range []string{"", "text", "integer", "uuid", "jsonb", "timestamptz", "inet", "text[]"} {
			out, err := f.Transform(ValueContext{PGType: pgtype}, nil, rule)
			if err != nil || out != nil {
				t.Errorf("%s/%s: nil -> %v, %v", name, pgtype, out, err)
			}
		}
		// Typed nils are NULLs too.
		for _, v := range []any{[]byte(nil), []string(nil), []any(nil)} {
			if name == "json" {
				continue
			}
			out, err := f.Transform(ValueContext{}, v, rule)
			if err != nil {
				t.Errorf("%s: %T(nil): %v", name, v, err)
			}
			switch o := out.(type) {
			case nil:
			case []byte:
				if o != nil {
					t.Errorf("%s: nil []byte became %q", name, o)
				}
			case []string:
				if o != nil {
					t.Errorf("%s: nil []string became %q", name, o)
				}
			case []any:
				if o != nil {
					t.Errorf("%s: nil []any became %v", name, o)
				}
			default:
				t.Errorf("%s: %T(nil) became %T", name, v, out)
			}
		}
	}
}

func TestCopyAndNull(t *testing.T) {
	f := newFaker(t, testKey, nil)
	for _, v := range []any{"x", 42, int64(7), true, 3.14, time.Unix(0, 0), []byte("b")} {
		out, err := f.Transform(ValueContext{}, v, Rule{Transformer: "copy"})
		if err != nil || fmt.Sprint(out) != fmt.Sprint(v) {
			t.Errorf("copy(%T) = %v, %v", v, out, err)
		}
		out, err = f.Transform(ValueContext{}, v, Rule{Transformer: "null"})
		if err != nil || out != nil {
			t.Errorf("null(%T) = %v, %v", v, out, err)
		}
	}
}

func TestEmptyPreserved(t *testing.T) {
	f := newFaker(t, testKey, nil)
	for name := range samples {
		rule := Rule{Transformer: name}
		if got := fake(t, f, rule, ""); got != "" {
			t.Errorf("%s: empty string became %q", name, got)
		}
		out, err := f.Transform(ValueContext{}, []byte{}, rule)
		if b, ok := out.([]byte); err != nil || !ok || b == nil || len(b) != 0 {
			t.Errorf("%s: empty []byte became %#v, %v", name, out, err)
		}
	}
	// Whitespace is data, not emptiness.
	if got := fake(t, f, Rule{Transformer: "full_name"}, " "); got == " " {
		t.Error("whitespace-only value was not transformed")
	}
}

func TestUTF8(t *testing.T) {
	f := newFaker(t, testKey, nil)
	inputs := []string{
		"Zoë Ångström", "张伟", "Мария Иванова", "محمد", "🙂 emoji 🙂", "é", // combining accent
		"üser@exämple.com", "\xff\xfe invalid \x80", "a\x00b",
	}
	for name := range samples {
		for _, in := range inputs {
			out := fake(t, f, Rule{Transformer: name, PreserveLength: name != "string" && name != "phone"}, in)
			if !utf8.ValidString(out) {
				t.Errorf("%s(%q) is not valid UTF-8: %q", name, in, out)
			}
			if out == in {
				t.Errorf("%s(%q) returned the input", name, in)
			}
		}
	}
	// string preserves the character count and the punctuation.
	for _, in := range inputs {
		out := fake(t, f, Rule{Transformer: "string"}, in)
		if utf8.RuneCountInString(out) != utf8.RuneCountInString(in) {
			t.Errorf("string(%q) = %q changes the length", in, out)
		}
	}
	// Distinct Unicode inputs stay distinct.
	if fake(t, f, Rule{Transformer: "email"}, "张伟") == fake(t, f, Rule{Transformer: "email"}, "张偉") {
		t.Error("distinct UTF-8 inputs collide")
	}
	// Length is counted in characters, not bytes.
	if got := fake(t, f, Rule{Transformer: "token", PreserveLength: true}, "张伟张伟"); len(got) != 4 {
		t.Errorf("preserve_length counted bytes: %q", got)
	}
}

func TestVeryLongStrings(t *testing.T) {
	f := newFaker(t, testKey, nil)
	long := strings.Repeat("sensitive-data-ü ", 60000) // ~1 MB
	other := long[:len(long)-1] + "!"
	for name := range samples {
		rule := Rule{Transformer: name}
		out := fake(t, f, rule, long)
		if out != fake(t, f, rule, long) {
			t.Errorf("%s: not deterministic on long input", name)
		}
		if name != "phone" && name != "ip" && name != "date" && name != "string" && out == fake(t, f, rule, other) {
			// phone/ip/date fall back to small domains on malformed input.
			t.Errorf("%s: long inputs differing in the last byte collide", name)
		}
		if strings.Contains(out, "sensitive") {
			t.Errorf("%s: output leaks the input", name)
		}
	}
	for _, name := range []string{"token", "secret", "hash"} {
		out := fake(t, f, Rule{Transformer: name, PreserveLength: true}, long)
		if utf8.RuneCountInString(out) != utf8.RuneCountInString(long) {
			t.Errorf("%s: preserve_length gave %d chars", name, len(out))
		}
	}
	// A long value must not poison the pooled buffers for later calls.
	want := fake(t, newFaker(t, testKey, nil), Rule{Transformer: "email"}, "alex@gmail.com")
	if got := fake(t, f, Rule{Transformer: "email"}, "alex@gmail.com"); got != want {
		t.Errorf("after long inputs: %q, want %q", got, want)
	}
}

func TestEmail(t *testing.T) {
	f := newFaker(t, testKey, nil)
	check := func(out string) {
		t.Helper()
		if !emailRE.MatchString(out) {
			t.Errorf("%q does not look like an email", out)
		}
		if _, err := mail.ParseAddress(out); err != nil {
			t.Errorf("%q: %v", out, err)
		}
	}

	out := fake(t, f, Rule{Transformer: "email"}, "alex@gmail.com")
	check(out)
	if !regexp.MustCompile(`^user_[0-9a-f]{20}@example\.test$`).MatchString(out) {
		t.Errorf("default form: %q", out)
	}

	out = fake(t, f, Rule{Transformer: "email", PreserveDomain: true}, "alex@gmail.com")
	check(out)
	if !strings.HasSuffix(out, "@gmail.com") || strings.Contains(out, "alex") {
		t.Errorf("preserve_domain: %q", out)
	}

	for _, in := range []string{"alex@gmail.com", "a.very.long.local.part+tag@sub.domain.example.org", "ab@c.de", "x"} {
		out = fake(t, f, Rule{Transformer: "email", PreserveLength: true}, in)
		check(out)
		if want := max(len(in), len("a@example.test")); len(out) != want {
			t.Errorf("preserve_length(%q) = %q (%d chars, want %d)", in, out, len(out), want)
		}
		out = fake(t, f, Rule{Transformer: "email", PreserveLength: true, PreserveDomain: true}, in)
		check(out)
		if strings.Contains(in, "@") && len(out) != len(in) {
			t.Errorf("preserve_length+domain(%q) = %q", in, out)
		}
	}
	// The local part never exceeds 64 characters, even to preserve length.
	out = fake(t, f, Rule{Transformer: "email", PreserveLength: true}, strings.Repeat("a", 500)+"@gmail.com")
	check(out)
}

func TestMalformedEmails(t *testing.T) {
	f := newFaker(t, testKey, nil)
	seen := map[string]string{}
	for _, in := range []string{
		"not-an-email", "@", "@@", "a@", "@b.com", "a@b@c.com", "a b@c.com", "a@b..com", "a@-b.com",
		"a@b.com.", "<script>@x.y", "a@[127.0.0.1]", "a@b_c.com", "üser@exämple.com", " ", "\x00", "a@" + strings.Repeat("d", 300) + ".com",
	} {
		for _, rule := range []Rule{
			{Transformer: "email"},
			{Transformer: "email", PreserveDomain: true},
			{Transformer: "email", PreserveLength: true},
			{Transformer: "email", PreserveDomain: true, PreserveLength: true},
		} {
			out := fake(t, f, rule, in)
			if _, err := mail.ParseAddress(out); err != nil || !emailRE.MatchString(out) {
				t.Errorf("email(%q, %+v) = %q is not a valid address", in, rule, out)
			}
		}
		out := fake(t, f, Rule{Transformer: "email"}, in)
		if prev, dup := seen[out]; dup {
			t.Errorf("malformed inputs %q and %q collide", in, prev)
		}
		seen[out] = in
	}
	// Only clean host names are preserved.
	if out := fake(t, f, Rule{Transformer: "email", PreserveDomain: true}, "a@b_c.com"); !strings.HasSuffix(out, "@example.test") {
		t.Errorf("unsafe domain was preserved: %q", out)
	}
	if out := fake(t, f, Rule{Transformer: "email", PreserveDomain: true}, "x@y@Corp.Example"); !strings.HasSuffix(out, "@Corp.Example") {
		t.Errorf("domain not preserved: %q", out)
	}
}

func skeleton(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return 'd'
		}
		return r
	}, s)
}

func TestPhone(t *testing.T) {
	f := newFaker(t, testKey, nil)
	rule := Rule{Transformer: "phone"}
	for _, c := range []struct{ in, prefix string }{
		{"+1 310 555 1234", "+1 555 "},
		{"+13105551234", "+1555"},
		{"1-310-555-1234", "1-555-"},
		{"(310) 555-1234", "(555) "},
		{"310.555.1234 x42", "555."},
		{"+1 (310) 555-1234 ext. 9001", "+1 (555) "},
		{"020 7946 0958 #12", "0"},
		{"+44 20 7946 0958", "+44 "},
		{"+4915112345678", "+49"},
		{"+7 495 123-45-67", "+7 "},
		{"+353 1 234 5678", "+353 "},
		{"030 1234567", "0"},
		{"0044 20 7946 0958", "00"},
		{"555-1234", ""},
		{"12345", ""},
	} {
		out := fake(t, f, rule, c.in)
		if skeleton(out) != skeleton(c.in) {
			t.Errorf("phone(%q) = %q changes the format", c.in, out)
		}
		if !strings.HasPrefix(out, c.prefix) {
			t.Errorf("phone(%q) = %q, want prefix %q", c.in, out, c.prefix)
		}
		if out == c.in {
			t.Errorf("phone(%q) unchanged", c.in)
		}
	}
	// Bytes in, bytes out, same result.
	out, err := f.Transform(ValueContext{}, []byte("+1 310 555 1234"), rule)
	if err != nil || string(out.([]byte)) != fake(t, f, rule, "+1 310 555 1234") {
		t.Errorf("[]byte phone = %q, %v", out, err)
	}
}

func TestMalformedPhones(t *testing.T) {
	f := newFaker(t, testKey, nil)
	fallback := regexp.MustCompile(`^\+1555\d{7}$`)
	for _, in := range []string{
		"call me", "n/a", "1-800-FLOWERS", "+", "++", "1", "12", "+1", "x", "٠١٢٣٤٥٦٧٨٩", "tel:+13105551234",
		"310 555 1234 (ask for Bob)", "\x00\x01", "+1 310 555 1234\n; DROP TABLE users",
	} {
		for _, unique := range []bool{false, true} {
			out := fake(t, f, Rule{Transformer: "phone", Unique: unique}, in)
			if !fallback.MatchString(out) {
				t.Errorf("phone(%q, unique=%v) = %q, want the +1555 fallback", in, unique, out)
			}
		}
	}
	// Absurdly long digit strings still work and keep their shape.
	in := strings.Repeat("1234567890", 20)
	for _, unique := range []bool{false, true} {
		out := fake(t, f, Rule{Transformer: "phone", Unique: unique}, in)
		if skeleton(out) != skeleton(in) || out == in {
			t.Errorf("long phone (unique=%v) = %q", unique, out)
		}
	}
}

func TestUUID(t *testing.T) {
	f := newFaker(t, testKey, nil)
	rule := Rule{Transformer: "uuid", Scope: "ids"}
	const in = "550e8400-e29b-41d4-a716-446655440000"
	out := fake(t, f, rule, in)
	if !uuidV4RE.MatchString(out) {
		t.Fatalf("%q is not a v4 UUID", out)
	}
	if out == in || out != fake(t, f, rule, in) {
		t.Fatalf("uuid not deterministic or unchanged: %q", out)
	}
	// Malformed input still produces a valid UUID.
	for _, bad := range []string{"not-a-uuid", "550e8400", "{550E8400-E29B-41D4-A716-446655440000}", "🙂"} {
		if got := fake(t, f, rule, bad); !uuidV4RE.MatchString(got) {
			t.Errorf("uuid(%q) = %q", bad, got)
		}
	}
	// The binary form and the text form of a uuid column agree.
	var raw [16]byte
	hex.Decode(raw[:], []byte(strings.ReplaceAll(in, "-", "")))
	bin, err := f.Transform(ValueContext{PGType: "uuid"}, raw, rule)
	if err != nil {
		t.Fatal(err)
	}
	b := bin.([16]byte)
	if b[6]>>4 != 4 || b[8]>>6 != 2 {
		t.Errorf("version/variant bits not set: %x", b)
	}
	txt, err := f.Transform(ValueContext{PGType: "uuid"}, in, rule)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(b[:]); got != strings.ReplaceAll(txt.(string), "-", "") {
		t.Errorf("binary %s != text %s", got, txt)
	}
	// hash on a uuid column yields a uuid as well.
	h, err := f.Transform(ValueContext{PGType: "uuid"}, in, Rule{Transformer: "hash"})
	if err != nil || !uuidV4RE.MatchString(h.(string)) {
		t.Errorf("hash on uuid = %v, %v", h, err)
	}
}

func TestOutputValidity(t *testing.T) {
	f := newFaker(t, testKey, nil)
	v4 := netip.MustParsePrefix("10.0.0.0/8")
	v6 := netip.MustParsePrefix("2001:db8::/32")
	for i := 0; i < 2000; i++ {
		in := fmt.Sprintf("input-%d", i)
		must := func(name string, re *regexp.Regexp) {
			t.Helper()
			if out := fake(t, f, Rule{Transformer: name}, in); !re.MatchString(out) {
				t.Fatalf("%s(%q) = %q", name, in, out)
			}
		}
		must("email", emailRE)
		must("uuid", uuidV4RE)
		must("username", usernameRE)
		must("first_name", nameRE)
		must("last_name", nameRE)
		must("full_name", fullNameRE)
		must("company", companyRE)
		must("hash", regexp.MustCompile(`^[0-9a-f]{64}$`))
		must("token", regexp.MustCompile(`^[0-9a-f]{32}$`))
		must("secret", regexp.MustCompile(`^[0-9A-Za-z]{40}$`))
		must("phone", regexp.MustCompile(`^\+1555\d{7}$`))

		ip4 := fmt.Sprintf("%d.%d.%d.%d", 1+i%200, i%256, (i*7)%256, (i*13)%256)
		if a, err := netip.ParseAddr(fake(t, f, Rule{Transformer: "ip"}, ip4)); err != nil || !v4.Contains(a) {
			t.Fatalf("ip(%s) = %v, %v", ip4, a, err)
		}
		ip6 := fmt.Sprintf("2a00:1450:%x::%x", i, i+1)
		if a, err := netip.ParseAddr(fake(t, f, Rule{Transformer: "ip"}, ip6)); err != nil || !v6.Contains(a) {
			t.Fatalf("ip(%s) = %v, %v", ip6, a, err)
		}
		if a, err := netip.ParseAddr(fake(t, f, Rule{Transformer: "ip"}, in)); err != nil || !v4.Contains(a) {
			t.Fatalf("ip(malformed) = %v, %v", a, err)
		}

		day := time.Date(1950, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i*13)
		out := fake(t, f, Rule{Transformer: "date"}, day.Format("2006-01-02"))
		got, err := time.Parse("2006-01-02", out)
		if err != nil {
			t.Fatalf("date = %q: %v", out, err)
		}
		if d := got.Sub(day).Hours() / 24; d < -365 || d > 365 {
			t.Fatalf("date moved %v days", d)
		}
		if _, err := time.Parse("2006-01-02", fake(t, f, Rule{Transformer: "date"}, in)); err != nil {
			t.Fatalf("date(malformed): %v", err)
		}
	}
}

func TestIP(t *testing.T) {
	f := newFaker(t, testKey, nil)
	rule := Rule{Transformer: "ip"}
	for _, c := range []struct{ pgtype, in, re string }{
		{"inet", "203.0.113.7", `^10\.\d+\.\d+\.\d+$`},
		{"inet", "203.0.113.7/24", `^10\.\d+\.\d+\.\d+/24$`},
		{"cidr", "203.0.113.0/24", `^10\.\d+\.\d+\.0/24$`},
		{"cidr", "203.0.113.7/32", `^10\.\d+\.\d+\.\d+/32$`},
		{"inet", "2a00:1450:4001::200e", `^2001:db8:[0-9a-f:]+$`},
		{"inet", "2a00:1450:4001::200e/64", `^2001:db8:[0-9a-f:]+/64$`},
		{"cidr", "2a00:1450:4001::/48", `^2001:db8:[0-9a-f]+::/48$`},
		{"text", "::ffff:203.0.113.7", `^10\.\d+\.\d+\.\d+$`},
		{"text", "203.0.113.7/99", `^10\.\d+\.\d+\.\d+$`},
		{"text", "999.1.1.1", `^10\.\d+\.\d+\.\d+$`},
	} {
		out, err := f.Transform(ValueContext{PGType: c.pgtype}, c.in, rule)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if !regexp.MustCompile(c.re).MatchString(out.(string)) {
			t.Errorf("ip(%s %q) = %q, want %s", c.pgtype, c.in, out, c.re)
		}
		if strings.Contains(out.(string), "/") {
			p, err := netip.ParsePrefix(out.(string))
			if err != nil {
				t.Errorf("%q: %v", out, err)
			} else if c.pgtype == "cidr" && p != p.Masked() {
				t.Errorf("cidr %q has host bits set", out)
			}
		}
	}
}

func TestDate(t *testing.T) {
	f := newFaker(t, testKey, nil)
	for _, c := range []struct{ pgtype, in, layout string }{
		{"date", "1987-04-12", "2006-01-02"},
		{"timestamp", "2024-01-15 10:30:00", "2006-01-02 15:04:05"},
		{"timestamp", "2024-01-15 10:30:00.123456", "2006-01-02 15:04:05.999999"},
		{"timestamptz", "2024-01-15 10:30:00+00", "2006-01-02 15:04:05-07"},
		{"timestamptz", "2024-01-15 10:30:00.5-08", "2006-01-02 15:04:05.9-07"},
		{"timestamptz", "2024-01-15 10:30:00.123+05:30", "2006-01-02 15:04:05.999-07:00"},
		{"text", "2024-01-15T10:30:00Z", time.RFC3339},
		{"text", "2024-01-15T10:30:00.25+02:00", time.RFC3339Nano},
		{"text", "2024-01-15T10:30:00", "2006-01-02T15:04:05"},
	} {
		for _, days := range []int{0, 1, 30} {
			out, err := f.Transform(ValueContext{PGType: c.pgtype}, c.in, Rule{Transformer: "date", Days: days})
			if err != nil {
				t.Fatalf("%s: %v", c.in, err)
			}
			src, _ := time.Parse(c.layout, c.in)
			got, err := time.Parse(c.layout, out.(string))
			if err != nil {
				t.Errorf("date(%q) = %q does not keep the layout: %v", c.in, out, err)
				continue
			}
			if len(out.(string)) != len(c.in) {
				t.Errorf("date(%q) = %q changes the length", c.in, out)
			}
			limit := days
			if limit == 0 {
				limit = 365
			}
			if d := got.Sub(src); d < -time.Duration(limit)*24*time.Hour || d > time.Duration(limit)*24*time.Hour {
				t.Errorf("date(%q, days=%d) = %q is outside the window", c.in, days, out)
			}
			_, so := src.Zone()
			_, go_ := got.Zone()
			if so != go_ || got.Nanosecond() != src.Nanosecond() {
				t.Errorf("date(%q) = %q changes zone or fraction", c.in, out)
			}
		}
	}
	// Different dates shift by different amounts.
	offsets := map[int]bool{}
	for i := 0; i < 50; i++ {
		day := time.Date(2000, 1, 1+i, 0, 0, 0, 0, time.UTC)
		got, _ := time.Parse("2006-01-02", fake(t, f, Rule{Transformer: "date"}, day.Format("2006-01-02")))
		offsets[int(got.Sub(day).Hours()/24)] = true
	}
	if len(offsets) < 40 {
		t.Errorf("only %d distinct offsets for 50 dates", len(offsets))
	}
	// Special and malformed values.
	for _, inf := range []string{"infinity", "-infinity"} {
		if out, _ := f.Transform(ValueContext{PGType: "timestamptz"}, inf, Rule{Transformer: "date"}); out != inf {
			t.Errorf("%s -> %v", inf, out)
		}
	}
	for pgtype, layout := range map[string]string{
		"date": "2006-01-02", "timestamp": "2006-01-02 15:04:05", "timestamptz": "2006-01-02 15:04:05-07", "text": "2006-01-02",
	} {
		for _, bad := range []string{"not a date", "2024-13-45", "0044-03-15 BC", "12/31/1999", "2024-01-15 25:00:00", "🙂"} {
			out, err := f.Transform(ValueContext{PGType: pgtype}, bad, Rule{Transformer: "date"})
			if err != nil {
				t.Fatalf("date(%q): %v", bad, err)
			}
			if _, err := time.Parse(layout, out.(string)); err != nil {
				t.Errorf("date(%s %q) = %q is not a valid %s", pgtype, bad, out, pgtype)
			}
		}
	}
	// Dates at the edge of the supported range stay inside it.
	for _, edge := range []string{"0001-01-01", "9999-12-31"} {
		out := fake(t, f, Rule{Transformer: "date", Days: 36500}, edge)
		if got, err := time.Parse("2006-01-02", out); err != nil || got.Year() < 1 || got.Year() > 9999 {
			t.Errorf("date(%s) = %q", edge, out)
		}
	}
}

func TestStringsTokensSecrets(t *testing.T) {
	f := newFaker(t, testKey, nil)

	out := fake(t, f, Rule{Transformer: "string"}, "AB-1234 xy_Z")
	if !regexp.MustCompile(`^[A-Z]{2}-\d{4} [a-z]{2}_[A-Z]$`).MatchString(out) {
		t.Errorf("string keeps the format: %q", out)
	}
	if out := fake(t, f, Rule{Transformer: "string", Length: 12}, "anything"); !regexp.MustCompile(`^[a-z0-9]{12}$`).MatchString(out) {
		t.Errorf("string length: %q", out)
	}

	const secret = "sk_live_Hs83hdKq02LmNoPq"
	for name, re := range map[string]*regexp.Regexp{"token": hexRE, "secret": base62RE, "hash": hexRE} {
		def := fake(t, f, Rule{Transformer: name}, secret)
		same := fake(t, f, Rule{Transformer: name, PreserveLength: true}, secret)
		fixed := fake(t, f, Rule{Transformer: name, Length: 7}, secret)
		if !re.MatchString(def) || !re.MatchString(same) || !re.MatchString(fixed) {
			t.Errorf("%s: bad alphabet: %q %q %q", name, def, same, fixed)
		}
		if len(same) != len(secret) || len(fixed) != 7 {
			t.Errorf("%s: lengths %d and %d", name, len(same), len(fixed))
		}
		for _, o := range []string{def, same} {
			// Nothing of the source survives, not even the "sk_live_" prefix.
			if strings.Contains(o, "sk_") || strings.Contains(o, "Hs83") || strings.Contains(o, "_") {
				t.Errorf("%s: output %q exposes the source", name, o)
			}
		}
	}
}

func TestUniqueMode(t *testing.T) {
	f := newFaker(t, testKey, nil)
	const n = 30000

	for _, name := range []string{"first_name", "last_name", "full_name", "company"} {
		plain := map[string]bool{}
		unique := map[string]bool{}
		for i := 0; i < n; i++ {
			in := fmt.Sprintf("person %d", i)
			p := fake(t, f, Rule{Transformer: name}, in)
			u := fake(t, f, Rule{Transformer: name, Unique: true}, in)
			if !suffixRE.MatchString(u) || !strings.HasPrefix(u, p+"-") {
				t.Fatalf("%s unique = %q, plain = %q", name, u, p)
			}
			plain[p] = true
			unique[u] = true
		}
		if len(plain) == n {
			t.Errorf("%s: expected dictionary collisions without unique", name)
		}
		if len(unique) != n {
			t.Errorf("%s: %d collisions with unique", name, n-len(unique))
		}
	}

	// Phone: the keyed permutation never collides for same-format numbers.
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		in := fmt.Sprintf("+1 310 %03d %04d", i/10000, i%10000)
		out := fake(t, f, Rule{Transformer: "phone", Unique: true}, in)
		if skeleton(out) != skeleton(in) || !strings.HasPrefix(out, "+1 ") {
			t.Fatalf("unique phone(%q) = %q", in, out)
		}
		if seen[out] {
			t.Fatalf("unique phone collision at %q", in)
		}
		seen[out] = true
	}
	// Exhaustive over a small format: a true bijection.
	seen = map[string]bool{}
	for i := 0; i < 100000; i++ {
		seen[fake(t, f, Rule{Transformer: "phone", Unique: true}, fmt.Sprintf("%05d", i))] = true
	}
	if len(seen) != 100000 {
		t.Errorf("5-digit unique phones: %d distinct outputs of 100000", len(seen))
	}

	// Accepted as a no-op where the domain is already huge.
	for _, name := range []string{"email", "username", "uuid", "hash", "token", "secret", "copy", "null"} {
		if _, err := f.Transform(ValueContext{}, "x", Rule{Transformer: name, Unique: true}); err != nil {
			t.Errorf("%s: unique rejected: %v", name, err)
		}
	}
	// Rejected where it cannot be honored.
	for _, name := range []string{"string", "ip", "date"} {
		if _, err := f.Transform(ValueContext{}, "x", Rule{Transformer: name, Unique: true}); !errors.Is(err, ErrInvalidRule) {
			t.Errorf("%s: unique should be rejected, got %v", name, err)
		}
	}
}

func TestConcurrency(t *testing.T) {
	rules := map[string]Rule{
		"users.email":   {Transformer: "email", Scope: "user_email"},
		"users.name":    {Transformer: "full_name", Unique: true},
		"users.phone":   {Transformer: "phone"},
		"users.ref":     {Transformer: "hash"},
		"users.profile": {Transformer: "json", Paths: map[string]Rule{"contact.email": {Transformer: "email", Scope: "user_email"}}},
	}
	table := Table{Name: "users", Columns: []Column{
		{"id", "bigint"}, {"email", "text"}, {"name", "text"}, {"phone", "text"}, {"ref", "bigint"}, {"profile", "jsonb"},
	}}
	row := func(i int) []any {
		email := fmt.Sprintf("user%d@corp.example", i)
		return []any{int64(i), email, fmt.Sprintf("Person %d", i), fmt.Sprintf("+1 310 555 %04d", i), int64(i),
			fmt.Sprintf(`{"contact":{"email":%q}}`, email)}
	}

	const rows = 300
	serial := newFaker(t, testKey, rules)
	want := make([][]any, rows)
	for i := range want {
		var err error
		if want[i], err = serial.TransformRow(table, row(i)); err != nil {
			t.Fatal(err)
		}
	}
	long := strings.Repeat("x", 100000)
	wantLong := fake(t, serial, Rule{Transformer: "secret", PreserveLength: true}, long)

	f := newFaker(t, testKey, rules)
	var wg sync.WaitGroup
	errs := make(chan string, 64)
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for n := 0; n < rows; n++ {
				i := (n*7 + g*31) % rows // every goroutine in its own order
				got, err := f.TransformRow(table, row(i))
				if err != nil {
					errs <- err.Error()
					return
				}
				if fmt.Sprint(got) != fmt.Sprint(want[i]) {
					errs <- fmt.Sprintf("row %d: got %v, want %v", i, got, want[i])
					return
				}
				if n%50 == g {
					out, err := f.Transform(ValueContext{}, long, Rule{Transformer: "secret", PreserveLength: true})
					if err != nil || out != wantLong {
						errs <- "long value differs under concurrency"
						return
					}
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

func TestInputsAreNotMutated(t *testing.T) {
	f := newFaker(t, testKey, nil)
	for name, in := range samples {
		buf := []byte(in)
		keep := append([]byte(nil), buf...)
		out, err := f.Transform(ValueContext{}, buf, Rule{Transformer: name, Unique: name == "phone"})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(buf, keep) {
			t.Errorf("%s: input buffer was modified", name)
		}
		// The output must not alias the input either.
		o := out.([]byte)
		for i := range o {
			o[i] = 'X'
		}
		if !bytes.Equal(buf, keep) {
			t.Errorf("%s: output aliases the input buffer", name)
		}
	}

	strs := []string{"a@b.c", "d@e.f"}
	if _, err := f.Transform(ValueContext{PGType: "text[]"}, strs, Rule{Transformer: "email"}); err != nil {
		t.Fatal(err)
	}
	anys := []any{"a@b.c", nil}
	if _, err := f.Transform(ValueContext{PGType: "text[]"}, anys, Rule{Transformer: "email"}); err != nil {
		t.Fatal(err)
	}
	if strs[0] != "a@b.c" || anys[0] != "a@b.c" {
		t.Error("slice input was modified")
	}

	doc := map[string]any{"email": "a@b.c", "nested": map[string]any{"list": []any{"x@y.z"}}}
	rule := Rule{Transformer: "json", Paths: map[string]Rule{"email": {Transformer: "email"}, "nested.list": {Transformer: "email"}}}
	out, err := f.Transform(ValueContext{PGType: "jsonb"}, doc, rule)
	if err != nil {
		t.Fatal(err)
	}
	if doc["email"] != "a@b.c" || doc["nested"].(map[string]any)["list"].([]any)[0] != "x@y.z" {
		t.Error("JSON document input was modified")
	}
	if o := out.(map[string]any); o["email"] == "a@b.c" || o["nested"].(map[string]any)["list"].([]any)[0] == "x@y.z" {
		t.Error("JSON document was not transformed")
	}
}

func TestNew(t *testing.T) {
	for _, key := range [][]byte{nil, {}} {
		if _, err := New(Options{Key: key}); !errors.Is(err, ErrNoKey) {
			t.Errorf("New(key=%v) = %v, want ErrNoKey", key, err)
		}
	}
	// The key is copied: wiping the caller's slice changes nothing.
	key := []byte(testKey)
	f, err := New(Options{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	for i := range key {
		key[i] = 0
	}
	if fake(t, f, Rule{Transformer: "email"}, "a@b.c") != fake(t, newFaker(t, testKey, nil), Rule{Transformer: "email"}, "a@b.c") {
		t.Error("Faker depends on the caller's key slice")
	}

	for name, rules := range map[string]map[string]Rule{
		"unknown transformer": {"users.email": {Transformer: "emial"}},
		"missing transformer": {"users.email": {}},
		"bad key":             {"email": {Transformer: "email"}},
		"empty key part":      {"users..email": {Transformer: "email"}},
		"too many parts":      {"a.b.c.d": {Transformer: "email"}},
		"bad nested rule":     {"users.doc": {Transformer: "json", Paths: map[string]Rule{"a": {Transformer: "nope"}}}},
		"bad path":            {"users.doc": {Transformer: "json", Paths: map[string]Rule{"a..b": {Transformer: "email"}}}},
		"paths without json":  {"users.doc": {Transformer: "email", Paths: map[string]Rule{"a": {Transformer: "email"}}}},
		"negative length":     {"users.t": {Transformer: "token", Length: -1}},
		"huge length":         {"users.t": {Transformer: "token", Length: 1 << 30}},
		"negative days":       {"users.d": {Transformer: "date", Days: -1}},
		"control in scope":    {"users.e": {Transformer: "email", Scope: "a\x00b"}},
	} {
		_, err := New(Options{Key: []byte(testKey), Rules: rules})
		var perr *Error
		if !errors.As(err, &perr) || !(errors.Is(err, ErrInvalidRule) || errors.Is(err, ErrUnknownTransformer)) {
			t.Errorf("%s: New = %v", name, err)
		}
	}
	if _, err := New(Options{Key: []byte(testKey), Rules: map[string]Rule{"users.email": {Transformer: "emial"}}}); !errors.Is(err, ErrUnknownTransformer) {
		t.Errorf("want ErrUnknownTransformer, got %v", err)
	}
}

func TestMaxLength(t *testing.T) {
	f := newFaker(t, testKey, nil)
	text := func(pgtype string, rule Rule, in string) (string, error) {
		out, err := f.Transform(ValueContext{PGType: pgtype}, in, rule)
		if err != nil {
			return "", err
		}
		return out.(string), nil
	}
	for i := 0; i < 200; i++ {
		in := fmt.Sprintf("value %d", i)
		for _, name := range []string{"full_name", "company", "username", "hash", "token", "secret", "first_name"} {
			for _, pgtype := range []string{"varchar(6)", "character varying(6)", "char(6)", "character(6)", "bpchar(6)"} {
				out, err := text(pgtype, Rule{Transformer: name, Unique: true}, in)
				if err != nil || utf8.RuneCountInString(out) > 6 || out == "" {
					t.Fatalf("%s into %s = %q, %v", name, pgtype, out, err)
				}
			}
		}
		for _, n := range []int{14, 15, 20, 30, 38, 64} {
			out, err := text(fmt.Sprintf("varchar(%d)", n), Rule{Transformer: "email"}, in)
			if err != nil || len(out) > n || !emailRE.MatchString(out) {
				t.Fatalf("email into varchar(%d) = %q, %v", n, out, err)
			}
			out, err = text("text", Rule{Transformer: "email", MaxLength: n, PreserveLength: true}, strings.Repeat("x", 80)+"@gmail.com")
			if err != nil || len(out) > n || !emailRE.MatchString(out) {
				t.Fatalf("email max_length %d = %q, %v", n, out, err)
			}
		}
	}
	// Values that cannot be shortened without breaking them are refused.
	for _, c := range []struct {
		pgtype string
		rule   Rule
		in     string
	}{
		{"varchar(13)", Rule{Transformer: "email"}, "a@b.c"},
		{"varchar(20)", Rule{Transformer: "email", PreserveDomain: true}, "a@a-rather-long-domain.example"},
		{"varchar(10)", Rule{Transformer: "uuid"}, "x"},
		{"varchar(5)", Rule{Transformer: "ip"}, "203.0.113.7"},
		{"varchar(8)", Rule{Transformer: "phone"}, "not a phone"},
		{"varchar(4)", Rule{Transformer: "date"}, "not a date"},
	} {
		if _, err := text(c.pgtype, c.rule, c.in); !errors.Is(err, ErrValueTooLong) {
			t.Errorf("%s into %s: err = %v, want ErrValueTooLong", c.rule.Transformer, c.pgtype, err)
		}
	}
	// max_length overrides the type modifier; multi-byte output is cut on
	// character boundaries.
	if out, _ := text("varchar(50)", Rule{Transformer: "full_name", MaxLength: 3}, "x"); len(out) != 3 {
		t.Errorf("max_length override: %q", out)
	}
	if out, _ := text("varchar(3)", Rule{Transformer: "string"}, "é—ü—é"); utf8.RuneCountInString(out) != 3 || !utf8.ValidString(out) {
		t.Errorf("multi-byte truncation: %q", out)
	}
}

func TestCitext(t *testing.T) {
	f := newFaker(t, testKey, nil)
	get := func(pgtype, in string) string {
		out, err := f.Transform(ValueContext{PGType: pgtype}, in, Rule{Transformer: "email", Scope: "e"})
		if err != nil {
			t.Fatal(err)
		}
		return out.(string)
	}
	if get("citext", "Alex@Gmail.com") != get("citext", "alex@gmail.com") {
		t.Error("citext values equal in PostgreSQL must stay equal")
	}
	if get("text", "Alex@Gmail.com") == get("text", "alex@gmail.com") {
		t.Error("text values that differ must stay different")
	}
	if get("citext", "alex@gmail.com") != get("text", "alex@gmail.com") {
		t.Error("citext and text should agree on lowercase values")
	}
}

func TestSecretsStayOutOfLogs(t *testing.T) {
	const key = "KEY-MATERIAL-5f3a9c"
	opts := Options{Key: []byte(key)}
	f, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		for _, v := range []any{f, opts, &opts} {
			out := fmt.Sprintf(format, v)
			if strings.Contains(out, key) || strings.Contains(out, hex.EncodeToString([]byte(key))) ||
				strings.Contains(out, fmt.Sprint([]byte(key))) {
				t.Errorf("fmt %s of %T exposes the key: %s", format, v, out)
			}
		}
	}
}
