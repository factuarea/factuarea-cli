package safety

import "testing"

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

func TestConfirmNeverBlocksInNoInput(t *testing.T) {
	called := false
	prompt := func(string) (string, error) { called = true; return "", nil }
	// Sin --confirm y no-input: falla inmediatamente, sin promptear.
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
