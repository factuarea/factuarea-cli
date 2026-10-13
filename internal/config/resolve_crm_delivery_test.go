package config_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/cmd"
	"github.com/factuarea/factuarea-cli/internal/config"
)

const (
	crmSandboxAKey = "fact_test_aaaaaaaaaaaaaaaaaaaaA001"
	crmSandboxBKey = "fact_test_bbbbbbbbbbbbbbbbbbbbB002"
	crmGenericKey  = "fact_live_eeeeeeeeeeeeeeeeeeeeE003"
)

type crmDeliveryProfileStore struct {
	keys    map[string]string
	key     string
	err     error
	reads   []string
	writes  int
	deletes int
}

func (store *crmDeliveryProfileStore) GetKey(profile string) (string, error) {
	store.reads = append(store.reads, profile)
	if store.err != nil {
		return store.key, store.err
	}
	if store.keys == nil {
		return store.key, nil
	}
	key, ok := store.keys[profile]
	if !ok {
		return "", config.ErrNotFound
	}
	return key, nil
}

func (store *crmDeliveryProfileStore) SetKey(profile, key string) error {
	store.writes++
	return nil
}

func (store *crmDeliveryProfileStore) DeleteKey(profile string) error {
	store.deletes++
	return nil
}

func crmDeliveryEnv(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestCRMDeliverySelectedProfilesKeepSandboxCredentialsIsolated(t *testing.T) {
	keys := map[string]string{
		"sandbox-a": crmSandboxAKey,
		"sandbox-b": crmSandboxBKey,
		"default":   crmGenericKey,
	}
	for _, selectedBy := range []string{"flag", "environment"} {
		for _, profile := range []string{"sandbox-a", "sandbox-b", "default"} {
			t.Run(selectedBy+"/"+profile, func(t *testing.T) {
				store := &crmDeliveryProfileStore{keys: keys}
				selected := profile
				values := map[string]string{config.EnvProfile: "sandbox-b", config.EnvAPIKey: crmGenericKey}
				if selectedBy == "environment" {
					selected = ""
					values[config.EnvProfile] = profile
				}
				result, err := config.ResolveAPIKey("", selected, crmDeliveryEnv(values), store)
				if err != nil || result.Profile != profile || result.Source != "profile" || result.APIKey != keys[profile] || result.Environment != config.Environment(keys[profile]) {
					t.Fatal("el perfil seleccionado usó la credencial de otra empresa o entorno")
				}
				if len(store.reads) != 1 || store.reads[0] != profile || store.writes != 0 || store.deletes != 0 {
					t.Fatal("la resolución accedió a credenciales fuera del perfil seleccionado o modificó el almacén")
				}
			})
		}
	}
}

func TestCRMDeliveryUnavailableSelectedProfileFailsClosedWithoutSecrets(t *testing.T) {
	for _, selectedBy := range []string{"flag", "environment"} {
		for _, failure := range []struct {
			name string
			key  string
			err  error
		}{
			{name: "empty"},
			{name: "missing", err: config.ErrNotFound},
			{name: "locked", err: errors.New("locked store " + crmSandboxBKey)},
			{name: "partial_key_with_error", key: crmSandboxBKey, err: errors.New("cannot unlock " + crmSandboxBKey)},
		} {
			t.Run(selectedBy+"/"+failure.name, func(t *testing.T) {
				store := &crmDeliveryProfileStore{key: failure.key, err: failure.err}
				selected := "sandbox-b"
				values := map[string]string{config.EnvProfile: "sandbox-a", config.EnvAPIKey: crmGenericKey}
				if selectedBy == "environment" {
					selected = ""
					values[config.EnvProfile] = "sandbox-b"
				}
				result, err := config.ResolveAPIKey("", selected, crmDeliveryEnv(values), store)
				if err == nil || result != (config.Resolution{}) || len(store.reads) != 1 || store.reads[0] != "sandbox-b" || store.writes != 0 || store.deletes != 0 {
					t.Fatal("el perfil no disponible recurrió a otra credencial o devolvió una resolución parcial")
				}
				crmDeliveryAssertNoSecrets(t, err.Error())
			})
		}
	}
	result, err := config.ResolveAPIKey("", "sandbox-b", crmDeliveryEnv(map[string]string{config.EnvAPIKey: crmGenericKey}), nil)
	if err == nil || result != (config.Resolution{}) {
		t.Fatal("el perfil sin almacén usó la credencial genérica de otra empresa")
	}
	crmDeliveryAssertNoSecrets(t, err.Error())
}

func TestCRMDeliveryFileStoreSwitchAndRevocationKeepSandboxesSeparate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewFileStore(path)
	keys := map[string]string{"sandbox-a": crmSandboxAKey, "sandbox-b": crmSandboxBKey, "default": crmGenericKey}
	for profile, key := range keys {
		if err := store.SetKey(profile, key); err != nil {
			t.Fatal("no se pudo preparar el almacén de prueba")
		}
	}
	values := map[string]string{config.EnvAPIKey: crmGenericKey}
	for _, profile := range []string{"sandbox-a", "sandbox-b", "sandbox-a"} {
		values[config.EnvProfile] = profile
		result, err := config.ResolveAPIKey("", "", crmDeliveryEnv(values), config.NewFileStore(path))
		if err != nil || result.Profile != profile || result.APIKey != keys[profile] || result.Source != "profile" || result.Environment != "test" {
			t.Fatal("el cambio de perfil reutilizó credenciales de otra empresa o del entorno live")
		}
	}
	if err := store.DeleteKey("sandbox-a"); err != nil {
		t.Fatal("no se pudo revocar la credencial de prueba")
	}
	if result, err := config.ResolveAPIKey("", "sandbox-a", crmDeliveryEnv(values), config.NewFileStore(path)); err == nil || result != (config.Resolution{}) {
		t.Fatal("el perfil revocado cayó en la credencial de otro perfil")
	}
	result, err := config.ResolveAPIKey("", "sandbox-b", crmDeliveryEnv(values), config.NewFileStore(path))
	if err != nil || result.APIKey != crmSandboxBKey || result.Profile != "sandbox-b" || result.Environment != "test" {
		t.Fatal("la revocación del sandbox A afectó al sandbox B")
	}
}

func TestCRMDeliveryStdinCredentialStaysBoundToSelectedSandbox(t *testing.T) {
	store := &crmDeliveryProfileStore{key: crmSandboxAKey, err: errors.New("locked store " + crmSandboxAKey)}
	result, err := config.ResolveAPIKey(crmSandboxBKey, "sandbox-b", crmDeliveryEnv(map[string]string{
		config.EnvProfile: "sandbox-a",
		config.EnvAPIKey:  crmGenericKey,
	}), store)
	if err != nil || result.Source != "stdin" || result.Profile != "sandbox-b" || result.APIKey != crmSandboxBKey || result.Environment != "test" || len(store.reads) != 0 || store.writes != 0 || store.deletes != 0 {
		t.Fatal("la credencial proporcionada por stdin cambió de perfil o accedió a otra credencial")
	}
}

func TestCRMDeliveryRedactsSandboxAndLiveKeys(t *testing.T) {
	for _, key := range []string{crmSandboxAKey, crmSandboxBKey, crmGenericKey} {
		if !config.ValidKeyFormat(key) {
			t.Fatal("el fixture de credencial no cumple el contrato real")
		}
		redacted := config.RedactKey(key)
		if redacted != key[:10]+"…"+key[len(key)-4:] {
			t.Fatal("la redacción no conserva exclusivamente entorno y sufijo")
		}
		crmDeliveryAssertNoSecrets(t, redacted)
		if strings.Contains(redacted, key[10:len(key)-4]) {
			t.Fatal("la redacción reveló la parte secreta de la credencial")
		}
	}
	if config.RedactKey("short-secret") != "…" {
		t.Fatal("una credencial corta no se ocultó por completo")
	}
}

func TestCRMDeliveryDryRunDoesNotResolveProfilesSendRequestsOrExposeKeys(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	t.Setenv("FACTUAREA_BASE_URL", server.URL)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := config.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	store := config.NewFileStore(path)
	for profile, key := range map[string]string{"sandbox-a": crmSandboxAKey, "sandbox-b": crmSandboxBKey} {
		if err := store.SetKey(profile, key); err != nil {
			t.Fatal("no se pudo preparar el almacén del dry-run")
		}
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"sandbox-a", "sandbox-b", "missing-profile"} {
		t.Run(profile, func(t *testing.T) {
			t.Setenv(config.EnvAPIKey, crmGenericKey)
			t.Setenv(config.EnvProfile, "sandbox-b")
			if profile == "missing-profile" {
				t.Setenv(config.EnvAPIKey, "")
			}
			root := cmd.NewRootCmd()
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetIn(strings.NewReader(""))
			root.SetArgs([]string{"--profile", profile, "crm", "leads", "create", "--data", `{"name":"Draft","email":null}`, "--dry-run", "--json", "--no-input"})
			err := root.Execute()
			output := stdout.String() + stderr.String()
			if err != nil {
				output += err.Error()
			}
			crmDeliveryAssertNoSecrets(t, output)
			if err != nil || stderr.Len() != 0 || requests.Load() != 0 {
				t.Fatal("el dry-run necesitó credenciales, produjo un error o realizó una petición")
			}
			var draft map[string]json.RawMessage
			if err := json.Unmarshal(stdout.Bytes(), &draft); err != nil || len(draft) != 2 || string(draft["name"]) != `"Draft"` || string(draft["email"]) != "null" {
				t.Fatal("el dry-run alteró el borrador o añadió identidad, empresa o credenciales")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("el dry-run modificó el almacén de credenciales")
			}
		})
	}
}

func crmDeliveryAssertNoSecrets(t *testing.T, text string) {
	t.Helper()
	for _, key := range []string{crmSandboxAKey, crmSandboxBKey, crmGenericKey} {
		if strings.Contains(text, key) {
			t.Fatal("la salida reveló una credencial completa")
		}
	}
}
