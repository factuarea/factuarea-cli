package crmmeta_native_batch80

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func bodyFile(t *testing.T, wire []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "body.json")
	if e := os.WriteFile(path, wire, 0600); e != nil {
		t.Fatal(e)
	}
	return path
}
func runCommand(t *testing.T, resolve Resolver, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "factuarea", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(NewCommand(resolve))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"meta"}, args...))
	e := root.Execute()
	return out.String(), e
}
func TestCLIDryRunCompleteRedactsSecretsAndNeverResolvesCredential(t *testing.T) {
	file := bodyFile(t, []byte(`{"state":"`+testState+`","code":"secret-code"}`))
	out, e := runCommand(t, func(*cobra.Command) (Config, error) { t.Fatal("dry run resolved credential"); return Config{}, nil }, "oauth-complete", "--body-file", file, "--confirm", "oauth-complete", "--dry-run")
	if e != nil || strings.Contains(out, "secret-code") || strings.Contains(out, testState) || !strings.Contains(out, "completeCrmMetaOAuth") {
		t.Fatal(out, e)
	}
}
func TestCLIIndependentConfirmationPrecedesHTTP(t *testing.T) {
	calls := 0
	resolver := func(*cobra.Command) (Config, error) { calls++; return Config{APIKey: "fact_test_actual"}, nil }
	for _, args := range [][]string{{"oauth-prepare", "--body-file", bodyFile(t, testWire(t, PrepareInput{testPage, true}))}, {"publish-mapping", testConnection, testForm, "--body-file", bodyFile(t, testWire(t, testPublish())), "--confirm", testConnection}, {"publish-consent", testConnection, "--body-file", bodyFile(t, testWire(t, ConsentInput{testCredential, []Grant{{testForm, []string{"read"}}}, nil, true}))}} {
		if _, e := runCommand(t, resolver, args...); e == nil {
			t.Fatal("accepted missing/foreign confirmation")
		}
	}
	if calls != 0 {
		t.Fatal("resolved/attempted HTTP before human confirmation")
	}
}
func TestCLIInspectOriginalJSONAndPrivateOutput(t *testing.T) {
	count := 0
	response := append([]byte(" \n"), testWire(t, map[string]any{"data": testInspection()})...)
	response = append(response, '\n')
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer current-native" {
			t.Error("native query/auth changed")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(response)
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "inspection.json")
	out, e := runCommand(t, func(*cobra.Command) (Config, error) {
		return Config{OAuthBearer: "current-native", BaseURL: server.URL + "/v1"}, nil
	}, "inspect", testConnection, testForm, "--body-file", bodyFile(t, testWire(t, testInspect())), "--output", "json", "--output-file", output)
	saved, _ := os.ReadFile(output)
	st, _ := os.Stat(output)
	if e != nil || out != string(response) || !bytes.Equal(saved, response) || st.Mode().Perm() != 0600 || count != 1 {
		t.Fatal(out, e, count)
	}
	var wire map[string]json.RawMessage
	if json.Unmarshal(saved, &wire) != nil {
		t.Fatal("native output changed")
	}
}
func TestCLIUnknownCompleteIsOnceWithoutAutomaticRecovery(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		io.WriteString(w, `{"error":"meta_oauth_unconfirmed","secret":"secret-code"}`)
	}))
	defer server.Close()
	out, e := runCommand(t, func(*cobra.Command) (Config, error) {
		return Config{APIKey: "fact_test_actual", BaseURL: server.URL + "/v1"}, nil
	}, "oauth-complete", "--body-file", bodyFile(t, testWire(t, CompleteInput{testState, "secret-code"})), "--confirm", "oauth-complete")
	if e == nil || count != 1 || strings.Contains(e.Error(), "secret-code") || strings.Contains(out, "secret-code") {
		t.Fatal(count, out, e)
	}
}
func TestCLIRegisterUsesExistingCRMAndRejectsDuplicate(t *testing.T) {
	root := &cobra.Command{Use: "factuarea"}
	crm := &cobra.Command{Use: "crm"}
	root.AddCommand(crm)
	resolver := func(*cobra.Command) (Config, error) { return Config{APIKey: "fact_test_actual"}, nil }
	if e := Register(root, resolver); e != nil {
		t.Fatal(e)
	}
	if len(crm.Commands()) != 1 || crm.Commands()[0].Name() != "meta" || len(crm.Commands()[0].Commands()) != 7 {
		t.Fatal("native seven commands missing")
	}
	if e := Register(root, resolver); e == nil {
		t.Fatal("duplicate registration accepted")
	}
}
