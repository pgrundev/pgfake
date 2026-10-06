// Package pgfake turns real production values into safe, deterministic,
// realistic fake values.
//
// Every fake value is derived with HMAC-SHA256 from
//
//	secret key + transformer + relationship scope + original value
//
// and from nothing else: there is no mapping table and no state. The same
// input therefore yields the same output on every run, on every machine, in
// every goroutine, for as long as the key and the rule stay the same.
//
//	faker, err := pgfake.New(pgfake.Options{Key: key})
//	fake, err := faker.Transform(
//	    pgfake.ValueContext{Table: "users", Column: "email", PGType: "text"},
//	    "alex@gmail.com",
//	    pgfake.Rule{Transformer: "email", Scope: "user_email"},
//	)
//
// NULL (nil) is always preserved, as are empty strings. Values are never
// logged and never appear in errors.
//
// pgfake only transforms values. Moving rows between databases is the job of
// the caller.
package pgfake

import (
	"sync"

	"github.com/pgrundev/pgfake/internal/deterministic"
	"github.com/pgrundev/pgfake/internal/transformers"
)

// Version is the library version.
const Version = "0.1.0"

// AlgorithmVersion is part of every HMAC namespace
// ("pgfake:v1:<transformer>:<scope>"). Outputs for a given version never
// change; a different algorithm would ship under a new version.
const AlgorithmVersion = deterministic.Version

// Transformer transforms a single value. *Faker implements it; code that
// embeds pgfake can depend on this interface alone.
type Transformer interface {
	Transform(vc ValueContext, value any, rule Rule) (any, error)
}

// RowTransformer transforms one row of a table. *Faker implements it.
type RowTransformer interface {
	TransformRow(table Table, row []any) ([]any, error)
}

var (
	_ Transformer    = (*Faker)(nil)
	_ RowTransformer = (*Faker)(nil)
)

// Faker is the transformation engine. It is immutable after New and safe for
// concurrent use by any number of goroutines.
type Faker struct {
	deriver *deterministic.Deriver
	rules   map[ruleKey]*compiled
	pool    sync.Pool // *transformers.Input, each owning a Stream
}

// New validates opts and returns a Faker.
func New(opts Options) (*Faker, error) {
	if len(opts.Key) == 0 {
		return nil, &Error{Err: ErrNoKey}
	}
	f := &Faker{
		deriver: deterministic.New(opts.Key),
		rules:   make(map[ruleKey]*compiled, len(opts.Rules)),
	}
	f.pool.New = func() any {
		return &transformers.Input{S: f.deriver.NewStream()}
	}
	for key, rule := range opts.Rules {
		k, err := parseRuleKey(key)
		if err != nil {
			return nil, &Error{Err: err}
		}
		c, err := compile(rule)
		if err != nil {
			return nil, &Error{Schema: k.schema, Table: k.table, Column: k.column, Err: err}
		}
		f.rules[k] = &c
	}
	return f, nil
}

// Transform applies rule to value.
//
// The result has the same Go type as value: string, []byte and
// json.RawMessage stay what they are, integers keep their width, time.Time,
// [16]byte (UUID), netip.Addr, netip.Prefix and net.IP are returned as such,
// and []string / []any are transformed element by element. nil is returned as
// nil. The input is never modified.
func (f *Faker) Transform(vc ValueContext, value any, rule Rule) (any, error) {
	loc := location{schema: vc.Schema, table: vc.Table, column: vc.Column, pgtype: vc.PGType}
	c, err := compile(rule)
	if err != nil {
		return nil, loc.wrap(rule.Transformer, err)
	}
	return f.apply(&loc, value, &c)
}

// TransformRow applies the rules from Options.Rules to one row and returns a
// new row; the input slice is not modified. Columns without a rule (typically
// primary and foreign keys) are copied as they are.
//
// On error the returned row is nil. Callers must drop the row rather than
// fall back to the original values.
func (f *Faker) TransformRow(table Table, row []any) ([]any, error) {
	if len(row) != len(table.Columns) {
		return nil, &Error{Schema: table.Schema, Table: table.Name, Err: ErrRowShape}
	}
	out := make([]any, len(row))
	for i, col := range table.Columns {
		c := f.lookup(table.Schema, table.Name, col.Name)
		if c == nil {
			out[i] = row[i]
			continue
		}
		loc := location{schema: table.Schema, table: table.Name, column: col.Name, pgtype: col.PGType}
		v, err := f.apply(&loc, row[i], c)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (f *Faker) lookup(schema, table, column string) *compiled {
	if len(f.rules) == 0 {
		return nil
	}
	if schema == "" {
		schema = defaultSchema
	}
	if c := f.rules[ruleKey{schema, table, column}]; c != nil {
		return c
	}
	return f.rules[ruleKey{"", table, column}]
}

// Transformers returns the names of the built-in transformers.
func Transformers() []string {
	return append([]string(nil), transformers.Names...)
}

// String keeps key material out of logs when a Faker is printed.
func (f *Faker) String() string { return "pgfake.Faker" }

// GoString keeps key material out of logs when a Faker is printed with %#v.
func (f *Faker) GoString() string { return f.String() }
