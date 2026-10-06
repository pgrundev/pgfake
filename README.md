# pgfake — Real data in. Safe fake data out.

Deterministic data masking for PostgreSQL.

```text
REAL DATA                         FAKE DATA

id        42                 →    42
name      Alex Johnson       →    Derek Harrison
email     alex@gmail.com     →    user_6ddae4ca594de840c2bc@example.test
phone     +1 310 555 1234    →    +1 555 621 5485
company   Acme Inc.          →    Timber Media
```

Same schema. Same relationships. No real customer data.

`Go · zero deps · deterministic`

pgfake is a small Go library (plus a tiny CLI) that turns production values
into realistic fake ones. It is a transformation engine only: it does not
connect to databases, copy tables, or replicate anything. Something else moves
the rows; pgfake makes them safe.

```text
real data → pgfake → safe fake data
```

## Why deterministic

Every fake value is derived with HMAC-SHA256 from exactly four things:

```text
secret key + transformer + relationship scope + original value
```

There is no mapping table and no state. The same input gives the same output
tomorrow, on another server, in another goroutine, in another table, for as
long as the key and the rule are the same. A different key gives a completely
different, unrelated data set.

That is what keeps a masked database usable: `alex@gmail.com` becomes the same
fake address in `users`, `orders` and `audit_logs`, so joins, lookups and
uniqueness still work, and a database refreshed next week lines up with the
one from last week.

## Install

```bash
go get github.com/pgrundev/pgfake            # library
go install github.com/pgrundev/pgfake/cmd/pgfake@latest   # CLI
```

Requires Go 1.22+. No dependencies outside the standard library.

## Library

```go
faker, err := pgfake.New(pgfake.Options{Key: key}) // key: 32+ random bytes

fake, err := faker.Transform(
    pgfake.ValueContext{Schema: "public", Table: "users", Column: "email", PGType: "text"},
    "alex@gmail.com",
    pgfake.Rule{Transformer: "email", Scope: "user_email"},
)
// fake == "user_6ddae4ca594de840c2bc@example.test"   (with Key "example-key")
```

Whole rows, with the rules configured once:

```go
faker, err := pgfake.New(pgfake.Options{
    Key: key,
    Rules: map[string]pgfake.Rule{
        "users.name":            {Transformer: "full_name"},
        "users.email":           {Transformer: "email", Scope: "user_email"},
        "users.phone":           {Transformer: "phone"},
        "orders.customer_email": {Transformer: "email", Scope: "user_email"},
    },
})

users := pgfake.Table{Name: "users", Columns: []pgfake.Column{
    {Name: "id", PGType: "bigint"},
    {Name: "name", PGType: "text"},
    {Name: "email", PGType: "text"},
    {Name: "phone", PGType: "text"},
}}

fakeRow, err := faker.TransformRow(users, []any{int64(42), "Alex Johnson", "alex@gmail.com", "+1 310 555 1234"})
// [42 "Derek Harrison" "user_6ddae4ca594de840c2bc@example.test" "+1 555 621 5485"]
```

The whole public API is `New`, `Faker.Transform`, `Faker.TransformRow`,
`Transformers`, and the types `Options`, `Rule`, `ValueContext`, `Table`,
`Column`, `Error`.

- A `Faker` is immutable and safe for concurrent use; results do not depend on
  call order.
- `NULL` (`nil`) is always preserved. So is the empty string.
- Columns without a rule are passed through unchanged, so primary and foreign
  keys stay intact unless you ask otherwise.
- The input is never modified; `[]byte` in gives a fresh `[]byte` out.
- On error nothing is returned: `TransformRow` yields a `nil` row. Drop the
  row; never fall back to the original.

### Embedding (pgsync and friends)

Consumers only need one of two interfaces, both implemented by `*Faker`:

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
syncer := pgsync.New(source, destination, pgsync.WithRowTransformer(faker))
```

pgsync moves rows. pgfake transforms values.

## Transformers

| Name | Output | Example (`Key: "example-key"`) |
|---|---|---|
| `copy` | the value, unchanged | `42` → `42` |
| `null` | `NULL` | `anything` → `NULL` |
| `hash` | text: 64 hex digits · integer: an integer of the same width · uuid: a UUID | `42` (bigint) → `4541185980144728689` |
| `string` | letters → letters (same case), digits → digits, everything else kept | `AB-1234 xy` → `JB-4011 yc` |
| `email` | `user_<20 hex>@example.test` | `alex@gmail.com` → `user_118a4132eb203e16e2c7@example.test` |
| `username` | `<first name>_<16 hex>` | `alexj` → `gabriel_7ec57aa5a6db859b` |
| `first_name` | dictionary first name | `Alex` → `Eleanor` |
| `last_name` | dictionary last name | `Johnson` → `Henry` |
| `full_name` | `<first> <last>` | `Alex Johnson` → `Francis Freeman` |
| `phone` | same format, new digits | `+44 20 7946 0958` → `+44 93 2588 3307` |
| `company` | `<name> <suffix>` | `Acme Inc.` → `Fairview Technologies` |
| `uuid` | version-4-shaped UUID | `550e8400-…` → `c8b83e72-1706-4613-bc7d-31958858c7c4` |
| `ip` | IPv4 in `10.0.0.0/8`, IPv6 in `2001:db8::/32` | `203.0.113.7` → `10.98.66.235` |
| `date` | shifted by up to ±`days` (default 365) | `1987-04-12` → `1987-12-23` |
| `token` | 32 hex digits | `sk_live_Hs83…` → `d6eeb90e9ecdda41f9370ef15ff8e4dd` |
| `secret` | 40 base62 characters | `sk_live_Hs83…` → `lNwJKLWhVGuATuXv1Zm4…` |
| `json` | applies rules to paths inside a JSON document | see [JSON](#json--jsonb) |

Each example is `pgfake value --type <name> --value <input>` with no scope;
your output depends on your key and scope.

### Rule options

| Field (JSON) | Applies to | Meaning |
|---|---|---|
| `Scope` (`scope`) | all | relationship scope, see below |
| `Unique` (`unique`) | names, company, phone | collision-free output, see below |
| `PreserveDomain` (`preserve_domain`) | email | keep the domain: `alex@gmail.com` → `user_6dda…@gmail.com` |
| `PreserveLength` (`preserve_length`) | email, hash, token, secret | output as long as the input |
| `Length` (`length`) | hash, string, token, secret | fixed output length |
| `MaxLength` (`max_length`) | text output | cap in characters; defaults to the `n` of `varchar(n)` / `char(n)` |
| `Days` (`days`) | date | half-width of the shift window |
| `Paths` (`paths`) | json | dotted path → rule |

Notes on behavior:

- **email** never uses the source local part, so malformed input still yields
  a valid address. The default domain `example.test` is reserved and cannot
  receive mail. `preserve_length` keeps the address valid first: the local
  part is clamped to 1–64 characters. `preserve_domain` only keeps plain ASCII
  host names.
- **phone** keeps punctuation, spacing, a leading `+<country code>` and trunk
  zeros. North American numbers get the unassigned area code `555`. Text that
  is not a phone number (`"call me"`, vanity letters) becomes `+1555xxxxxxx`.
  Outside North America there are no reserved ranges, so a fake number can
  coincide with a real one.
- **token / secret** never carry any part of the source — no `sk_live_`
  prefix survives. pgfake has no partial-masking transformer.
- **string** deliberately keeps punctuation and layout (`AB-1234` stays
  shaped like a licence plate); use `token` when the shape itself is
  sensitive.
- **date** keeps the input's layout, fraction and time zone; `infinity` is
  left alone. Because the shift is bounded, the output stays close to the
  real date (an age stays roughly an age). Widen `days` if that matters.
- **ip** on a `cidr` column clears the host bits so the result stays a valid
  network.
- **hash on integers** is a keyed permutation of the integer range, not a
  truncated digest: distinct ids always map to distinct ids, the sign is kept,
  and the result always fits the column (`smallint` / `integer` / `bigint`).

## Relationship scopes

The scope decides which values share a fake identity.

- **No scope:** the scope is the value's own column,
  `schema.table.column` (schema defaults to `public`; JSON paths are
  appended). Unrelated columns never share identities by accident.
- **Explicit scope:** used verbatim. Table and column names no longer
  matter, so any number of columns can join the same relationship:

```json
{
  "users.email":           { "transformer": "email", "scope": "user_email" },
  "orders.customer_email": { "transformer": "email", "scope": "user_email" },
  "audit_logs.user_email": { "transformer": "email", "scope": "user_email" }
}
```

`alex@gmail.com` is now the same fake address in all three columns, with or
without a foreign key between them. The same trick covers natural keys: if an
email (or an integer id run through `hash`) is a primary key, give the key and
every reference to it one scope.

Scopes are per transformer: `email` and `username` under the same scope are
independent.

## Unique columns

Outputs with a large domain — `email` (80 bits), `uuid` (122), `username`,
`hash`, `token`, `secret` — collide only with negligible probability, and
`hash` on integers cannot collide at all.

Dictionaries are finite (200 first names × 200 last names), so names repeat.
For columns with a unique constraint set `Unique: true`:

| Transformer | With `Unique` |
|---|---|
| `first_name`, `last_name`, `full_name`, `company` | a 48-bit digest suffix is appended: `Derek Harrison-c5b7943b5ad8` |
| `phone` | the national digits go through a keyed permutation: numbers of the same format never collide, but leave the `555` range |
| `email`, `username`, `uuid`, `hash`, `token`, `secret` | accepted, no change needed |
| `string`, `ip`, `date` | rejected with `ErrInvalidRule` — their output domain cannot be widened |

No state is kept to enforce uniqueness. The suffix makes collisions
improbable, not impossible (about 1 in 10⁸ for a million rows); the phone and
integer permutations make them impossible. A `MaxLength` shorter than the
suffixed value cuts the suffix and the guarantee with it.

## PostgreSQL types

pgfake never talks to PostgreSQL, but `PGType` tells it what the result must
remain valid for. It accepts the names from `format_type()` and
`pg_type.typname` (`character varying(64)`, `int4`, `timestamp with time
zone`, `text[]`, `_text`, …).

| PostgreSQL type | Go values accepted | Transformers |
|---|---|---|
| `text`, `varchar`, `char`, `citext`, unknown types | `string`, `[]byte` | all text transformers |
| `smallint`, `integer`, `bigint` | any Go integer, `json.Number`, text form | `hash` |
| `uuid` | `[16]byte`, text form | `uuid`, `hash` |
| `date`, `timestamp`, `timestamptz` | `time.Time`, text form | `date` |
| `inet`, `cidr` | `netip.Addr`, `netip.Prefix`, `net.IP`, text form | `ip` |
| `json`, `jsonb` | `string`, `[]byte`, `json.RawMessage`, `map[string]any`, `[]any` | `json` |
| `boolean`, `numeric`, floats | any | `copy`, `null` only |
| arrays | `[]string`, `[]any` (element by element) | by element type |

The result always has the Go type of the input. A transformer that cannot
produce the column's type fails with `ErrTypeMismatch` (for example `email` on
an `integer` column) instead of writing something PostgreSQL would reject.
`varchar(n)` limits are honored: free-form output is cut to fit, emails get a
shorter local part, and values that cannot shrink (a UUID into `varchar(10)`)
fail with `ErrValueTooLong`. `citext` values are compared case-insensitively,
like PostgreSQL does. Array literals in text form (`{a,b}`) are not parsed in
v0.1; pass a Go slice.

The text form and the native Go form of a value agree: `"42"` in a `bigint`
column and `int64(42)` give the same fake id.

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
→
{"name":"Donna","settings":{"phone":"+15559535033","theme":"dark"}}
```

Paths are plain dotted keys. Arrays on the way are traversed, so
`contacts.email` reaches every element of `contacts`. Missing paths are
ignored, `null` stays `null`, and everything not named by a path is kept.
Documents given as text are re-encoded, which normalizes whitespace and key
order the way `jsonb` does; numbers keep their exact digits. There is no query
language.

## CLI

The CLI is for trying rules and debugging, not for moving data. The key comes
from an environment variable and is never accepted as a flag.

```bash
export PGFAKE_KEY="$(openssl rand -hex 32)"

pgfake value --type email --value "alex@gmail.com"
pgfake value --type email --value "alex@gmail.com" --scope user_email --preserve-domain
pgfake value --type hash  --value 42 --pgtype bigint --scope user_id
cut -f3 users.tsv | pgfake value --type full_name --scope user_name   # one value per line

echo '{"id":42,"name":"Alex Johnson","email":"alex@gmail.com"}' |
  pgfake json --config pgfake.json

pgfake list       # transformer names
pgfake version
```

`pgfake json` reads a stream of JSON objects, treats each top-level key as a
column of one table (`--table`, inferred when the config mentions only one)
and writes one object per line, keys in their original order.

```json
{
  "key_env": "PGFAKE_KEY",
  "rules": {
    "users.email": { "transformer": "email", "scope": "user_email" },
    "users.name":  { "transformer": "full_name" },
    "users.phone": { "transformer": "phone" }
  }
}
```

Rule keys are `table.column` (any schema) or `schema.table.column` (wins when
both match). See [`pgfake.example.json`](pgfake.example.json).

## Safety

- The key is copied into the `Faker`, never logged, and printing a `Faker` or
  `Options` with `%v` / `%+v` / `%#v` does not reveal it.
- pgfake has no logging at all.
- Errors are typed (`errors.Is` against `ErrTypeMismatch`, `ErrInvalidInput`,
  `ErrInvalidJSON`, `ErrValueTooLong`, `ErrUnsupportedValue`,
  `ErrUnknownTransformer`, `ErrInvalidRule`, `ErrRowShape`, `ErrNoKey`;
  `errors.As` to `*pgfake.Error` for schema, table, column, JSON path and
  transformer). They are built from configuration only and never contain the
  value being transformed.
- Malformed data does not panic; the test suite includes a fuzz target.

What pgfake does **not** protect against:

- **A leaked key.** Whoever has the key can test guesses ("is this fake
  address `alex@gmail.com`?"). Treat it like a production secret, use 32+
  random bytes, and never commit it.
- **Structure you chose to keep.** Unmasked columns, preserved domains, string
  layout, value lengths, shifted dates and which values are equal all remain
  visible. Determinism means equal inputs are visibly equal — that is the
  feature, and also a frequency side channel for low-cardinality columns.
- **Free text.** pgfake does not find PII inside notes, comments or documents.
  Use `null`, or `json` paths for structured parts.

## Compatibility: algorithm v1

Golden databases built on different days must not drift, so outputs are a
compatibility contract:

- [`testdata/vectors.json`](testdata/vectors.json) pins exact outputs for
  every transformer and option. A release that changes any of them is a bug.
- The embedded dictionaries are frozen; a test pins their checksums.
- The algorithm version is part of every HMAC input. A changed algorithm would
  ship as `v2` next to `v1`, never as a silent replacement.

The construction, should you need to reproduce it elsewhere:

```text
ns      = "pgfake:v1:" + transformer + ":" + scope
block 0 = HMAC-SHA256(key, ns ‖ 0x00 ‖ value)
block i = HMAC-SHA256(key, ns ‖ 0x02 ‖ block 0 ‖ uint32be(i))        i ≥ 1
stream  = block 0 ‖ block 1 ‖ …
```

Each transformer reads the stream in a fixed order: `email` is `user_` plus
the hex of the first 10 bytes; `uuid` is the first 16 bytes with the version
and variant bits set; `hash` on text is the hex of block 0. `value` is the
text form of the input (lowercased for `citext`).

Integer `hash` and `phone` with `Unique` use a 10-round alternating Feistel
network over `[0,a) × [0,b)` with round function

```text
F(round, half) = uint64be(HMAC-SHA256(key,
    ns ‖ 0x01 ‖ byte(round) ‖ uint64be(a) ‖ uint64be(b) ‖ uint64be(half))[:8])
```

where `a·b` is the size of the domain (`2^7·2^8` for `smallint`, `2^15·2^16`
for `integer`, `2^31·2^32` for `bigint`, `10^⌊n/2⌋·10^⌈n/2⌉` for `n` phone
digits).

## Development

```bash
go test ./...
go test -race ./...
go vet ./...
go test -bench . -run '^$'
go test -fuzz FuzzTransform -fuzztime 30s .
```

On an Apple M1 Pro a typical transformation takes about 330 ns and 2–3 small
allocations; a six-column row about 1.6 µs.

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

pgfake will not grow into any of these: CDC, WAL or logical replication, a
`pg_dump` / `pg_restore` replacement, storage, scheduling, a server, database
subsetting, schema migrations, or automatic PII detection. Those belong to the
tools that embed it.

## Prior art

pgfake borrows ideas from, and is much smaller than,
[Greenmask](https://github.com/GreenmaskIO/greenmask) (deterministic engines,
type-aware validation), [Neosync](https://github.com/nucleuscloud/neosync)
(transformer catalog), [pgstream](https://github.com/xataio/pgstream)
(transformation kept apart from replication) and
[go-masker](https://github.com/ggwhite/go-masker).

## License

MIT
