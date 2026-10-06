package pgfake

import (
	"fmt"
	"testing"
)

func BenchmarkTransform(b *testing.B) {
	f, err := New(Options{Key: []byte(testKey)})
	if err != nil {
		b.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		pgtype string
		value  any
		rule   Rule
	}{
		{"email", "text", "alex@gmail.com", Rule{Transformer: "email", Scope: "user_email"}},
		{"email_default_scope", "text", "alex@gmail.com", Rule{Transformer: "email"}},
		{"email_bytes", "text", []byte("alex@gmail.com"), Rule{Transformer: "email", Scope: "user_email"}},
		{"full_name", "text", "Alex Johnson", Rule{Transformer: "full_name"}},
		{"full_name_unique", "text", "Alex Johnson", Rule{Transformer: "full_name", Unique: true}},
		{"username", "text", "alexj", Rule{Transformer: "username"}},
		{"company", "text", "Acme Inc.", Rule{Transformer: "company"}},
		{"phone", "text", "+1 310 555 1234", Rule{Transformer: "phone"}},
		{"phone_unique", "text", "+1 310 555 1234", Rule{Transformer: "phone", Unique: true}},
		{"uuid_text", "uuid", "550e8400-e29b-41d4-a716-446655440000", Rule{Transformer: "uuid"}},
		{"uuid_binary", "uuid", [16]byte{0x55, 0x0e, 0x84}, Rule{Transformer: "uuid"}},
		{"ip", "inet", "203.0.113.7", Rule{Transformer: "ip"}},
		{"date", "date", "1987-04-12", Rule{Transformer: "date"}},
		{"timestamptz", "timestamptz", "2024-01-15 10:30:00.123456+00", Rule{Transformer: "date"}},
		{"token", "text", "sk_live_Hs83hdKq02", Rule{Transformer: "token"}},
		{"secret", "text", "sk_live_Hs83hdKq02", Rule{Transformer: "secret"}},
		{"hash_text", "text", "Alex Johnson", Rule{Transformer: "hash"}},
		{"hash_bigint", "bigint", int64(42), Rule{Transformer: "hash"}},
		{"string", "text", "AB-1234 xy", Rule{Transformer: "string"}},
		{"copy", "bigint", int64(42), Rule{Transformer: "copy"}},
		{"null_input", "text", nil, Rule{Transformer: "email"}},
	} {
		vc := ValueContext{Schema: "public", Table: "users", Column: "c", PGType: c.pgtype}
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := f.Transform(vc, c.value, c.rule); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkTransformJSON(b *testing.B) {
	f, err := New(Options{Key: []byte(testKey)})
	if err != nil {
		b.Fatal(err)
	}
	vc := ValueContext{Table: "users", Column: "profile", PGType: "jsonb"}
	b.ReportAllocs()
	b.SetBytes(int64(len(jsonDoc)))
	for i := 0; i < b.N; i++ {
		if _, err := f.Transform(vc, jsonDoc, jsonRule); err != nil {
			b.Fatal(err)
		}
	}
}

func benchRow(i int) []any {
	return []any{int64(i), int32(7), "Alex Johnson", fmt.Sprintf("user%d@gmail.com", i), "+1 310 555 1234", "Acme Inc.", nil}
}

func BenchmarkTransformRow(b *testing.B) {
	f, err := New(Options{Key: []byte(testKey), Rules: userRules})
	if err != nil {
		b.Fatal(err)
	}
	row := benchRow(1)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := f.TransformRow(usersTable, row); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTransformRowParallel(b *testing.B) {
	f, err := New(Options{Key: []byte(testKey), Rules: userRules})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		row := benchRow(1)
		for pb.Next() {
			if _, err := f.TransformRow(usersTable, row); err != nil {
				b.Fatal(err)
			}
		}
	})
}
