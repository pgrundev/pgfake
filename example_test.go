package pgfake_test

import (
	"fmt"

	"github.com/pgrundev/pgfake"
)

func ExampleFaker_Transform() {
	faker, err := pgfake.New(pgfake.Options{Key: []byte("example-key")})
	if err != nil {
		panic(err)
	}

	rule := pgfake.Rule{Transformer: "email", Scope: "user_email"}

	// The same value under the same scope is the same fake value everywhere.
	a, _ := faker.Transform(pgfake.ValueContext{Table: "users", Column: "email"}, "alex@gmail.com", rule)
	b, _ := faker.Transform(pgfake.ValueContext{Table: "orders", Column: "customer_email"}, "alex@gmail.com", rule)
	null, _ := faker.Transform(pgfake.ValueContext{Table: "users", Column: "email"}, nil, rule)

	fmt.Println(a)
	fmt.Println(b)
	fmt.Println(null)
	// Output:
	// user_6ddae4ca594de840c2bc@example.test
	// user_6ddae4ca594de840c2bc@example.test
	// <nil>
}

func ExampleFaker_TransformRow() {
	faker, err := pgfake.New(pgfake.Options{
		Key: []byte("example-key"),
		Rules: map[string]pgfake.Rule{
			"users.name":    {Transformer: "full_name"},
			"users.email":   {Transformer: "email", Scope: "user_email"},
			"users.phone":   {Transformer: "phone"},
			"users.company": {Transformer: "company"},
		},
	})
	if err != nil {
		panic(err)
	}

	users := pgfake.Table{Name: "users", Columns: []pgfake.Column{
		{Name: "id", PGType: "bigint"},
		{Name: "name", PGType: "text"},
		{Name: "email", PGType: "text"},
		{Name: "phone", PGType: "text"},
		{Name: "company", PGType: "text"},
	}}
	real := []any{int64(42), "Alex Johnson", "alex@gmail.com", "+1 310 555 1234", "Acme Inc."}

	fake, err := faker.TransformRow(users, real)
	if err != nil {
		panic(err)
	}
	for i, col := range users.Columns {
		fmt.Printf("%-8s %-16v -> %v\n", col.Name, real[i], fake[i])
	}
	// Output:
	// id       42               -> 42
	// name     Alex Johnson     -> Derek Harrison
	// email    alex@gmail.com   -> user_6ddae4ca594de840c2bc@example.test
	// phone    +1 310 555 1234  -> +1 555 621 5485
	// company  Acme Inc.        -> Timber Media
}

func ExampleFaker_Transform_json() {
	faker, err := pgfake.New(pgfake.Options{Key: []byte("example-key")})
	if err != nil {
		panic(err)
	}

	doc := `{"name": "Alex", "settings": {"phone": "+13105551234", "theme": "dark"}}`
	fake, err := faker.Transform(
		pgfake.ValueContext{Table: "users", Column: "profile", PGType: "jsonb"},
		doc,
		pgfake.Rule{Transformer: "json", Paths: map[string]pgfake.Rule{
			"name":           {Transformer: "first_name"},
			"settings.phone": {Transformer: "phone"},
		}},
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(fake)
	// Output:
	// {"name":"Donna","settings":{"phone":"+15559535033","theme":"dark"}}
}
