package pgfake

import (
	"errors"
	"reflect"
	"testing"
)

var userRules = map[string]Rule{
	"users.email":                 {Transformer: "email", Scope: "user_email"},
	"users.name":                  {Transformer: "full_name"},
	"users.phone":                 {Transformer: "phone"},
	"users.company":               {Transformer: "company"},
	"orders.customer_email":       {Transformer: "email", Scope: "user_email"},
	"audit.audit_logs.user_email": {Transformer: "email", Scope: "user_email"},
	"audit.users.name":            {Transformer: "null"},
}

var usersTable = Table{Schema: "public", Name: "users", Columns: []Column{
	{"id", "bigint"}, {"company_id", "integer"}, {"name", "text"}, {"email", "citext"},
	{"phone", "varchar(32)"}, {"company", "text"}, {"deleted_at", "timestamptz"},
}}

func TestTransformRow(t *testing.T) {
	f := newFaker(t, testKey, userRules)
	in := []any{int64(42), int32(7), "Alex Johnson", "alex@gmail.com", "+1 310 555 1234", "Acme Inc.", nil}
	keep := append([]any(nil), in...)

	out, err := f.TransformRow(usersTable, in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, keep) {
		t.Error("input row was modified")
	}
	// Keys and NULLs are preserved by default.
	if out[0] != int64(42) || out[1] != int32(7) || out[6] != nil {
		t.Errorf("keys or NULL changed: %v", out)
	}
	for i := 2; i <= 5; i++ {
		if out[i] == in[i] || out[i] == nil {
			t.Errorf("column %s not transformed: %v", usersTable.Columns[i].Name, out[i])
		}
	}
	// Same row, same result, on a different Faker.
	again, err := newFaker(t, testKey, userRules).TransformRow(usersTable, in)
	if err != nil || !reflect.DeepEqual(out, again) {
		t.Errorf("row not deterministic: %v vs %v", out, again)
	}

	// The shared scope keeps the logical relationship across tables/schemas.
	orders := Table{Name: "orders", Columns: []Column{{"id", "bigint"}, {"customer_email", "text"}}}
	o, err := f.TransformRow(orders, []any{int64(1), "alex@gmail.com"})
	if err != nil {
		t.Fatal(err)
	}
	audit := Table{Schema: "audit", Name: "audit_logs", Columns: []Column{{"user_email", "text"}}}
	a, err := f.TransformRow(audit, []any{"alex@gmail.com"})
	if err != nil {
		t.Fatal(err)
	}
	if o[1] != out[3] || a[0] != out[3] {
		t.Errorf("relationship broken: users=%v orders=%v audit=%v", out[3], o[1], a[0])
	}

	// Unqualified keys match any schema; qualified keys win.
	tenant := usersTable
	tenant.Schema = "tenant1"
	tr, err := f.TransformRow(tenant, in)
	if err != nil || tr[3] != out[3] || tr[2] == in[2] {
		t.Errorf("unqualified rule did not apply to another schema: %v, %v", tr, err)
	}
	if tr[2] == out[2] {
		t.Error("default scope should differ between schemas")
	}
	aud := usersTable
	aud.Schema = "audit"
	ar, err := f.TransformRow(aud, in)
	if err != nil || ar[2] != nil || ar[3] != out[3] {
		t.Errorf("qualified rule should win: %v, %v", ar, err)
	}
	// An empty schema is "public".
	pub := usersTable
	pub.Schema = ""
	pr, err := f.TransformRow(pub, in)
	if err != nil || !reflect.DeepEqual(pr, out) {
		t.Errorf("empty schema should behave like public: %v", pr)
	}

	// Tables without rules pass through.
	other := Table{Name: "products", Columns: []Column{{"id", "bigint"}, {"email", "text"}}}
	p, err := f.TransformRow(other, []any{int64(1), "alex@gmail.com"})
	if err != nil || p[1] != "alex@gmail.com" {
		t.Errorf("unconfigured table changed: %v, %v", p, err)
	}
	// A Faker without rules copies rows.
	c, err := newFaker(t, testKey, nil).TransformRow(usersTable, in)
	if err != nil || !reflect.DeepEqual(c, in) {
		t.Errorf("no rules: %v, %v", c, err)
	}
	// TransformRow and Transform agree.
	single, err := f.Transform(ValueContext{Schema: "public", Table: "users", Column: "name", PGType: "text"}, "Alex Johnson", userRules["users.name"])
	if err != nil || single != out[2] {
		t.Errorf("Transform = %v, TransformRow = %v", single, out[2])
	}
}

func TestTransformRowErrors(t *testing.T) {
	f := newFaker(t, testKey, userRules)
	if out, err := f.TransformRow(usersTable, []any{1, 2}); !errors.Is(err, ErrRowShape) || out != nil {
		t.Errorf("short row: %v, %v", out, err)
	}
	// One bad column fails the whole row; nothing original leaks out.
	row := []any{int64(42), int32(7), "Alex Johnson", 12345, "+1 310 555 1234", "Acme Inc.", nil}
	out, err := f.TransformRow(usersTable, row)
	var perr *Error
	if out != nil || !errors.As(err, &perr) || !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("got %v, %v", out, err)
	}
	if perr.Schema != "public" || perr.Table != "users" || perr.Column != "email" || perr.Transformer != "email" {
		t.Errorf("error context: %#v", perr)
	}
}

// A consumer such as pgsync only needs these two interfaces.
func TestInterfaces(t *testing.T) {
	var tr Transformer = newFaker(t, testKey, nil)
	var rt RowTransformer = newFaker(t, testKey, userRules)
	if out, err := tr.Transform(ValueContext{}, "a@b.c", Rule{Transformer: "email"}); err != nil || out == "a@b.c" {
		t.Errorf("Transformer: %v, %v", out, err)
	}
	if out, err := rt.TransformRow(Table{Name: "users", Columns: []Column{{"email", "text"}}}, []any{"a@b.c"}); err != nil || out[0] == "a@b.c" {
		t.Errorf("RowTransformer: %v, %v", out, err)
	}
}
