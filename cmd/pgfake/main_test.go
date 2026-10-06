package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(args, strings.NewReader(stdin), &out)
	return out.String(), err
}

func TestValue(t *testing.T) {
	t.Setenv("PGFAKE_KEY", "test-key-1")

	// Matches the golden vector "email, scope email".
	out, err := runCLI(t, "", "value", "--type", "email", "--scope", "email", "--value", "alex@gmail.com")
	if err != nil || out != "user_0a14a603968e522f9d80@example.test\n" {
		t.Fatalf("got %q, %v", out, err)
	}
	out, err = runCLI(t, "", "value", "--type", "email", "--scope", "email", "--value", "alex@gmail.com", "--preserve-domain")
	if err != nil || out != "user_0a14a603968e522f9d80@gmail.com\n" {
		t.Fatalf("got %q, %v", out, err)
	}
	out, err = runCLI(t, "", "value", "--type", "hash", "--scope", "ids", "--pgtype", "integer", "--value", "42")
	if err != nil || out != "1674335515\n" {
		t.Fatalf("got %q, %v", out, err)
	}
	// Values from stdin, one per line.
	out, err = runCLI(t, "alex@gmail.com\nsam@corp.example\nalex@gmail.com\n", "value", "--type", "email", "--scope", "email")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if err != nil || len(lines) != 3 || lines[0] != lines[2] || lines[0] == lines[1] {
		t.Fatalf("got %q, %v", out, err)
	}
	// A different key gives different output.
	t.Setenv("OTHER_KEY", "test-key-2")
	out, err = runCLI(t, "", "value", "--type", "email", "--scope", "email", "--value", "alex@gmail.com", "--key-env", "OTHER_KEY")
	if err != nil || out != "user_e3d839d6614befff4150@example.test\n" {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestValueErrors(t *testing.T) {
	t.Setenv("PGFAKE_KEY", "")
	if _, err := runCLI(t, "", "value", "--type", "email", "--value", "x"); err == nil || !strings.Contains(err.Error(), "PGFAKE_KEY") {
		t.Errorf("missing key: %v", err)
	}
	t.Setenv("PGFAKE_KEY", "KEY-MATERIAL-5f3a9c")
	for _, args := range [][]string{
		{"value", "--value", "x"},
		{"value", "--type", "nope", "--value", "SENSITIVE-9731"},
		{"value", "--type", "email", "--pgtype", "integer", "--value", "9731"},
		{"value", "--type", "hash", "--pgtype", "integer", "--value", "SENSITIVE-9731"},
		{"bogus"},
		{"json"},
		{"json", "--config", "/does/not/exist.json"},
	} {
		out, err := runCLI(t, "", args...)
		if err == nil {
			t.Errorf("%v: expected an error, got %q", args, out)
			continue
		}
		if strings.Contains(err.Error(), "9731") || strings.Contains(err.Error(), "KEY-MATERIAL") {
			t.Errorf("%v: error exposes input or key: %v", args, err)
		}
	}
}

func TestJSON(t *testing.T) {
	t.Setenv("PGFAKE_KEY", "test-key-1")
	cfg := filepath.Join(t.TempDir(), "pgfake.json")
	if err := os.WriteFile(cfg, []byte(`{
	  "key_env": "PGFAKE_KEY",
	  "rules": {
	    "users.email": {"transformer": "email", "scope": "email"},
	    "users.name":  {"transformer": "full_name"},
	    "users.profile": {"transformer": "json", "paths": {"settings.phone": {"transformer": "phone"}}}
	  }
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	in := `{"id":42,"email":"alex@gmail.com","name":"Alex Johnson","note":"<b>&</b>","deleted_at":null,"profile":{"settings":{"phone":"+13105551234"}},"big":12345678901234567890}
{"email":"alex@gmail.com","email2":"email"}
`
	out, err := runCLI(t, in, "json", "--config", cfg)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %q", out)
	}
	// Key order, untouched values and number precision survive.
	if !strings.HasPrefix(lines[0], `{"id":42,"email":"user_0a14a603968e522f9d80@example.test","name":"`) ||
		!strings.Contains(lines[0], `"note":"<b>&</b>","deleted_at":null,"profile":{"settings":{"phone":"+1555`) ||
		!strings.HasSuffix(lines[0], `"big":12345678901234567890}`) {
		t.Errorf("line 1: %s", lines[0])
	}
	if lines[1] != `{"email":"user_0a14a603968e522f9d80@example.test","email2":"email"}` {
		t.Errorf("line 2: %s", lines[1])
	}
	for _, leak := range []string{"alex@gmail.com", "Alex Johnson", "3105551234"} {
		if strings.Contains(out, leak) {
			t.Errorf("output contains %q", leak)
		}
	}

	// Malformed input fails without echoing it.
	_, err = runCLI(t, `{"email":"SENSITIVE-9731"`, "json", "--config", cfg)
	if err == nil || strings.Contains(err.Error(), "9731") {
		t.Errorf("malformed input: %v", err)
	}
	_, err = runCLI(t, `["SENSITIVE-9731"]`, "json", "--config", cfg)
	if err == nil || strings.Contains(err.Error(), "9731") {
		t.Errorf("non-object input: %v", err)
	}

	// Several tables in the config: --table picks one.
	multi := filepath.Join(t.TempDir(), "multi.json")
	os.WriteFile(multi, []byte(`{"rules":{"users.email":{"transformer":"email","scope":"email"},"orders.customer_email":{"transformer":"email","scope":"email"}}}`), 0o600)
	if _, err := runCLI(t, `{}`, "json", "--config", multi); err == nil || !strings.Contains(err.Error(), "--table") {
		t.Errorf("ambiguous table: %v", err)
	}
	out, err = runCLI(t, `{"customer_email":"alex@gmail.com"}`, "json", "--config", multi, "--table", "orders")
	if err != nil || out != `{"customer_email":"user_0a14a603968e522f9d80@example.test"}`+"\n" {
		t.Errorf("--table: %q, %v", out, err)
	}

	// Typos in the config are caught.
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte(`{"rules":{"users.email":{"transformer":"email","scop":"x"}}}`), 0o600)
	if _, err := runCLI(t, `{}`, "json", "--config", bad); err == nil {
		t.Error("unknown config field accepted")
	}
}

func TestListAndVersion(t *testing.T) {
	out, err := runCLI(t, "", "list")
	if err != nil || !strings.Contains(out, "email\n") || !strings.Contains(out, "full_name\n") {
		t.Errorf("list: %q, %v", out, err)
	}
	out, err = runCLI(t, "", "version")
	if err != nil || !strings.HasPrefix(out, "pgfake 0.1.0 (algorithm v1)") {
		t.Errorf("version: %q, %v", out, err)
	}
	if out, err := runCLI(t, ""); err != nil || !strings.Contains(out, "Usage:") {
		t.Errorf("usage: %q, %v", out, err)
	}
}
