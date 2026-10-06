package pgfake

// ValueContext says where a value lives. All fields are optional.
//
// Schema, Table and Column form the default relationship scope of the value
// (see Rule.Scope) and label errors. PGType tells pgfake which PostgreSQL
// type the result must remain valid for.
type ValueContext struct {
	Schema string
	Table  string
	Column string
	// PGType is the PostgreSQL type name as reported by format_type() or
	// pg_type.typname: "text", "varchar(64)", "character varying(64)",
	// "int4", "bigint", "uuid", "timestamptz", "inet", "jsonb", "text[]", ...
	// Empty or unknown types are treated as text.
	PGType string
}

// Column describes one column of a Table.
type Column struct {
	Name   string
	PGType string
}

// Table describes the shape of the rows passed to Faker.TransformRow.
type Table struct {
	Schema  string // empty means "public"
	Name    string
	Columns []Column
}

// location is a ValueContext plus the JSON path being visited.
type location struct {
	schema, table, column, path string
	pgtype                      string
}

func (l *location) wrap(transformer string, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*Error); ok {
		return err
	}
	return &Error{
		Schema:      l.schema,
		Table:       l.table,
		Column:      l.column,
		Path:        l.path,
		Transformer: transformer,
		Err:         err,
	}
}
