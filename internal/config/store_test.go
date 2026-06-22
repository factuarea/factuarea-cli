package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	st := NewFileStore(path)

	if _, err := st.GetKey("default"); err == nil {
		t.Fatal("expected error for missing key")
	}
	if err := st.SetKey("default", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetKey("default")
	if err != nil || got != "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("roundtrip failed: %q %v", got, err)
	}
	// Segundo profile no pisa al primero.
	if err := st.SetKey("acme-live", "fact_live_bbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetKey("default"); got != "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("second profile clobbered first: %q", got)
	}
	if err := st.DeleteKey("default"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetKey("default"); err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestFileStoreFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	st := NewFileStore(path)

	if err := st.SetKey("acme-live", "fact_live_cccccccccccccccccccccccc"); err != nil {
		t.Fatal(err)
	}

	// El fichero final debe quedar en 0600 (la key live no es legible por grupo/otros).
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected file mode 0600, got %o", perm)
	}

	// Tras un SetKey exitoso no debe quedar ningún temporal *.tmp huérfano.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file after SetKey: %s", e.Name())
		}
	}
}
