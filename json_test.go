package pgfake

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var jsonRule = Rule{Transformer: "json", Paths: map[string]Rule{
	"name":           {Transformer: "full_name"},
	"email":          {Transformer: "email", Scope: "user_email"},
	"settings.phone": {Transformer: "phone"},
}}

const jsonDoc = `{
  "name": "Alex",
  "email": "alex@gmail.com",
  "settings": {"phone": "+13105551234", "theme": "dark", "limit": 12345678901234567890.5},
  "tags": ["a", "b"],
  "html": "<b>&</b>",
  "active": true,
  "deleted": null
}`

func TestJSONPaths(t *testing.T) {
	f := newFaker(t, testKey, nil)
	vc := ValueContext{Schema: "public", Table: "users", Column: "profile", PGType: "jsonb"}
	out, err := f.Transform(vc, jsonDoc, jsonRule)
	if err != nil {
		t.Fatal(err)
	}
	text := out.(string)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, text)
	}
	settings := doc["settings"].(map[string]any)

	if !fullNameRE.MatchString(doc["name"].(string)) {
		t.Errorf("name = %v", doc["name"])
	}
	// An explicit scope ties the JSON field to plain columns.
	if want := fake(t, f, Rule{Transformer: "email", Scope: "user_email"}, "alex@gmail.com"); doc["email"] != want {
		t.Errorf("email = %v, want %s", doc["email"], want)
	}
	if p := settings["phone"].(string); !strings.HasPrefix(p, "+1555") || len(p) != 12 {
		t.Errorf("phone = %v", p)
	}
	// Everything else is untouched, byte for byte where it matters.
	if settings["theme"] != "dark" || doc["active"] != true || doc["deleted"] != nil {
		t.Errorf("untouched fields changed: %s", text)
	}
	for _, keep := range []string{`"limit":12345678901234567890.5`, `"html":"<b>&</b>"`, `"tags":["a","b"]`} {
		if !strings.Contains(text, keep) {
			t.Errorf("output lost %s: %s", keep, text)
		}
	}
	for _, leak := range []string{"Alex", "alex@gmail.com", "3105551234"} {
		if strings.Contains(text, leak) {
			t.Errorf("output still contains %q: %s", leak, text)
		}
	}
	if again, _ := f.Transform(vc, jsonDoc, jsonRule); again != out {
		t.Error("json output is not deterministic")
	}

	// Without an explicit scope a path is scoped to its own location.
	nameAt := func(vc ValueContext) any {
		out, err := f.Transform(vc, map[string]any{"name": "Alex"}, jsonRule)
		if err != nil {
			t.Fatal(err)
		}
		return out.(map[string]any)["name"]
	}
	if nameAt(vc) != doc["name"] {
		t.Error("decoded and text documents disagree")
	}
	other := vc
	other.Column = "settings"
	if nameAt(other) == doc["name"] {
		t.Error("the same path in another column shares an identity by default")
	}
	want, _ := f.Transform(ValueContext{}, "Alex", Rule{Transformer: "full_name", Scope: "public.users.profile.name"})
	if doc["name"] != want {
		t.Errorf("default path scope should be schema.table.column.path: %v vs %v", doc["name"], want)
	}
}

func TestJSONShapes(t *testing.T) {
	f := newFaker(t, testKey, nil)
	email := Rule{Transformer: "email", Scope: "e"}
	want := fake(t, f, email, "a@b.c")
	run := func(doc string, paths map[string]Rule) string {
		t.Helper()
		out, err := f.Transform(ValueContext{PGType: "jsonb"}, doc, Rule{Transformer: "json", Paths: paths})
		if err != nil {
			t.Fatalf("%s: %v", doc, err)
		}
		return out.(string)
	}
	for _, c := range []struct {
		name, doc, want string
		paths           map[string]Rule
	}{
		{"arrays of objects are traversed", `{"contacts":[{"email":"a@b.c"},{"email":null},{"other":1}]}`,
			`{"contacts":[{"email":"` + want + `"},{"email":null},{"other":1}]}`, map[string]Rule{"contacts.email": email}},
		{"arrays of scalars", `{"emails":["a@b.c","a@b.c"]}`,
			`{"emails":["` + want + `","` + want + `"]}`, map[string]Rule{"emails": email}},
		{"top-level array", `[{"email":"a@b.c"}]`, `[{"email":"` + want + `"}]`, map[string]Rule{"email": email}},
		{"missing paths are ignored", `{"a":{"b":1}}`, `{"a":{"b":1}}`, map[string]Rule{"x.y": email, "a.b.c": email}},
		{"null leaves stay null", `{"email":null}`, `{"email":null}`, map[string]Rule{"email": email}},
		{"empty strings stay empty", `{"email":""}`, `{"email":""}`, map[string]Rule{"email": email}},
		{"null transformer", `{"email":"a@b.c","n":1}`, `{"email":null,"n":1}`, map[string]Rule{"email": {Transformer: "null"}}},
		{"null replaces objects", `{"pii":{"a":1}}`, `{"pii":null}`, map[string]Rule{"pii": {Transformer: "null"}}},
		{"no paths", `{ "a" : 1 }`, `{"a":1}`, nil},
		{"scalar document", `"x"`, `"x"`, map[string]Rule{"a": email}},
		{"nested json rule", `{"a":{"email":"a@b.c"}}`, `{"a":{"email":"` + want + `"}}`,
			map[string]Rule{"a": {Transformer: "json", Paths: map[string]Rule{"email": email}}}},
		{"json in a string", `{"raw":"{\"email\":\"a@b.c\"}"}`, `{"raw":"{\"email\":\"` + want + `\"}"}`,
			map[string]Rule{"raw": {Transformer: "json", Paths: map[string]Rule{"email": email}}}},
	} {
		if got := run(c.doc, c.paths); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}

	// Integers inside documents can be pseudonymized and stay integers.
	out := run(`{"id":42,"big":9007199254740993}`, map[string]Rule{"id": {Transformer: "hash"}, "big": {Transformer: "hash"}})
	var ids map[string]json.Number
	if err := json.Unmarshal([]byte(out), &ids); err != nil {
		t.Fatal(err)
	}
	for k, v := range ids {
		if n, err := v.Int64(); err != nil || n < 0 || v == "42" || v == "9007199254740993" {
			t.Errorf("%s = %s", k, v)
		}
	}

	// Empty text is preserved like an empty string anywhere else.
	if got, err := f.Transform(ValueContext{}, "", jsonRule); err != nil || got != "" {
		t.Errorf("empty document: %v, %v", got, err)
	}
}

func TestJSONErrors(t *testing.T) {
	f := newFaker(t, testKey, nil)
	for _, bad := range []string{`{`, `{"a":}`, `{"a":1} trailing`, `{"a":1}{"b":2}`, `not json`, `{'a':1}`, "\x00"} {
		for _, v := range []any{bad, []byte(bad), json.RawMessage(bad)} {
			out, err := f.Transform(ValueContext{Table: "t", Column: "doc", PGType: "jsonb"}, v, jsonRule)
			if !errors.Is(err, ErrInvalidJSON) || out != nil {
				t.Errorf("%T %q: %v, %v", v, bad, out, err)
			}
		}
	}
	// A failing leaf reports its path and fails the whole document.
	rule := Rule{Transformer: "json", Paths: map[string]Rule{"settings.flag": {Transformer: "email"}}}
	out, err := f.Transform(ValueContext{Table: "t", Column: "doc"}, `{"settings":{"flag":true}}`, rule)
	var perr *Error
	if !errors.As(err, &perr) || !errors.Is(err, ErrTypeMismatch) || out != nil {
		t.Fatalf("got %v, %v", out, err)
	}
	if perr.Path != "settings.flag" || perr.Column != "doc" || perr.Transformer != "email" {
		t.Errorf("error context: %#v", perr)
	}
	if want := `pgfake: t.doc.settings.flag: email: transformer does not support this type: "email" cannot produce boolean values`; err.Error() != want {
		t.Errorf("message:\n got %s\nwant %s", err, want)
	}
}
