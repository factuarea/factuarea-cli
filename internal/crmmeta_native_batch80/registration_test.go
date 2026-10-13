package crmmeta_native_batch80_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/cmd"
)

func TestActualRootRegistersNativeMetaWithoutRemovingOriginalSurfaces(t *testing.T) {
	root := cmd.NewRootCmd()
	for _, path := range [][]string{{"crm", "meta", "options"}, {"crm", "credential-context"}, {"crm", "whatsapp", "prepare"}} {
		leaf, _, e := root.Find(path)
		if e != nil || leaf.Name() != path[len(path)-1] {
			t.Fatalf("actual root lacks %v: %v", path, e)
		}
	}
	group, _, e := root.Find([]string{"crm", "meta"})
	if e != nil || len(group.Commands()) != 7 {
		t.Fatal("native seven carriers missing")
	}
	originals, _, e := root.Find([]string{"crm", "originals"})
	if e != nil || originals.Name() != "originals" || len(originals.Commands()) != 4 {
		t.Fatal("the four native Original GET commands changed")
	}
}
func TestActualRootNativeOAuthAndGlobalJSONWithoutAPIKeyFallback(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "")
	t.Setenv("META_NATIVE_CURRENT_OAUTH", "actual-current-native-bearer")
	calls := 0
	raw := " \n{\"data\":{\"options\":[]}}\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/crm/connections/meta-lead-ads/options" || r.URL.RawQuery != "" || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer actual-current-native-bearer" || r.Header.Get("Factuarea-Version") != "2026-10-10" || r.Header.Get("X-API-Key") != "" {
			t.Error("actual native root resolver changed original transport")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, raw)
	}))
	defer server.Close()
	t.Setenv("FACTUAREA_BASE_URL", server.URL)
	root := cmd.NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--json", "crm", "meta", "options", "--oauth-token-env", "META_NATIVE_CURRENT_OAUTH", "--api-version", "2026-10-10"})
	if e := root.Execute(); e != nil || calls != 1 || out.String() != raw {
		t.Fatal(out.String(), calls, e)
	}
}
func TestActualRootAPIKeyAndOAuthDisguiseRejection(t *testing.T) {
	key := "fact_test_" + strings.Repeat("a", 24)
	t.Setenv("FACTUAREA_API_KEY", key)
	t.Setenv("META_NATIVE_CURRENT_OAUTH", key)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("actual API key was replaced")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":{"options":[]}}`)
	}))
	defer server.Close()
	t.Setenv("FACTUAREA_BASE_URL", server.URL)
	for _, oauth := range []bool{false, true} {
		root := cmd.NewRootCmd()
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		args := []string{"crm", "meta", "options"}
		if oauth {
			args = append(args, "--oauth-token-env", "META_NATIVE_CURRENT_OAUTH")
		}
		root.SetArgs(args)
		e := root.Execute()
		if !oauth && e != nil || oauth && e == nil {
			t.Fatal("native auth distinction lost", e)
		}
	}
	if calls != 1 {
		t.Fatal("OAuth guise attempted HTTP", calls)
	}
}
