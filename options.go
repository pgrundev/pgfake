package pgfake

// Options configures a Faker.
type Options struct {
	// Key is the secret all fake values are derived from. It is required.
	// Use at least 32 random bytes in production: anyone holding the key can
	// confirm guesses of original values. The slice is copied.
	Key []byte

	// Rules maps columns to rules for TransformRow. Keys are "table.column"
	// (any schema) or "schema.table.column"; the qualified form wins.
	// Columns without a rule are passed through unchanged.
	Rules map[string]Rule
}

// String keeps the key out of logs when Options is printed.
func (o Options) String() string { return "pgfake.Options{Key: [redacted]}" }

// GoString keeps the key out of logs when Options is printed with %#v.
func (o Options) GoString() string { return o.String() }
