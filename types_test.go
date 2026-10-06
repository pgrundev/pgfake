package pgfake

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/pgrundev/pgfake/internal/transformers"
)

func TestParsePGType(t *testing.T) {
	for _, c := range []struct {
		in     string
		kind   transformers.Kind
		bits   int
		maxLen int
	}{
		{"", transformers.Text, 0, 0},
		{"text", transformers.Text, 0, 0},
		{"TEXT", transformers.Text, 0, 0},
		{"varchar", transformers.Text, 0, 0},
		{"varchar(64)", transformers.Text, 0, 64},
		{"character varying(255)", transformers.Text, 0, 255},
		{" Character Varying (12) ", transformers.Text, 0, 12},
		{"char(2)", transformers.Text, 0, 2},
		{"bpchar", transformers.Text, 0, 0},
		{"citext", transformers.Text, 0, 0},
		{"my_enum", transformers.Text, 0, 0},
		{"smallint", transformers.Int, 15, 0},
		{"int2", transformers.Int, 15, 0},
		{"integer", transformers.Int, 31, 0},
		{"int4", transformers.Int, 31, 0},
		{"serial", transformers.Int, 31, 0},
		{"bigint", transformers.Int, 63, 0},
		{"int8", transformers.Int, 63, 0},
		{"pg_catalog.int8", transformers.Int, 63, 0},
		{"uuid", transformers.UUID, 0, 0},
		{"date", transformers.Date, 0, 0},
		{"timestamp", transformers.Timestamp, 0, 0},
		{"timestamp without time zone", transformers.Timestamp, 0, 0},
		{"timestamp(3) without time zone", transformers.Timestamp, 0, 0},
		{"timestamptz", transformers.TimestampTZ, 0, 0},
		{"timestamp with time zone", transformers.TimestampTZ, 0, 0},
		{"timestamp(6) with time zone", transformers.TimestampTZ, 0, 0},
		{"inet", transformers.IP, 0, 0},
		{"cidr", transformers.IP, 0, 0},
		{"json", transformers.JSON, 0, 0},
		{"jsonb", transformers.JSON, 0, 0},
		{"boolean", transformers.Bool, 0, 0},
		{"numeric", transformers.Numeric, 0, 0},
		{"numeric(10,2)", transformers.Numeric, 0, 0},
		{"double precision", transformers.Numeric, 0, 0},
	} {
		pt := parsePGType(c.in)
		if pt.kind != c.kind || pt.bits != c.bits || pt.maxLen != c.maxLen || pt.array {
			t.Errorf("parsePGType(%q) = %+v", c.in, pt)
		}
	}
	if !parsePGType("citext").citext || !parsePGType("cidr").cidr {
		t.Error("citext/cidr flags not set")
	}
	for in, elem := range map[string]string{"text[]": "text", "_int4": "int4", "character varying(10)[]": "character varying(10)", "UUID[]": "uuid"} {
		if pt := parsePGType(in); !pt.array || pt.elem != elem {
			t.Errorf("parsePGType(%q) = %+v", in, pt)
		}
	}
}

func TestIntegers(t *testing.T) {
	f := newFaker(t, testKey, nil)
	rule := Rule{Transformer: "hash", Scope: "ids"}
	hash := func(pgtype string, v any) any {
		t.Helper()
		out, err := f.Transform(ValueContext{PGType: pgtype}, v, rule)
		if err != nil {
			t.Fatalf("hash(%T %v): %v", v, v, err)
		}
		if reflect.TypeOf(out) != reflect.TypeOf(v) {
			t.Fatalf("hash(%T) returned %T", v, out)
		}
		return out
	}

	// Go type and width are preserved; the result always fits.
	for _, v := range []any{
		int(42), int8(42), int16(42), int32(42), int64(42),
		uint(42), uint8(42), uint16(42), uint32(42), uint64(42),
		int8(math.MaxInt8), int16(math.MaxInt16), int32(math.MaxInt32), int64(math.MaxInt64),
		uint8(math.MaxUint8), uint16(math.MaxUint16), uint32(math.MaxUint32), uint64(math.MaxUint64),
		int8(math.MinInt8), int16(math.MinInt16), int32(math.MinInt32), int64(math.MinInt64), int64(0), int64(-1),
	} {
		out := hash("", v)
		if reflect.ValueOf(v).CanInt() && (reflect.ValueOf(v).Int() < 0) != (reflect.ValueOf(out).Int() < 0) {
			t.Errorf("hash(%T %v) = %v changes the sign", v, v, out)
		}
		if out2 := hash("", v); out2 != out {
			t.Errorf("hash(%T %v) not deterministic", v, v)
		}
	}

	// The column type bounds the result even for wider Go types.
	for i := int64(0); i < 2000; i++ {
		if out := hash("smallint", i).(int64); out < 0 || out > math.MaxInt16 {
			t.Fatalf("smallint hash(%d) = %d", i, out)
		}
		if out := hash("integer", i).(int64); out < 0 || out > math.MaxInt32 {
			t.Fatalf("integer hash(%d) = %d", i, out)
		}
		if out := hash("bigint", i).(int64); out < 0 {
			t.Fatalf("bigint hash(%d) = %d", i, out)
		}
	}
	if _, err := f.Transform(ValueContext{PGType: "smallint"}, int64(40000), rule); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("out-of-range smallint: %v", err)
	}

	// Text, binary and JSON forms of the same integer agree.
	want := strconv.FormatInt(hash("bigint", int64(42)).(int64), 10)
	if got := hash("bigint", "42"); got != want {
		t.Errorf("text form: %v, want %s", got, want)
	}
	if got := string(hash("bigint", []byte("42")).([]byte)); got != want {
		t.Errorf("bytes form: %v, want %s", got, want)
	}
	if got := hash("", json.Number("42")); got != json.Number(want) {
		t.Errorf("json.Number form: %v, want %s", got, want)
	}
	if got := hash("bigint", "-42").(string); got[0] != '-' {
		t.Errorf("negative text form: %s", got)
	}
	// Different widths are different permutations.
	if hash("integer", int64(42)) == hash("bigint", int64(42)) {
		t.Error("integer and bigint permutations coincide")
	}

	for _, bad := range []string{"abc", "12abc", "1.5", "99999999999999999999", " 42"} {
		if _, err := f.Transform(ValueContext{PGType: "integer"}, bad, rule); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("hash(integer %q): %v", bad, err)
		}
	}
}

// Integer identifiers go through a keyed permutation: unique in, unique out.
func TestIntegerHashIsBijective(t *testing.T) {
	f := newFaker(t, testKey, nil)
	rule := Rule{Transformer: "hash", Scope: "ids"}
	seen := make(map[int16]bool, 1<<16)
	for i := math.MinInt16; i <= math.MaxInt16; i++ {
		out, err := f.Transform(ValueContext{PGType: "smallint"}, int16(i), rule)
		if err != nil {
			t.Fatal(err)
		}
		seen[out.(int16)] = true
	}
	if len(seen) != 1<<16 {
		t.Fatalf("smallint: %d distinct outputs of %d", len(seen), 1<<16)
	}
	for _, pgtype := range []string{"integer", "bigint"} {
		seen := make(map[int64]bool, 100000)
		for i := int64(1); i <= 100000; i++ {
			out, err := f.Transform(ValueContext{PGType: pgtype}, i, rule)
			if err != nil {
				t.Fatal(err)
			}
			seen[out.(int64)] = true
		}
		if len(seen) != 100000 {
			t.Fatalf("%s: collisions among sequential ids", pgtype)
		}
	}
}

func TestTimeValues(t *testing.T) {
	f := newFaker(t, testKey, nil)
	rule := Rule{Transformer: "date", Days: 30}
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		ny = time.FixedZone("EST", -5*3600)
	}
	for _, c := range []struct {
		pgtype string
		in     time.Time
	}{
		{"date", time.Date(1987, 4, 12, 0, 0, 0, 0, time.UTC)},
		{"timestamp", time.Date(2024, 1, 15, 10, 30, 0, 123456000, time.UTC)},
		{"timestamptz", time.Date(2024, 1, 15, 10, 30, 0, 123456789, ny)},
		{"", time.Date(2024, 1, 15, 10, 30, 0, 0, time.FixedZone("", 5*3600+1800))},
	} {
		out, err := f.Transform(ValueContext{PGType: c.pgtype}, c.in, rule)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := out.(time.Time)
		if !ok {
			t.Fatalf("got %T", out)
		}
		if d := got.Sub(c.in); d == 0 || d < -30*24*time.Hour-time.Hour || d > 30*24*time.Hour+time.Hour {
			t.Errorf("%s: moved by %v", c.pgtype, d)
		}
		if got.Location() != c.in.Location() || got.Nanosecond() != c.in.Nanosecond() {
			t.Errorf("%s: location or fraction changed: %v -> %v", c.pgtype, c.in, got)
		}
		if c.pgtype == "date" && (got.Hour() != 0 || got.Minute() != 0 || got.Second() != 0) {
			t.Errorf("date gained a time of day: %v", got)
		}
		again, _ := f.Transform(ValueContext{PGType: c.pgtype}, c.in, rule)
		if !again.(time.Time).Equal(got) {
			t.Errorf("%s: not deterministic", c.pgtype)
		}
	}
	// The same instant in two zones is the same value.
	a := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	x, _ := f.Transform(ValueContext{PGType: "timestamptz"}, a, rule)
	y, _ := f.Transform(ValueContext{PGType: "timestamptz"}, a.In(ny), rule)
	if !x.(time.Time).Equal(y.(time.Time)) {
		t.Error("equal instants map to different instants")
	}
	// A date column gives the same day in text and binary form.
	txt, _ := f.Transform(ValueContext{PGType: "date"}, "1987-04-12", rule)
	bin, _ := f.Transform(ValueContext{PGType: "date"}, time.Date(1987, 4, 12, 0, 0, 0, 0, time.UTC), rule)
	if bin.(time.Time).Format("2006-01-02") != txt {
		t.Errorf("date text %v != binary %v", txt, bin)
	}
}

func TestIPValues(t *testing.T) {
	f := newFaker(t, testKey, nil)
	rule := Rule{Transformer: "ip", Scope: "ips"}
	tr := func(pgtype string, v any) any {
		t.Helper()
		out, err := f.Transform(ValueContext{PGType: pgtype}, v, rule)
		if err != nil {
			t.Fatal(err)
		}
		if reflect.TypeOf(out) != reflect.TypeOf(v) {
			t.Fatalf("ip(%T) returned %T", v, out)
		}
		return out
	}
	want := tr("inet", "203.0.113.7").(string)
	if got := tr("inet", netip.MustParseAddr("203.0.113.7")).(netip.Addr); got.String() != want {
		t.Errorf("netip.Addr: %v, want %s", got, want)
	}
	if got := tr("inet", netip.MustParsePrefix("203.0.113.7/32")).(netip.Prefix); got.Addr().String() != want || got.Bits() != 32 {
		t.Errorf("netip.Prefix /32: %v, want %s", got, want)
	}
	if got := tr("inet", net.ParseIP("203.0.113.7")).(net.IP); got.String() != want {
		t.Errorf("net.IP: %v, want %s", got, want)
	}
	if got := tr("inet", netip.MustParsePrefix("203.0.113.7/24")).(netip.Prefix); got.Bits() != 24 || !netip.MustParsePrefix("10.0.0.0/8").Contains(got.Addr()) {
		t.Errorf("netip.Prefix /24: %v", got)
	}
	if got := tr("cidr", netip.MustParsePrefix("203.0.113.0/24")).(netip.Prefix); got != got.Masked() || got.Bits() != 24 {
		t.Errorf("cidr prefix: %v", got)
	}
	if got := tr("inet", netip.MustParseAddr("2a00:1450::1")).(netip.Addr); !netip.MustParsePrefix("2001:db8::/32").Contains(got) {
		t.Errorf("IPv6: %v", got)
	}
	// Invalid zero values are passed through like NULL.
	if got := tr("inet", netip.Addr{}).(netip.Addr); got.IsValid() {
		t.Errorf("zero Addr became %v", got)
	}
	if got := tr("inet", net.IP(nil)).(net.IP); got != nil {
		t.Errorf("nil net.IP became %v", got)
	}
}

func TestSlices(t *testing.T) {
	f := newFaker(t, testKey, nil)
	rule := Rule{Transformer: "email", Scope: "e"}
	want := fake(t, f, rule, "a@b.c")

	out, err := f.Transform(ValueContext{PGType: "text[]"}, []string{"a@b.c", "", "d@e.f"}, rule)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.([]string); len(got) != 3 || got[0] != want || got[1] != "" || got[2] == want {
		t.Errorf("[]string = %q", got)
	}
	out, err = f.Transform(ValueContext{PGType: "_text"}, []any{"a@b.c", nil, []byte("a@b.c")}, rule)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.([]any); len(got) != 3 || got[0] != want || got[1] != nil || string(got[2].([]byte)) != want {
		t.Errorf("[]any = %v", got)
	}
	// Element types follow the array type.
	out, err = f.Transform(ValueContext{PGType: "integer[]"}, []any{int32(1), int32(2)}, Rule{Transformer: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range out.([]any) {
		if v, ok := e.(int32); !ok || v < 0 {
			t.Errorf("integer[] element %T %v", e, e)
		}
	}
	out, err = f.Transform(ValueContext{PGType: "varchar(5)[]"}, []string{"x"}, Rule{Transformer: "full_name"})
	if err != nil || len(out.([]string)[0]) != 5 {
		t.Errorf("varchar(5)[] = %v, %v", out, err)
	}
	// Array literals in text form are refused rather than mangled.
	for _, v := range []any{"{a@b.c,d@e.f}", []byte("{a@b.c}")} {
		if _, err := f.Transform(ValueContext{PGType: "text[]"}, v, rule); !errors.Is(err, ErrUnsupportedValue) {
			t.Errorf("array literal %T: %v", v, err)
		}
	}
}

func TestTypeMismatch(t *testing.T) {
	f := newFaker(t, testKey, nil)
	for _, c := range []struct {
		pgtype string
		value  any
		name   string
		want   error
	}{
		{"integer", "42", "email", ErrTypeMismatch},
		{"", 42, "email", ErrTypeMismatch},
		{"", int64(42), "full_name", ErrTypeMismatch},
		{"", true, "hash", ErrTypeMismatch},
		{"boolean", "t", "hash", ErrTypeMismatch},
		{"", 3.14, "hash", ErrTypeMismatch},
		{"numeric", "3.14", "hash", ErrTypeMismatch},
		{"", json.Number("3.14"), "hash", ErrTypeMismatch},
		{"uuid", "550e8400-e29b-41d4-a716-446655440000", "email", ErrTypeMismatch},
		{"", [16]byte{1}, "token", ErrTypeMismatch},
		{"date", "2024-01-01", "uuid", ErrTypeMismatch},
		{"", time.Now(), "ip", ErrTypeMismatch},
		{"inet", "10.0.0.1", "date", ErrTypeMismatch},
		{"", netip.MustParseAddr("10.0.0.1"), "hash", ErrTypeMismatch},
		{"jsonb", `{"a":1}`, "email", ErrTypeMismatch},
		{"", 42, "json", ErrTypeMismatch},
		{"", struct{ A string }{"x"}, "email", ErrUnsupportedValue},
		{"", map[string]any{"a": "b"}, "email", ErrUnsupportedValue},
		{"", &struct{}{}, "hash", ErrUnsupportedValue},
		{"", new(string), "email", ErrUnsupportedValue},
		{"", "x", "no_such_transformer", ErrUnknownTransformer},
		{"", "x", "", ErrInvalidRule},
	} {
		out, err := f.Transform(ValueContext{Table: "t", Column: "c", PGType: c.pgtype}, c.value, Rule{Transformer: c.name})
		if !errors.Is(err, c.want) {
			t.Errorf("%s on %s %T: err = %v, want %v", c.name, c.pgtype, c.value, err, c.want)
		}
		if out != nil {
			t.Errorf("%s on %T: returned %v alongside an error", c.name, c.value, out)
		}
		var perr *Error
		if !errors.As(err, &perr) || perr.Table != "t" || perr.Column != "c" || perr.Transformer != c.name {
			t.Errorf("%s on %T: error lacks context: %#v", c.name, c.value, err)
		}
	}
	// copy and null accept anything, including types pgfake knows nothing about.
	for _, v := range []any{true, 3.14, struct{}{}, fmt.Errorf("x")} {
		if _, err := f.Transform(ValueContext{}, v, Rule{Transformer: "copy"}); err != nil {
			t.Errorf("copy(%T): %v", v, err)
		}
	}
}

func TestBytesAndRawMessage(t *testing.T) {
	f := newFaker(t, testKey, nil)
	rule := Rule{Transformer: "email", Scope: "e"}
	want := fake(t, f, rule, "a@b.c")
	out, err := f.Transform(ValueContext{}, []byte("a@b.c"), rule)
	if b, ok := out.([]byte); err != nil || !ok || string(b) != want {
		t.Errorf("[]byte: %v, %v", out, err)
	}
	out, err = f.Transform(ValueContext{PGType: "jsonb"}, json.RawMessage(`{"email":"a@b.c"}`),
		Rule{Transformer: "json", Paths: map[string]Rule{"email": rule}})
	if b, ok := out.(json.RawMessage); err != nil || !ok || string(b) != `{"email":"`+want+`"}` {
		t.Errorf("json.RawMessage: %s, %v", out, err)
	}
}
