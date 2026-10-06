package pgfake

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/vectors.json (only when adding vectors, never to make a failing vector pass)")

type vector struct {
	Name    string `json:"name"`
	Key     string `json:"key"`
	Context *struct {
		Schema string `json:"schema,omitempty"`
		Table  string `json:"table,omitempty"`
		Column string `json:"column,omitempty"`
		PGType string `json:"pgtype,omitempty"`
	} `json:"context,omitempty"`
	Rule   Rule   `json:"rule"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

const vectorsFile = "testdata/vectors.json"

// TestGoldenVectors is the compatibility contract of algorithm v1.
//
// Every vector pins the exact output for a key, rule and input. Databases
// masked on different days, or with different pgfake releases, only stay
// consistent if these outputs never change. A failing vector means fake
// identities would change for every user of the library: fix the code, do not
// regenerate the file. A deliberate change of algorithm must be released as a
// new algorithm version ("pgfake:v2:...") next to v1.
func TestGoldenVectors(t *testing.T) {
	data, err := os.ReadFile(vectorsFile)
	if err != nil {
		t.Fatal(err)
	}
	var vectors []vector
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) < 80 {
		t.Fatalf("only %d vectors; the file was truncated", len(vectors))
	}

	names := map[string]bool{}
	for i := range vectors {
		v := &vectors[i]
		if names[v.Name] {
			t.Errorf("duplicate vector name %q", v.Name)
		}
		names[v.Name] = true

		f, err := New(Options{Key: []byte(v.Key)})
		if err != nil {
			t.Fatal(err)
		}
		var vc ValueContext
		if v.Context != nil {
			vc = ValueContext{Schema: v.Context.Schema, Table: v.Context.Table, Column: v.Context.Column, PGType: v.Context.PGType}
		}
		out, err := f.Transform(vc, v.Input, v.Rule)
		if err != nil {
			t.Errorf("%s: %v", v.Name, err)
			continue
		}
		got := out.(string)
		if *update {
			v.Output = got
			continue
		}
		if got != v.Output {
			t.Errorf("%s: output changed\n input %q\n   got %q\n  want %q", v.Name, v.Input, got, v.Output)
		}
		// The byte form of the same value must agree with the text form.
		if b, err := f.Transform(vc, []byte(v.Input), v.Rule); err != nil || string(b.([]byte)) != v.Output {
			t.Errorf("%s: []byte input gives %q, %v", v.Name, b, err)
		}
	}

	if *update {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(vectors); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorsFile, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
