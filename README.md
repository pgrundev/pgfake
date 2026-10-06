<div align="center">

# 🎭 pgfake

**Real data in. Safe fake data out.**

Deterministic data masking for PostgreSQL.<br>
A small Go library that turns production values into realistic fake ones —<br>
the same fake, every time, with no mapping table.

Go · zero deps · deterministic

<br>

[![CI](https://github.com/pgrundev/pgfake/actions/workflows/ci.yml/badge.svg)](https://github.com/pgrundev/pgfake/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/pgrundev/pgfake.svg)](https://pkg.go.dev/github.com/pgrundev/pgfake)
[![Go Report Card](https://goreportcard.com/badge/github.com/pgrundev/pgfake)](https://goreportcard.com/report/github.com/pgrundev/pgfake)
[![Go](https://img.shields.io/badge/go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Dependencies](https://img.shields.io/badge/dependencies-0-brightgreen)](go.mod)
[![Algorithm](https://img.shields.io/badge/algorithm-v1%20%C2%B7%20frozen-8A2BE2)](#compatibility-algorithm-v1)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

[Quickstart](#quickstart) · [Why](#why-pgfake) · [Go library](#go-library) · [Transformers](#transformers) · [Scopes](#relationship-scopes) · [Unique](#unique-columns) · [JSON](#json--jsonb) · [CLI](#cli) · [Safety](#safety) · [Compatibility](#compatibility-algorithm-v1)

</div>

---

```text
REAL DATA                         FAKE DATA

id        42                 →    42
name      Alex Johnson       →    Derek Harrison
email     alex@gmail.com     →    user_6ddae4ca594de840c2bc@example.test
phone     +1 310 555 1234    →    +1 555 621 5485
company   Acme Inc.          →    Timber Media
```

**Same schema. Same relationships. No real customer data.**

> **Status: v0.1.** The API is small and may still move. The *outputs* may not:
> everything produced under algorithm `v1` is pinned by
> [permanent test vectors](#compatibility-algorithm-v1), so a database masked
> today lines up with one masked after an upgrade.

pgfake is the transformation engine and nothing else. It does not connect to
databases, copy tables or replicate anything.

```text
Production PostgreSQL  →  your sync tool  →  pgfake  →  safe copy
                          (moves rows)       (transforms values)
```

## Quickstart

Try it from a shell — the output below is real, with this exact key:

```console
$ go install github.com/pgrundev/pgfake/cmd/pgfake@latest
$ export PGFAKE_KEY=example-key          # use `openssl rand -hex 32` for real data

$ pgfake value --type email --scope user_email --value alex@gmail.com
user_6ddae4ca594de840c2bc@example.test

$ pgfake value --type email --scope user_email --value alex@gmail.com   # again, any machine, any day
user_6ddae4ca594de840c2bc@example.test

$ PGFAKE_KEY=another-key pgfake value --type email --scope user_email --value alex@gmail.com
user_255422ccc583633b1199@example.test
```

Or use it as a library:

```bash
go get github.com/pgrundev/pgfake@latest
```

```go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/pgrundev/pgfake"
)

func main() {
	faker, err := pgfake.New(pgfake.Options{Key: []byte(os.Getenv("PGFAKE_KEY"))})
	if err != nil {
		log.Fatal(err)
	}

	fake, err := faker.Transform(
		pgfake.ValueContext{Table: "users", Column: "email", PGType: "text"},
		"alex@gmail.com",
		pgfake.Rule{Transformer: "email", Scope: "user_email"},
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(fake) // user_6ddae4ca594de840c2bc@example.test
}
```

Requires Go 1.22+. Nothing outside the standard library.

## Why pgfake

| | |
|---|---|
| **Deterministic, with no state** | Every fake value is `HMAC-SHA256(key, transformer + scope + value)`. No mapping table, no lookup database, nothing to back up or keep in sync. Same input, same output — tomorrow, on another server, in another goroutine. |
| **Relationships survive** | Give `users.email`, `orders.customer_email` and `audit_logs.user_email` one [scope](#relationship-scopes) and `alex@gmail.com` becomes the same fake address in all three. Joins and lookups keep working, with or without a foreign key. |
| **Realistic, not `********`** | An email stays a valid email, a UUID a valid UUID, a phone number keeps its formatting, a date stays a date. Your application behaves on the copy the way it does in production. |
| **Type-safe for PostgreSQL** | pgfake knows what the column is. It will not put a name into an `integer`, overflow a `varchar(20)`, or leave host bits in a `cidr`. When it cannot produce a valid value it returns a typed error instead. |
| **Keys stay keys** | Columns without a rule pass through untouched, so primary and foreign keys are preserved by default. When an id must be masked, integer ids go through a keyed *permutation*: distinct in, distinct out, always in range. |
| **Fails closed, and quietly** | `NULL` stays `NULL`. Errors never contain the value being transformed. There is no logging. A row that fails comes back as `nil`, never half-masked. |
| **Outputs are a contract** | 84 pinned vectors define algorithm `v1`. A release that changes one of them is a bug; a new algorithm ships as `v2` beside it. |
| **Small** | ~2,400 lines of Go, one public package, zero dependencies. About 330 ns per value. |

A log-redaction library answers *"how do I hide this?"*. pgfake answers
*"how do I get a database that still works?"*

## Go library

The whole public API:

```go
func New(Options) (*Faker, error)
func (*Faker) Transform(ValueContext, any, Rule) (any, error)
func (*Faker) TransformRow(Table, []any) ([]any, error)
func Transformers() []string
```

### One value

```go
fake, err := faker.Transform(
    pgfake.ValueContext{Schema: "public", Table: "users", Column: "email", PGType: "text"},
    "alex@gmail.com",
    pgfake.Rule{Transformer: "email", Scope: "user_email"},
)
```

### Whole rows

Configure rules once, then hand over rows. This is the path for a sync tool.

```go
faker, err := pgfake.New(pgfake.Options{
    Key: key,
    Rules: map[string]pgfake.Rule{
        "users.name":            {Transformer: "full_name"},
        "users.email":           {Transformer: "email", Scope: "user_email"},
        "users.phone":           {Transformer: "phone"},
        "users.company":         {Transformer: "company"},
        "orders.customer_email": {Transformer: "email", Scope: "user_email"},
    },
})

users := pgfake.Table{Name: "users", Columns: []pgfake.Column{
    {Name: "id", PGType: "bigint"},
    {Name: "name", PGType: "text"},
    {Name: "email", PGType: "text"},
    {Name: "phone", PGType: "text"},
    {Name: "company", PGType: "text"},
}}

fake, err := faker.TransformRow(users,
    []any{int64(42), "Alex Johnson", "alex@gmail.com", "+1 310 555 1234", "Acme Inc."})
```

```text
id       42               -> 42
name     Alex Johnson     -> Derek Harrison
email    alex@gmail.com   -> user_6ddae4ca594de840c2bc@example.test
phone    +1 310 555 1234  -> +1 555 621 5485
company  Acme Inc.        -> Timber Media
```

Rule keys are `table.column` (any schema) or `schema.table.column` (wins when
both match).

### Guarantees

- **Concurrent.** A `Faker` is immutable after `New` and safe to share between
  any number of goroutines. Results never depend on call order.
- **`NULL` in, `NULL` out.** `nil` is preserved by every transformer. So is the
  empty string.
- **Same type out.** `string` → `string`, `[]byte` → a fresh `[]byte`,
  `int32` → `int32`, `time.Time` → `time.Time`, `[16]byte` → `[16]byte`.
- **Inputs are never modified.** Not buffers, not slices, not JSON maps.
- **Nothing on error.** `Transform` returns `nil`; `TransformRow` returns a
  `nil` row. Drop the row — never fall back to the original.

### Embedding

A consumer needs only one of two interfaces, both implemented by `*Faker`:

```go
type Transformer interface {
    Transform(vc ValueContext, value any, rule Rule) (any, error)
}

type RowTransformer interface {
    TransformRow(table Table, row []any) ([]any, error)
}
```

```go
faker, _ := pgfake.New(pgfake.Options{Key: key, Rules: rules})

syncer := pgsync.New(source, destination,
    pgsync.WithRowTransformer(faker),
)
```

pgsync moves rows. pgfake transforms values. Neither does the other's job.

## Transformers

Each row is `pgfake value --type <name> --value <input>` with
`PGFAKE_KEY=example-key` and no scope.

| Name | Produces | Real | Fake |
|---|---|---|---|
| `email` | `user_<20 hex>@example.test` | `alex@gmail.com` | `user_118a4132eb203e16e2c7@example.test` |
| `username` | `<first name>_<16 hex>` | `alexj` | `gabriel_7ec57aa5a6db859b` |
| `first_name` | dictionary first name | `Alex` | `Eleanor` |
| `last_name` | dictionary last name | `Johnson` | `Henry` |
| `full_name` | `<first> <last>` | `Alex Johnson` | `Francis Freeman` |
| `company` | `<name> <suffix>` | `Acme Inc.` | `Fairview Technologies` |
| `phone` | same format, new digits | `+44 20 7946 0958` | `+44 93 2588 3307` |
| `uuid` | version-4-shaped UUID | `550e8400-e29b-41d4-a716-446655440000` | `c8b83e72-1706-4613-bc7d-31958858c7c4` |
| `ip` | `10.0.0.0/8` or `2001:db8::/32` | `203.0.113.7` | `10.98.66.235` |
| `date` | shifted by up to ±`days` | `1987-04-12` | `1987-12-23` |
| `token` | 32 hex digits | `sk_live_Hs83hdKq02LmNoPq` | `d6eeb90e9ecdda41f9370ef15ff8e4dd` |
| `secret` | 40 base62 characters | `sk_live_Hs83hdKq02LmNoPq` | `lNwJKLWhVGuATuXv1Zm4m1Y7GyO0zNnmy1FuJP2z` |
| `string` | letter → letter, digit → digit | `AB-1234 xy` | `JB-4011 yc` |
| `hash` | hex digest, or an integer / UUID for those columns | `42` (`bigint`) | `7807322984712449977` |
| `json` | rules applied to paths in a document | see [JSON](#json--jsonb) | |
| `copy` | the value, unchanged | `42` | `42` |
| `null` | `NULL` | `anything` | `NULL` |

### Rule options

| Go field | JSON | Applies to | Meaning |
|---|---|---|---|
| `Scope` | `scope` | all | which values share a fake identity — see [scopes](#relationship-scopes) |
| `Unique` | `unique` | names, company, phone | collision-free output — see [unique columns](#unique-columns) |
| `PreserveDomain` | `preserve_domain` | `email` | keep the domain: `alex@gmail.com` → `user_6dda…c2bc@gmail.com` |
| `PreserveLength` | `preserve_length` | `email`, `hash`, `token`, `secret` | output as long as the input |
| `Length` | `length` | `hash`, `string`, `token`, `secret` | fixed output length |
| `MaxLength` | `max_length` | text output | cap in characters; defaults to the `n` of `varchar(n)` |
| `Days` | `days` | `date` | half-width of the shift window (default 365) |
| `Paths` | `paths` | `json` | dotted path → rule |

### What each one promises

<details>
<summary><b>email</b> — always valid, never deliverable</summary>

The source local part is never used, so malformed input (`"not-an-email"`,
`"@"`, 1 MB of garbage) still yields a valid address. The default domain
`example.test` is reserved by RFC 6761 and cannot receive mail. The 20 hex
digits are 80 bits: collisions stay negligible into the billions of rows.

`preserve_domain` keeps the domain only when it is a plain ASCII host name.
`preserve_length` matches the input length where a valid address allows it —
validity wins, so the local part is clamped to 1–64 characters. In a
`varchar(n)` column the local part is shortened to fit.

</details>

<details>
<summary><b>phone</b> — keeps the format, replaces the digits</summary>

```text
+1 310 555 1234      →  +1 555 619 0666      country code kept, area code 555
(310) 555-1234       →  (555) 138-9150       punctuation kept
+44 20 7946 0958     →  +44 93 2588 3307     country code kept
030 1234567          →  012 4772247          trunk zero kept
310.555.1234 x42     →  555.053.5988 x09     extension replaced too
call me maybe        →  +15559394335         not a number: safe fallback
```

(No scope here, so the digits differ from the `users.phone` row at the top.)
North American numbers get the unassigned area code `555`. Vanity letters
(`1-800-FLOWERS`) are treated as malformed rather than partially preserved.
Outside North America there is no reserved range, so a fake number can
coincide with a real one.

</details>

<details>
<summary><b>token / secret</b> — nothing of the source survives</summary>

No prefix, no suffix, no length unless you ask for it with `preserve_length`.
`sk_live_…` does not come out as `sk_live_…`. pgfake has no partial-masking
transformer on purpose.

</details>

<details>
<summary><b>string</b> — format-preserving</summary>

Letters become letters of the same case, digits become digits, and
punctuation, spacing and length stay: `AB-1234 xy` → `JB-4011 yc`. Good for
licence plates, SKUs and postal codes. Use `token` when the shape itself is
sensitive.

</details>

<details>
<summary><b>date</b> — a bounded, deterministic shift</summary>

Dates move by whole days and timestamps by whole seconds, within ±`days`.
Layout, fractional seconds and time zone are kept
(`2024-01-15 10:30:00+00` → `2024-12-17 16:08:36+00`); `infinity` is left
alone. Because the shift is bounded, the fake stays near the real date — an
age stays roughly an age. Widen `days` if that is too close for you.

</details>

<details>
<summary><b>ip</b> — never routable</summary>

IPv4 lands in `10.0.0.0/8`, IPv6 in the documentation range `2001:db8::/32`.
A `/prefix` suffix is kept, and on a `cidr` column the host bits are cleared
so the result is still a valid network.

</details>

<details>
<summary><b>hash</b> — the type-aware pseudonym</summary>

On text it is the raw 64-digit HMAC. On a `uuid` column it is a UUID. On
integer columns it is a keyed permutation of the integer range, not a
truncated digest:

```console
$ for id in 1 2 3 42; do pgfake value --type hash --pgtype integer --scope user_id --value $id; done
1048585139
57343698
1910878362
1341585551
```

Distinct ids always map to distinct ids, the sign is kept, and the result
always fits the column (`smallint`, `integer` or `bigint`). This is how you
mask an id that is a primary key: give the key and every reference to it the
same scope.

</details>

## Relationship scopes

The scope decides which values share a fake identity.

| | Scope used | Effect |
|---|---|---|
| **No `scope`** | the column itself: `schema.table.column` | unrelated columns never share identities by accident |
| **Explicit `scope`** | the string you gave, verbatim | every column with that scope maps equal values to the same fake |

```json
{
  "users.email":           { "transformer": "email", "scope": "user_email" },
  "orders.customer_email": { "transformer": "email", "scope": "user_email" },
  "audit_logs.user_email": { "transformer": "email", "scope": "user_email" }
}
```

```text
users.email             alex@gmail.com  →  user_6ddae4ca594de840c2bc@example.test
orders.customer_email   alex@gmail.com  →  user_6ddae4ca594de840c2bc@example.test
audit_logs.user_email   alex@gmail.com  →  user_6ddae4ca594de840c2bc@example.test

billing.contact_email   alex@gmail.com  →  user_4b68434d00f72875e6a9@example.test   (scope: billing_email)
```

With an explicit scope, table and column names are not part of the seed at
all. Without one, the schema defaults to `public` and JSON paths are appended
(`public.users.profile.settings.phone`). Scopes are per transformer: `email`
and `username` under the same scope are independent.

## Unique columns

Large output domains do not need help: `email` (80 bits), `uuid` (122 bits),
`username`, `hash`, `token` and `secret` collide only with negligible
probability, and `hash` on integers cannot collide at all.

Dictionaries are finite — 200 first names × 200 last names — so names repeat.
For a column with a unique constraint, set `Unique: true`:

| Transformer | With `Unique` |
|---|---|
| `first_name`, `last_name`, `full_name`, `company` | appends a 48-bit digest suffix: `Derek Harrison-c5b7943b5ad8` |
| `phone` | national digits go through a keyed permutation: same-format numbers never collide, but leave the `555` range |
| `email`, `username`, `uuid`, `hash`, `token`, `secret` | accepted; nothing to change |
| `string`, `ip`, `date` | rejected with `ErrInvalidRule` — their output domain cannot be widened |

No state is kept to enforce any of this. The suffix makes collisions
improbable, not impossible: across a million rows the chance of any collision
is about 1 in 20 million for `full_name` and about 1 in 100,000 for a lone
`first_name` or `last_name`. The phone and integer permutations make
collisions impossible. A `MaxLength` shorter than the
suffixed value cuts the suffix and the guarantee with it.

## PostgreSQL types

pgfake never talks to PostgreSQL, but `PGType` tells it what the result must
stay valid for. It accepts the spellings of `format_type()` and
`pg_type.typname`: `character varying(64)`, `int4`, `timestamp with time
zone`, `text[]`, `_text`, …

| PostgreSQL type | Go values accepted | Transformers |
|---|---|---|
| `text`, `varchar`, `char`, `citext`, unknown types | `string`, `[]byte` | every text transformer |
| `smallint`, `integer`, `bigint` | any Go integer, `json.Number`, text form | `hash` |
| `uuid` | `[16]byte`, text form | `uuid`, `hash` |
| `date`, `timestamp`, `timestamptz` | `time.Time`, text form | `date` |
| `inet`, `cidr` | `netip.Addr`, `netip.Prefix`, `net.IP`, text form | `ip` |
| `json`, `jsonb` | `string`, `[]byte`, `json.RawMessage`, `map[string]any`, `[]any` | `json` |
| `boolean`, `numeric`, floats | any | `copy`, `null` |
| arrays | `[]string`, `[]any` — element by element | by element type |

- **Wrong type is an error, not bad data.**
  `email` on an `integer` column fails with `ErrTypeMismatch`.
- **`varchar(n)` is honored.** Free-form output is cut to fit, emails get a
  shorter local part, and what cannot shrink (a UUID into `varchar(10)`) fails
  with `ErrValueTooLong`.
- **`citext` compares like PostgreSQL.** `Alex@Gmail.com` and `alex@gmail.com`
  get the same fake in a `citext` column, different fakes in a `text` column.
- **Text and native forms agree.** `"42"` in a `bigint` column and `int64(42)`
  produce the same fake id.
- **Not yet:** array literals in text form (`{a,b}`) are refused in v0.1; pass
  a Go slice.

## JSON / JSONB

```go
pgfake.Rule{Transformer: "json", Paths: map[string]pgfake.Rule{
    "name":           {Transformer: "first_name"},
    "email":          {Transformer: "email", Scope: "user_email"},
    "settings.phone": {Transformer: "phone"},
}}
```

```text
{"name": "Alex", "settings": {"phone": "+13105551234", "theme": "dark"}}
                                  ↓
{"name":"Donna","settings":{"phone":"+15559535033","theme":"dark"}}
```

Paths are plain dotted keys; there is no query language. Arrays along the way
are traversed, so `contacts.email` reaches every element of `contacts`.
Missing paths are ignored, `null` stays `null`, and everything not named by a
path is kept. Text documents are re-encoded, which normalizes whitespace and
key order the way `jsonb` does; numbers keep their exact digits.

## CLI

For trying rules and debugging — not for moving data. The key is read from an
environment variable and is never accepted as a flag.

| Command | Does |
|---|---|
| `pgfake value --type T [--value V]` | transform one value, or one per line from stdin |
| `pgfake json --config FILE [--table T]` | transform a stream of JSON objects |
| `pgfake list` | print the transformer names |
| `pgfake version` | print the library and algorithm version |

| `value` flag | |
|---|---|
| `--type` | transformer name (required) |
| `--value` | the value; omit to read stdin line by line |
| `--scope` | relationship scope |
| `--schema`, `--table`, `--column` | where the value lives (the default scope) |
| `--pgtype` | PostgreSQL type, default text |
| `--unique`, `--preserve-domain`, `--preserve-length` | rule options |
| `--length`, `--max-length`, `--days` | rule options |
| `--key-env` | environment variable holding the key (default `PGFAKE_KEY`) |

```console
$ pgfake value --type email --scope user_email --preserve-domain --value alex@gmail.com
user_6ddae4ca594de840c2bc@gmail.com

$ printf 'alex@gmail.com\nsam@corp.example\nalex@gmail.com\n' | pgfake value --type email --scope user_email
user_6ddae4ca594de840c2bc@example.test
user_4a5934d3df15d427865e@example.test
user_6ddae4ca594de840c2bc@example.test

$ pgfake value --type email --pgtype integer --value 42
pgfake: email: transformer does not support this type: "email" cannot produce integer values
```

`pgfake json` treats each top-level key as a column of one table (`--table`,
inferred when the config mentions only one) and writes one object per line,
keys in their original order:

```json
{
  "key_env": "PGFAKE_KEY",
  "rules": {
    "users.name":    { "transformer": "full_name" },
    "users.email":   { "transformer": "email", "scope": "user_email" },
    "users.phone":   { "transformer": "phone" },
    "users.company": { "transformer": "company" }
  }
}
```

```console
$ cat users.ndjson
{"id":42,"company_id":7,"name":"Alex Johnson","email":"alex@gmail.com","phone":"+1 310 555 1234","company":"Acme Inc.","deleted_at":null}
{"id":43,"company_id":7,"name":"Sam Lee","email":"sam@corp.example","phone":"(415) 555-0199","company":"Acme Inc.","deleted_at":null}

$ pgfake json --config pgfake.json < users.ndjson
{"id":42,"company_id":7,"name":"Derek Harrison","email":"user_6ddae4ca594de840c2bc@example.test","phone":"+1 555 621 5485","company":"Timber Media","deleted_at":null}
{"id":43,"company_id":7,"name":"Joel Hamilton","email":"user_4a5934d3df15d427865e@example.test","phone":"(555) 375-1281","company":"Timber Media","deleted_at":null}
```

Ids, foreign keys and `NULL`s are untouched; both rows still belong to the
same (fake) company. The key never goes in the config file — only the name of
the variable that holds it. A fuller config is in
[`pgfake.example.json`](pgfake.example.json).

## Safety

**What pgfake does**

- Copies the key into the `Faker` and never emits it. Printing a `Faker` or
  `Options` with `%v`, `%+v` or `%#v` does not reveal it.
- Has no logging at all.
- Returns typed errors built from configuration only — transformer, schema,
  table, column, JSON path, Go and PostgreSQL type names. Never the value.
- Does not panic on malformed data; the suite includes a fuzz target.

```go
var perr *pgfake.Error
if errors.As(err, &perr) {
    log.Printf("masking failed for %s.%s", perr.Table, perr.Column) // safe to log
}
if errors.Is(err, pgfake.ErrTypeMismatch) { /* ... */ }
```

`ErrNoKey` · `ErrUnknownTransformer` · `ErrInvalidRule` ·
`ErrUnsupportedValue` · `ErrTypeMismatch` · `ErrInvalidInput` ·
`ErrInvalidJSON` · `ErrValueTooLong` · `ErrRowShape`

**What pgfake does not protect against**

- **A leaked key.** Whoever holds the key can test guesses (*"is this fake
  address `alex@gmail.com`?"*). Treat it like a production secret, use 32+
  random bytes, never commit it.
- **Structure you chose to keep.** Unmasked columns, preserved domains, string
  layout, value lengths and shifted dates all remain visible. Determinism
  means equal inputs are visibly equal — that is the feature, and also a
  frequency side channel on low-cardinality columns.
- **Free text.** pgfake does not look for PII inside notes, comments or
  documents. Use `null`, or `json` paths for the structured parts.

## Compatibility: algorithm v1

Golden databases built on different days must not drift apart, so outputs are
a compatibility contract.

- [`testdata/vectors.json`](testdata/vectors.json) pins the exact output of
  84 cases covering every transformer and option. Changing one is a bug.
- The embedded dictionaries are frozen; a test pins their checksums.
- The algorithm version is part of every HMAC input. A new algorithm would
  ship as `v2` next to `v1`, never as a silent replacement.

<details>
<summary><b>The construction</b>, should you need to reproduce it elsewhere</summary>

```text
ns      = "pgfake:v1:" + transformer + ":" + scope
block 0 = HMAC-SHA256(key, ns ‖ 0x00 ‖ value)
block i = HMAC-SHA256(key, ns ‖ 0x02 ‖ block 0 ‖ uint32be(i))        i ≥ 1
stream  = block 0 ‖ block 1 ‖ …
```

`value` is the text form of the input (lowercased for `citext`). Each
transformer reads the stream in a fixed order: `email` is `user_` plus the hex
of the first 10 bytes; `uuid` is the first 16 bytes with the version and
variant bits set; `hash` on text is the hex of block 0.

```console
$ printf 'pgfake:v1:email:user_email\0alex@gmail.com' \
    | openssl dgst -sha256 -hmac example-key -r | cut -c1-20
6ddae4ca594de840c2bc
```

Integer `hash` and `phone` with `Unique` use a 10-round alternating Feistel
network over `[0,a) × [0,b)` with round function

```text
F(round, half) = uint64be(HMAC-SHA256(key,
    ns ‖ 0x01 ‖ byte(round) ‖ uint64be(a) ‖ uint64be(b) ‖ uint64be(half))[:8])
```

where `a·b` is the size of the domain: `2^7·2^8` for `smallint`, `2^15·2^16`
for `integer`, `2^31·2^32` for `bigint`, `10^⌊n/2⌋·10^⌈n/2⌉` for `n` phone
digits. The separator bytes `0x00`, `0x01` and `0x02` cannot occur in `ns`, so
the three message families never collide.

</details>

## Benchmarks

Apple M1 Pro, Go 1.27, `go test -bench . -run '^$'`:

```text
email            326 ns/op    112 B/op    3 allocs/op
full_name        349 ns/op     32 B/op    2 allocs/op
phone            373 ns/op     32 B/op    2 allocs/op
uuid             354 ns/op     64 B/op    2 allocs/op
ip               326 ns/op     32 B/op    2 allocs/op
date             485 ns/op     32 B/op    2 allocs/op
token            330 ns/op     48 B/op    2 allocs/op
secret           636 ns/op    112 B/op    3 allocs/op
hash (text)      389 ns/op    144 B/op    3 allocs/op
hash (bigint)   1839 ns/op     32 B/op    2 allocs/op    keyed permutation, 10 HMAC rounds

row, 4 masked columns of 7    1.6 µs    one core
same, 10 goroutines           0.37 µs   per row
```

One HMAC-SHA256 per value is the floor; the rest is formatting.

## Tests

```bash
go test ./...
go test -race ./...
go vet ./...
go test -fuzz FuzzTransform -fuzztime 30s .
```

What the suite proves, each as its own test:

→ same input + same key = same output, across separate `Faker` instances  
→ different key, scope or transformer = different output  
→ same scope across tables and schemas = same output  
→ `NULL` and empty values are preserved by every transformer  
→ UTF-8, 1 MB inputs, malformed emails / phones / dates / UUIDs / JSON  
→ every output is valid for its type  
→ unique mode: suffixes, and exhaustive bijection checks for integers and phones  
→ 16 goroutines in different orders agree with a serial run (`-race` clean)  
→ input buffers, slices and maps are never modified or aliased  
→ no error message ever contains the input or the key  
→ 84 golden vectors, plus the HMAC construction checked against plain `crypto/hmac`

CI runs it on Go 1.22 and the current stable release.

```text
pgfake.go  options.go  rule.go  context.go  errors.go   public API
value.go   pgtype.go   json.go                          Go and PostgreSQL types
internal/deterministic/                                 HMAC stream, keyed permutation
internal/transformers/                                  one pure function per transformer
internal/datasets/                                      frozen name dictionaries
cmd/pgfake/                                             CLI
testdata/vectors.json                                   compatibility vectors
```

## Non-goals

No CDC.  
No WAL or logical replication.  
No `pg_dump` / `pg_restore` replacement.  
No server, scheduler or storage.  
No database subsetting or schema migrations.  
No automatic PII detection.

Those belong to the tools that embed pgfake.

> **Your sync tool moves rows. pgfake makes them safe.**

## Prior art

pgfake borrows ideas from, and is much smaller than,
[Greenmask](https://github.com/GreenmaskIO/greenmask) (deterministic engines,
type-aware validation), [Neosync](https://github.com/nucleuscloud/neosync)
(transformer catalog), [pgstream](https://github.com/xataio/pgstream)
(transformation kept apart from replication) and
[go-masker](https://github.com/ggwhite/go-masker).

## License

[MIT](LICENSE)
