// Command pgfake is a small demo and debugging front end for the pgfake
// library. It transforms single values or JSON objects; it does not connect
// to databases.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/pgrundev/pgfake"
)

const usage = `pgfake — Real data in. Safe fake data out.

Usage:
  pgfake value --type <transformer> [--value <v>] [options]
  pgfake json  --config <file> [--table <name>] [--schema <name>]
  pgfake list
  pgfake version

The secret key is read from the environment variable PGFAKE_KEY
(override the variable name with --key-env or "key_env" in the config).

Run "pgfake <command> -h" for the options of a command.
`

const defaultKeyEnv = "PGFAKE_KEY"

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return nil
	}
	switch args[0] {
	case "value":
		return runValue(args[1:], stdin, stdout)
	case "json":
		return runJSON(args[1:], stdin, stdout)
	case "list":
		for _, name := range pgfake.Transformers() {
			fmt.Fprintln(stdout, name)
		}
		return nil
	case "version":
		fmt.Fprintf(stdout, "pgfake %s (algorithm %s)\n", pgfake.Version, pgfake.AlgorithmVersion)
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	}
	return fmt.Errorf("pgfake: unknown command %q\n\n%s", args[0], usage)
}

func loadKey(env string) ([]byte, error) {
	if env == "" {
		env = defaultKeyEnv
	}
	key := os.Getenv(env)
	if key == "" {
		return nil, fmt.Errorf("pgfake: environment variable %s is not set", env)
	}
	return []byte(key), nil
}

func runValue(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("pgfake value", flag.ContinueOnError)
	var (
		rule   pgfake.Rule
		vc     pgfake.ValueContext
		value  string
		keyEnv string
	)
	fs.StringVar(&rule.Transformer, "type", "", "transformer name (see \"pgfake list\")")
	fs.StringVar(&value, "value", "", "value to transform; without it, one value per line is read from stdin")
	fs.StringVar(&rule.Scope, "scope", "", "relationship scope")
	fs.BoolVar(&rule.Unique, "unique", false, "request collision-free output")
	fs.BoolVar(&rule.PreserveDomain, "preserve-domain", false, "email: keep the domain")
	fs.BoolVar(&rule.PreserveLength, "preserve-length", false, "keep the input length")
	fs.IntVar(&rule.Length, "length", 0, "fixed output length")
	fs.IntVar(&rule.MaxLength, "max-length", 0, "maximum output length")
	fs.IntVar(&rule.Days, "days", 0, "date: shift window in days (default 365)")
	fs.StringVar(&vc.Schema, "schema", "", "schema the value belongs to")
	fs.StringVar(&vc.Table, "table", "", "table the value belongs to")
	fs.StringVar(&vc.Column, "column", "", "column the value belongs to")
	fs.StringVar(&vc.PGType, "pgtype", "", "PostgreSQL type of the value (default text)")
	fs.StringVar(&keyEnv, "key-env", defaultKeyEnv, "environment variable holding the secret key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if rule.Transformer == "" {
		return errors.New("pgfake value: --type is required")
	}
	key, err := loadKey(keyEnv)
	if err != nil {
		return err
	}
	faker, err := pgfake.New(pgfake.Options{Key: key})
	if err != nil {
		return err
	}

	one := func(v string) error {
		out, err := faker.Transform(vc, v, rule)
		if err != nil {
			return err
		}
		if out == nil {
			out = "NULL"
		}
		_, err = fmt.Fprintln(stdout, out)
		return err
	}

	hasValue := false
	fs.Visit(func(f *flag.Flag) { hasValue = hasValue || f.Name == "value" })
	if hasValue {
		return one(value)
	}
	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		if err := one(sc.Text()); err != nil {
			return err
		}
	}
	return sc.Err()
}

// config is the CLI's JSON configuration file.
type config struct {
	KeyEnv string                 `json:"key_env"`
	Rules  map[string]pgfake.Rule `json:"rules"`
}

func loadConfig(path string) (*config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pgfake: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var cfg config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("pgfake: %s: %w", path, err)
	}
	return &cfg, nil
}

// soleTable returns the table all rules refer to, if there is exactly one.
func (c *config) soleTable() (string, bool) {
	tables := map[string]bool{}
	for key := range c.Rules {
		parts := strings.Split(key, ".")
		if len(parts) < 2 {
			continue
		}
		tables[parts[len(parts)-2]] = true
	}
	if len(tables) != 1 {
		return "", false
	}
	for t := range tables {
		return t, true
	}
	return "", false
}

func runJSON(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("pgfake json", flag.ContinueOnError)
	var cfgPath, table, schema string
	fs.StringVar(&cfgPath, "config", "", "JSON configuration file")
	fs.StringVar(&table, "table", "", "table whose rules apply to the input objects")
	fs.StringVar(&schema, "schema", "", "schema of the table (default public)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfgPath == "" {
		return errors.New("pgfake json: --config is required")
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	if table == "" {
		t, ok := cfg.soleTable()
		if !ok {
			var names []string
			for key := range cfg.Rules {
				names = append(names, key)
			}
			sort.Strings(names)
			return fmt.Errorf("pgfake json: --table is required (rules: %s)", strings.Join(names, ", "))
		}
		table = t
	}
	key, err := loadKey(cfg.KeyEnv)
	if err != nil {
		return err
	}
	faker, err := pgfake.New(pgfake.Options{Key: key, Rules: cfg.Rules})
	if err != nil {
		return err
	}

	dec := json.NewDecoder(stdin)
	dec.UseNumber()
	out := bufio.NewWriter(stdout)
	defer out.Flush()
	for n := 1; ; n++ {
		names, values, err := readObject(dec)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			// The decoder's own message may quote input, so it is not shown.
			return fmt.Errorf("pgfake json: input object %d is not a valid JSON object", n)
		}
		t := pgfake.Table{Schema: schema, Name: table, Columns: make([]pgfake.Column, len(names))}
		for i, name := range names {
			t.Columns[i].Name = name
		}
		row, err := faker.TransformRow(t, values)
		if err != nil {
			return err
		}
		if err := writeObject(out, names, row); err != nil {
			return err
		}
	}
}

// readObject reads one JSON object, keeping the order of its keys.
func readObject(dec *json.Decoder) (names []string, values []any, err error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, nil, errors.New("not an object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, errors.New("bad key")
		}
		name, ok := tok.(string)
		if !ok {
			return nil, nil, errors.New("bad key")
		}
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, nil, errors.New("bad value")
		}
		names = append(names, name)
		values = append(values, v)
	}
	if _, err := dec.Token(); err != nil {
		return nil, nil, errors.New("unterminated object")
	}
	return names, values, nil
}

func writeObject(w *bufio.Writer, names []string, values []any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	encode := func(v any) error {
		buf.Reset()
		if err := enc.Encode(v); err != nil {
			return err
		}
		_, err := w.Write(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
		return err
	}
	w.WriteByte('{')
	for i, name := range names {
		if i > 0 {
			w.WriteByte(',')
		}
		if err := encode(name); err != nil {
			return err
		}
		w.WriteByte(':')
		if err := encode(values[i]); err != nil {
			return fmt.Errorf("pgfake json: cannot encode column %q", name)
		}
	}
	w.WriteString("}\n")
	return w.Flush()
}
