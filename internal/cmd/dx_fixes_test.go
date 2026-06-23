package cmd

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/config"
	"github.com/factuarea/factuarea-cli/internal/exit"
)

func TestFileNotFoundTranslatedAndUsageExit(t *testing.T) {
	cases := [][]string{{"-d", "@/no/existe.json"}, {"--data-file", "/no/existe.json"}}
	for _, c := range cases {
		args := append([]string{"clients", "create"}, c...)
		_, err := runCmd(t, "", args...)
		if err == nil {
			t.Fatalf("%v con fichero inexistente debe fallar", c)
		}
		if !strings.Contains(err.Error(), "no se pudo leer el fichero") || !strings.Contains(err.Error(), "no existe") {
			t.Fatalf("%v: error sin mensaje español, got %v", c, err)
		}
		if exit.ForError(err) != exit.Usage {
			t.Fatalf("%v: exit = %d, want Usage(2)", c, exit.ForError(err))
		}
	}
}

func TestWriteOutputFileTranslatedAndUsageExit(t *testing.T) {
	err := writeOutputFile(filepath.Join(t.TempDir(), "noexiste", "x.pdf"), []byte("x"))
	if err == nil {
		t.Fatal("escribir en un directorio inexistente debe fallar")
	}
	if !strings.Contains(err.Error(), "no se pudo escribir el fichero") {
		t.Fatalf("error sin mensaje español, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want Usage(2)", exit.ForError(err))
	}
}

func TestDataFileIsDirectoryTranslated(t *testing.T) {
	dir := t.TempDir()
	_, err := runCmd(t, "", "clients", "create", "--data-file", dir)
	if err == nil || !strings.Contains(err.Error(), "es un directorio") {
		t.Fatalf("directorio como fichero debe avisar, got %v", err)
	}
}

func TestUnknownCommandTranslatedAtRoot(t *testing.T) {
	_, err := runCmd(t, "", "frobnicate")
	if err == nil || !strings.Contains(err.Error(), "comando desconocido") {
		t.Fatalf("comando desconocido debe traducirse, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want Usage(2)", exit.ForError(err))
	}
}

func TestUnknownSubcommandSuggestsSibling(t *testing.T) {
	_, err := runCmd(t, "", "clients", "lst")
	if err == nil {
		t.Fatal("subcomando desconocido debe fallar")
	}
	if !strings.Contains(err.Error(), "comando desconocido") || !strings.Contains(err.Error(), "¿Quisiste decir?") || !strings.Contains(err.Error(), "list") {
		t.Fatalf("debe sugerir 'list' en español, got %v", err)
	}
}

func TestKeyValueParseErrorTranslated(t *testing.T) {
	_, err := runCmd(t, "", "clients", "create", "--metadata", "sinformato", "--name", "x", "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "clave=valor") {
		t.Fatalf("error de map mal formado debe traducirse, got %v", err)
	}
}

func TestLoginRejectsPositionalKey(t *testing.T) {
	_, err := runRoot(t, "", "login", "fact_test_zzzzzzzzzzzzzzzzzzzzzzzz")
	if err == nil || !strings.Contains(err.Error(), "no acepta la API key como argumento posicional") {
		t.Fatalf("login debe rechazar la key posicional, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want Usage(2)", exit.ForError(err))
	}
}

func TestKeyFormatWordingMentionsTrailingChars(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "")
	_, err := runRoot(t, "fact_test_short", "login", "--api-key=-", "--no-input")
	if err == nil || !strings.Contains(err.Error(), "seguido de 24 caracteres") {
		t.Fatalf("el wording de la key debe decir 'seguido de 24 caracteres', got %v", err)
	}
}

func TestNonJSONErrorBodyDoesNotLeakServerHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(502)
		_, _ = w.Write([]byte("<html>nginx/1.25.3</html>"))
	}))
	t.Cleanup(srv.Close)
	_, err := runCmd(t, srv.URL, "clients", "show", "cli_1")
	if err == nil || !strings.Contains(err.Error(), "respuesta no-JSON") {
		t.Fatalf("error no-JSON debe normalizarse, got %v", err)
	}
	if strings.Contains(err.Error(), "nginx") {
		t.Fatalf("no debe filtrar el cuerpo/HTML, got %v", err)
	}
}

func TestKeyringFallbackWarningUsesPlatformPath(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "config.toml")
	store := config.NewFileStore(want)
	p, ok := store.(config.PathProvider)
	if !ok {
		t.Fatal("fileStore debe exponer Path()")
	}
	if p.Path() != want {
		t.Fatalf("Path() = %q, want %q", p.Path(), want)
	}
}
