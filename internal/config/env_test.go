package config

import "testing"

func TestEnvironment(t *testing.T) {
	cases := map[string]string{
		"fact_test_aaaaaaaaaaaaaaaaaaaaaaaa": "test",
		"fact_live_bbbbbbbbbbbbbbbbbbbbbbbb": "live",
		"sk_other":                           "unknown",
		"":                                   "unknown",
	}
	for k, want := range cases {
		if got := Environment(k); got != want {
			t.Errorf("Environment(%q)=%q want %q", k, got, want)
		}
	}
}

func TestRedactKey(t *testing.T) {
	got := RedactKey("fact_test_abcdefghijklmnopqrstuvwx")
	if got == "fact_test_abcdefghijklmnopqrstuvwx" || got == "" {
		t.Fatalf("key not redacted: %q", got)
	}
	if want := "fact_test_…uvwx"; got != want {
		t.Fatalf("RedactKey = %q want %q", got, want)
	}
}
