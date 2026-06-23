package safety

import (
	"errors"
	"testing"
)

func TestRequireLive(t *testing.T) {
	if err := RequireLive("live", false); err == nil {
		t.Fatal("live without --live must fail")
	}
	if err := RequireLive("live", true); err != nil {
		t.Fatal("live with --live must pass")
	}
	if err := RequireLive("test", false); err != nil {
		t.Fatal("test never needs --live")
	}
}

func TestRequireSandbox(t *testing.T) {
	if err := RequireSandbox("live"); err == nil {
		t.Fatal("trigger must reject live")
	}
	if err := RequireSandbox("test"); err != nil {
		t.Fatal("trigger must allow test")
	}
}

func TestHasScope(t *testing.T) {
	cases := []struct {
		name     string
		scopes   []string
		required string
		want     bool
	}{
		{"empty required passes", nil, "", true},
		{"missing scope blocks", nil, "invoices:read", false},
		{"wildcard covers any", []string{"*"}, "invoices:read", true},
		{"exact match passes", []string{"invoices:read"}, "invoices:read", true},
		{"different scope blocks", []string{"clients:read"}, "invoices:read", false},
		{"present among many", []string{"clients:read", "invoices:read"}, "invoices:read", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HasScope(c.scopes, c.required); got != c.want {
				t.Fatalf("HasScope(%v, %q) = %v, want %v", c.scopes, c.required, got, c.want)
			}
		})
	}
}

func TestConfirmNeverBlocksInNoInput(t *testing.T) {
	called := false
	prompt := func(string) (string, error) { called = true; return "", nil }
	if err := Confirm("inv_1", "", false, true, prompt); err == nil {
		t.Fatal("expected immediate failure")
	}
	if called {
		t.Fatal("must not prompt in no-input")
	}
}

func TestConfirmFlagMatch(t *testing.T) {
	if err := Confirm("inv_1", "inv_1", false, true, nil); err != nil {
		t.Fatal("matching --confirm must pass")
	}
	if err := Confirm("inv_1", "inv_2", false, true, nil); err == nil {
		t.Fatal("mismatched --confirm must fail")
	}
}

func TestConfirmInteractivePrompt(t *testing.T) {
	prompt := func(string) (string, error) { return "inv_1", nil }
	if err := Confirm("inv_1", "", true, false, prompt); err != nil {
		t.Fatalf("interactive matching prompt must pass: %v", err)
	}
}

func TestConfirmInteractivePromptError(t *testing.T) {
	boom := errors.New("error de lectura del prompt")
	prompt := func(string) (string, error) { return "", boom }
	err := Confirm("inv_1", "", true, false, prompt)
	if err == nil {
		t.Fatal("interactive prompt error must propagate")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected the prompt error to propagate, got %v", err)
	}
}

func TestConfirmInteractivePromptMismatch(t *testing.T) {
	prompt := func(string) (string, error) { return "inv_2", nil }
	if err := Confirm("inv_1", "", true, false, prompt); err == nil {
		t.Fatal("interactive prompt with mismatched id must fail (cancelación)")
	}
}
