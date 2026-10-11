package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

const crmEntityExportTestID = "01900000-0000-7000-8000-000000000110"

func runCrmEntityExportTestCommand(t *testing.T, serverURL, input string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("FACTUAREA_API_KEY", "fact_test_"+strings.Repeat("a", 24))
	t.Setenv("FACTUAREA_BASE_URL", serverURL)
	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(input))
	root.SetArgs(append([]string{"--json", "crm", "entity-exports"}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestCrmEntityExportLostReceiptRecoversSameOriginalAndNativeBinary(t *testing.T) {
	// Raw whitespace, native Activity note and column order are part of the
	// original intent. A complete JSON prefix with an incomplete HTTP body must
	// not become a confirmed receipt or trigger another POST.
	body := "{\n  \"id\":\"" + crmEntityExportTestID + "\", \"entity_type\":\"activity\",\n  \"filters\":{\"type\":\"note\",\"due_from\":\"2026-10-01T00:00:00Z\",\"due_to\":\"2026-10-10T23:59:59Z\"},\n  \"columns\":[\"type\",\"id\",\"status\"], \"confirmed\":true\n}\n"
	const key = "source110-original-intent"
	const binary = "type,id,status\r\nnote,01900000-0000-7000-8000-000000000111,pending\r\n"
	var posts, reads, downloads, accounts atomic.Int32
	var ready atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fact_test_"+strings.Repeat("a", 24) {
			t.Error("native profile authentication changed")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/account":
			accounts.Add(1)
			fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm_imports:export"]}}}`)
		case "/v1/crm/entity-exports":
			posts.Add(1)
			raw, err := io.ReadAll(r.Body)
			if err != nil || r.Method != http.MethodPost || string(raw) != body || r.Header.Get("Idempotency-Key") != key || r.URL.RawQuery != "" {
				t.Error("request altered original id/body/key or dispatched another route")
			}
			receipt := fmt.Sprintf(`{"data":{"id":%q,"entity_type":"activity","status":"processing"}}`, crmEntityExportTestID)
			w.Header().Set("Content-Length", strconv.Itoa(len(receipt)+8))
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, receipt)
		case "/v1/crm/entity-exports/" + crmEntityExportTestID:
			reads.Add(1)
			if r.Method != http.MethodGet || r.URL.RawQuery != "" || r.Header.Get("Idempotency-Key") != "" {
				t.Error("recovery did not use the read-only Original")
			}
			if ready.Load() {
				fmt.Fprintf(w, `{"data":{"id":%q,"entity_type":"activity","status":"ready","filename":"crm-activity.csv","size_bytes":%d}}`, crmEntityExportTestID, len(binary))
			} else {
				fmt.Fprintf(w, `{"data":{"id":%q,"entity_type":"activity","status":"processing"}}`, crmEntityExportTestID)
			}
		case "/v1/crm/entity-exports/" + crmEntityExportTestID + "/download":
			downloads.Add(1)
			if r.Method != http.MethodGet || r.URL.RawQuery != "" || r.Header.Get("Accept") != "text/csv" {
				t.Error("download did not use the native binary route")
			}
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="crm-activity.csv"`)
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			fmt.Fprint(w, binary)
		default:
			t.Error("unexpected route, pagination or resend")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	_, _, err := runCrmEntityExportTestCommand(t, server.URL, body, "request", "--data", "-", "--idempotency-key", key)
	if err == nil || exit.ForError(err) != exit.Usage || accounts.Load() != 0 || posts.Load() != 0 {
		t.Fatal("request without explicit human confirmation reached HTTP")
	}
	stdout, stderr, err := runCrmEntityExportTestCommand(t, server.URL, body, "request", "--data", "-", "--idempotency-key", key, "--confirmed")
	if err == nil || exit.ForError(err) != exit.Network || posts.Load() != 1 || stdout != "" || !json.Valid([]byte(stderr)) || !strings.Contains(stderr, crmEntityExportTestID) {
		t.Fatal("incomplete response was confirmed or original POST was replayed")
	}
	for _, private := range []string{key, body, "due_from", "fact_test_"} {
		if strings.Contains(stderr, private) {
			t.Error("uncertain result exposed original selection or credential")
		}
	}
	stdout, stderr, err = runCrmEntityExportTestCommand(t, server.URL, "", "status", crmEntityExportTestID)
	if err != nil || !json.Valid([]byte(stdout)) || !strings.Contains(stdout, `"status":"processing"`) || stderr != "" || posts.Load() != 1 {
		t.Fatal("status alias did not recover the same Original without resend")
	}
	ready.Store(true)
	outputPath := filepath.Join(t.TempDir(), "chosen.csv")
	stdout, stderr, err = runCrmEntityExportTestCommand(t, server.URL, "", "download", crmEntityExportTestID, "--output", outputPath)
	actual, readErr := os.ReadFile(outputPath)
	if err != nil || readErr != nil || string(actual) != binary || stdout != "" || stderr != "" || posts.Load() != 1 || reads.Load() != 2 || downloads.Load() != 1 || accounts.Load() != 3 {
		t.Fatal("download changed the original binary, omitted fresh Original or created another job")
	}
	if info, err := os.Stat(outputPath); err != nil || info.Mode().Perm() != 0600 {
		t.Error("explicit binary destination does not retain private file permissions")
	}
}

func TestCrmEntityExportFreshReadyDoesNotGrantRevokedBinaryDownload(t *testing.T) {
	var reads, downloads, posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts.Add(1)
			t.Error("download recovery sent a POST")
		}
		switch r.URL.Path {
		case "/v1/account":
			fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm_imports:export"]}}}`)
		case "/v1/crm/entity-exports/" + crmEntityExportTestID:
			reads.Add(1)
			fmt.Fprintf(w, `{"data":{"id":%q,"entity_type":"service_ticket","status":"ready","filename":"crm-service_ticket.csv","size_bytes":16}}`, crmEntityExportTestID)
		case "/v1/crm/entity-exports/" + crmEntityExportTestID + "/download":
			downloads.Add(1)
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":{"type":"authorization_error","code":"forbidden","subcode":"crm_context_revoked","message":"Autoridad actual denegada"}}`)
		default:
			t.Error("unexpected route or retry")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	outputPath := filepath.Join(t.TempDir(), "must-not-survive.csv")
	stdout, stderr, err := runCrmEntityExportTestCommand(t, server.URL, "", "download", crmEntityExportTestID, "--output", outputPath)
	if err == nil || exit.ForError(err) != exit.Perm || stdout != "" || !json.Valid([]byte(stderr)) || !strings.Contains(stderr, "crm_context_revoked") || reads.Load() != 1 || downloads.Load() != 1 || posts.Load() != 0 {
		t.Fatal("ready metadata became a grant, denial changed or request was replayed")
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Error("denied binary left an automatic or partial local PII file")
	}
}
