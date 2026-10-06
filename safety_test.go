package pgfake

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// marker stands for sensitive data. It must never show up in an error.
const marker = "ZQX-S3NSITIVE-9731"

const leakKey = "KEY-MATERIAL-5f3a9c"

func assertNoLeak(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	for _, text := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err), fmt.Sprintf("%q", err)} {
		low := strings.ToLower(text)
		for _, secret := range []string{marker, "s3nsitive", "9731", leakKey} {
			if strings.Contains(low, strings.ToLower(secret)) {
				t.Errorf("%s: error exposes %q: %s", what, secret, text)
			}
		}
	}
}

func TestErrorsNeverContainInput(t *testing.T) {
	f := newFaker(t, leakKey, nil)
	type holder struct{ Secret string }

	values := []any{
		marker,
		[]byte(marker),
		json.RawMessage(marker),
		json.RawMessage(`{"a": "` + marker + `", broken`),
		`{"a": "` + marker + `"`,
		`{"a": ` + marker + `}`,
		`{"a": "` + marker + `"} ` + marker,
		"9731" + marker,
		"{" + marker + "," + marker + "}",
		[]string{marker},
		[]any{marker, holder{marker}},
		map[string]any{marker: marker, "a": holder{marker}, "c": marker},
		`{"c": "` + marker + `", "a": {"b": "` + marker + `"}}`,
		map[string]any{"a": map[string]any{"b": holder{marker}}},
		holder{marker},
		&holder{marker},
		fmt.Errorf("%s", marker),
		json.Number("9731.5"),
		json.Number(marker),
		9731, int64(9731), uint8(97), 9731.5, true,
		time.Date(9731, 1, 1, 0, 0, 0, 0, time.UTC),
		netip.MustParseAddr("97.31.97.31"),
	}
	pgtypes := []string{"", "text", "varchar(3)", "char(1)", "smallint", "integer", "bigint", "uuid", "date",
		"timestamp", "timestamptz", "inet", "cidr", "json", "jsonb", "boolean", "numeric", "text[]", "integer[]", marker}

	rules := []Rule{}
	for _, name := range Transformers() {
		rules = append(rules,
			Rule{Transformer: name},
			Rule{Transformer: name, PreserveLength: true, PreserveDomain: true, MaxLength: 2},
		)
	}
	rules = append(rules,
		Rule{Transformer: "json", Paths: map[string]Rule{"a": {Transformer: "email"}, "a.b": {Transformer: "hash"}, "c": {Transformer: "uuid", MaxLength: 3}}},
		Rule{Transformer: "full_name", Unique: true, MaxLength: 1},
		Rule{Transformer: "phone", Unique: true, MaxLength: 4},
	)

	failures := 0
	for _, rule := range rules {
		for _, pgtype := range pgtypes {
			for _, v := range values {
				// Column names are configuration, not data; keep them clean
				// so that any marker in an error must come from the value.
				vc := ValueContext{Schema: "public", Table: "users", Column: "secret_col", PGType: pgtype}
				if pgtype == marker {
					continue
				}
				out, err := f.Transform(vc, v, rule)
				if err != nil {
					failures++
					if out != nil {
						t.Errorf("%s/%s/%T: value returned with error", rule.Transformer, pgtype, v)
					}
				}
				assertNoLeak(t, fmt.Sprintf("%s on %s %T", rule.Transformer, pgtype, v), err)
			}
		}
	}
	if failures < 500 {
		t.Fatalf("only %d error paths exercised; the test lost its teeth", failures)
	}

	// Rows fail closed and quietly, too.
	rf := newFaker(t, leakKey, map[string]Rule{"users.email": {Transformer: "email"}, "users.doc": {Transformer: "json", Paths: map[string]Rule{"a": {Transformer: "email"}}}})
	table := Table{Name: "users", Columns: []Column{{"email", "integer"}, {"doc", "jsonb"}}}
	for _, row := range [][]any{{marker, nil}, {nil, `{"a":"` + marker}, {nil, map[string]any{"a": holder{marker}}}, {marker}} {
		out, err := rf.TransformRow(table, row)
		if err == nil || out != nil {
			t.Errorf("row %v: expected failure, got %v", len(row), out)
		}
		assertNoLeak(t, "TransformRow", err)
	}

	// Configuration errors may name configuration, but never the key.
	_, err := New(Options{Key: []byte(leakKey), Rules: map[string]Rule{"users.email": {Transformer: "nope"}}})
	assertNoLeak(t, "New", err)
}

// Free-form transformers must not carry any part of the source into the
// output unless asked to (preserve_domain, string's punctuation, phone's
// formatting).
func TestOutputsDoNotExposeInput(t *testing.T) {
	f := newFaker(t, testKey, nil)
	inputs := []string{marker, "john.smith@" + marker + ".com", "sk_live_" + marker, marker + " " + marker}
	for _, name := range []string{"hash", "email", "username", "first_name", "last_name", "full_name", "company", "uuid", "ip", "date", "token", "secret", "phone", "string"} {
		for _, in := range inputs {
			for _, rule := range []Rule{{Transformer: name}, {Transformer: name, PreserveLength: true}, {Transformer: name, Length: 40}} {
				out := fake(t, f, rule, in)
				low := strings.ToLower(out)
				for _, piece := range []string{"zqx", "s3nsitive", "9731", "john", "smith", "sk_live"} {
					if strings.Contains(low, piece) {
						t.Errorf("%s(%q) = %q exposes %q", name, in, out, piece)
					}
				}
			}
		}
	}
}

func FuzzTransform(f *testing.F) {
	for _, s := range []string{
		"", " ", "alex@gmail.com", "+1 310 555 1234", "550e8400-e29b-41d4-a716-446655440000", "203.0.113.7/24",
		"2024-01-15 10:30:00+00", "1987-04-12", "::1", "42", "-42", `{"a":{"b":["x",1,null]}}`, "{a,b}", "\xff\xfe", "a\x00b",
		"99999999999999999999", "0000-00-00", "9999-12-31 23:59:59.999999999+14", "+", "@", strings.Repeat("9", 40),
	} {
		f.Add(s, uint8(0), uint8(0))
	}
	faker, err := New(Options{Key: []byte(testKey)})
	if err != nil {
		f.Fatal(err)
	}
	names := Transformers()
	pgtypes := []string{"", "text", "varchar(8)", "citext", "smallint", "integer", "bigint", "uuid", "date", "timestamp", "timestamptz", "inet", "cidr", "jsonb", "boolean", "numeric", "text[]"}
	f.Fuzz(func(t *testing.T, in string, a, b uint8) {
		name := names[int(a)%len(names)]
		rule := Rule{
			Transformer:    name,
			Unique:         b&1 != 0 && name != "string" && name != "ip" && name != "date",
			PreserveDomain: b&2 != 0,
			PreserveLength: b&4 != 0,
			Length:         int(b>>4) * 3,
		}
		if name == "json" {
			rule = Rule{Transformer: "json", Paths: map[string]Rule{"a": {Transformer: "email"}, "a.b": {Transformer: "phone"}}}
		}
		vc := ValueContext{Table: "t", Column: "c", PGType: pgtypes[int(b>>1)%len(pgtypes)]}
		for _, v := range []any{in, []byte(in)} {
			out1, err1 := faker.Transform(vc, v, rule)
			out2, err2 := faker.Transform(vc, v, rule)
			if (err1 == nil) != (err2 == nil) || fmt.Sprint(out1) != fmt.Sprint(out2) {
				t.Fatalf("%s is not deterministic", name)
			}
			if err1 != nil {
				if out1 != nil {
					t.Fatalf("%s returned a value with an error", name)
				}
				continue
			}
			if s, ok := out1.(string); ok && name != "copy" && name != "json" && !utf8.ValidString(s) {
				t.Fatalf("%s produced invalid UTF-8", name)
			}
		}
	})
}
