package pgfake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// applyJSON runs the "json" transformer: it applies the rule's paths inside a
// JSON document and leaves everything else untouched.
//
// Documents given as text are decoded and re-encoded, which normalizes
// whitespace and key order (as jsonb does); numbers keep their exact digits.
// Documents given as map[string]any / []any are deep-copied first.
func (f *Faker) applyJSON(loc *location, value any, c *compiled) (any, error) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return value, nil
		}
		out, err := f.jsonText(loc, v, c)
		return out, err
	case []byte:
		if len(v) == 0 {
			return value, nil
		}
		out, err := f.jsonText(loc, string(v), c)
		return []byte(out), err
	case json.RawMessage:
		if len(v) == 0 {
			return value, nil
		}
		out, err := f.jsonText(loc, string(v), c)
		return json.RawMessage(out), err
	case map[string]any:
		if v == nil {
			return value, nil
		}
		return f.jsonPaths(loc, deepCopy(v), c)
	case []any:
		if v == nil {
			return value, nil
		}
		return f.jsonPaths(loc, deepCopy(v), c)
	}
	return nil, fmt.Errorf("%w: %q needs a JSON document, got %T", ErrTypeMismatch, c.spec.Name, value)
}

func (f *Faker) jsonText(loc *location, text string, c *compiled) (string, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var doc any
	// encoding/json errors quote the offending input, so they are dropped.
	if err := dec.Decode(&doc); err != nil {
		return "", ErrInvalidJSON
	}
	if _, err := dec.Token(); err != io.EOF {
		return "", ErrInvalidJSON
	}

	doc, err := f.jsonPaths(loc, doc, c)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return "", ErrInvalidJSON
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

func (f *Faker) jsonPaths(loc *location, doc any, c *compiled) (any, error) {
	for i := range c.paths {
		p := &c.paths[i]
		leaf := *loc
		leaf.pgtype = ""
		leaf.path = p.path
		if loc.path != "" {
			leaf.path = loc.path + "." + p.path
		}
		var err error
		if doc, err = f.jsonWalk(&leaf, doc, p.keys, &p.rule); err != nil {
			return nil, err
		}
	}
	return doc, nil
}

// jsonWalk descends along keys and transforms what it finds at the end.
// Arrays on the way are traversed; missing keys are not an error.
func (f *Faker) jsonWalk(leaf *location, node any, keys []string, rule *compiled) (any, error) {
	if len(keys) == 0 {
		return f.apply(leaf, node, rule)
	}
	switch n := node.(type) {
	case []any:
		for i := range n {
			var err error
			if n[i], err = f.jsonWalk(leaf, n[i], keys, rule); err != nil {
				return nil, err
			}
		}
	case map[string]any:
		child, ok := n[keys[0]]
		if !ok {
			return node, nil
		}
		out, err := f.jsonWalk(leaf, child, keys[1:], rule)
		if err != nil {
			return nil, err
		}
		n[keys[0]] = out
	}
	return node, nil
}

func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = deepCopy(e)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = deepCopy(e)
		}
		return out
	}
	return v
}
