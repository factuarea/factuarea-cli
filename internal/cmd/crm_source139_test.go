package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

func runCrmSource139(t *testing.T, url, input string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("FACTUAREA_API_KEY", "fact_test_"+strings.Repeat("a", 24))
	t.Setenv("FACTUAREA_BASE_URL", url)
	root := NewRootCmd()
	var out, diagnostics bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&diagnostics)
	root.SetIn(strings.NewReader(input))
	root.SetArgs(append([]string{"--json"}, args...))
	err := root.Execute()
	return out.String(), diagnostics.String(), err
}

func TestCrmSource139ApplyOriginalReadsLiteralIntentStatesAndFreshDenialWithoutPost(t *testing.T) {
	const run = "550e8400-e29b-41d4-a716-446655440139"
	const key = "native139 original key"
	// Input is retained as an opaque first POST body; this reader does not claim
	// admission of Apply, whose request schema is absent from native export139.
	raw := "{\n \"expected_version\":1, \"preview_hash\":\"" + strings.Repeat("a", 64) + "\",\n \"effect_uuid\":\"01900000-0000-7000-8000-000000000139\", \"mode\":\"retry_failed\", \"confirmed\":true\n}\n"
	hash := sha256.Sum256([]byte(raw))
	expectedHash := hex.EncodeToString(hash[:])
	const committedBody = "{\n \"data\":{\"id\":\"01900000-0000-7000-8000-000000000139\",\"status\":\"processing\"}\n}\n"
	var requests, originals, writes atomic.Int32
	state := "missing"
	denied := false
	truncated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet {
			writes.Add(1)
			t.Error("Original reader sent a mutating request")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/account" {
			fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm_imports:write"]}}}`)
			return
		}
		if r.URL.Path != "/v1/crm/imports/"+run+"/apply/original" {
			t.Error("Current or undeclared recovery route was requested")
			w.WriteHeader(404)
			return
		}
		originals.Add(1)
		if r.Header.Get("Idempotency-Key") != key || r.URL.Query().Get("original_body_sha256") != expectedHash || len(r.URL.Query()) != 1 || r.ContentLength > 0 {
			t.Error("Original UUID/raw hash/key were changed or body was uploaded")
		}
		if denied {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"error":{"type":"authorization_error","code":"insufficient_scope","message":"Current C7 revoked"}}`)
			return
		}
		var receipt any
		if state == "confirmed" {
			receipt = map[string]any{"status": 202, "body": committedBody}
		}
		payload, _ := json.Marshal(map[string]any{"data": map[string]any{"original_body_sha256": expectedHash, "authorization_version": "native-current-proof139", "state": state, "original_response": receipt}})
		if truncated {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)+8))
		}
		w.Write(payload)
	}))
	defer server.Close()
	args := []string{"crm", "imports", "apply-original", run, "--original-body", "-", "--idempotency-key", key}
	_, _, err := runCrmSource139(t, server.URL, raw, args...)
	if err == nil || exit.ForError(err) != exit.Usage || requests.Load() != 0 {
		t.Fatal("private source gate dispatched HTTP")
	}
	args = append(args, "--source-contracts")
	for _, factual := range []string{"missing", "in_progress", "unconfirmed", "confirmed"} {
		state = factual
		stdout, stderr, err := runCrmSource139(t, server.URL, raw, args...)
		if err != nil || stderr != "" {
			t.Fatalf("Original read failed for %s: %v", factual, err)
		}
		var envelope struct {
			Data struct {
				State            string `json:"state"`
				OriginalResponse *struct {
					Status int    `json:"status"`
					Body   string `json:"body"`
				} `json:"original_response"`
			} `json:"data"`
		}
		if json.Unmarshal([]byte(stdout), &envelope) != nil || envelope.Data.State != factual {
			t.Fatal("factual Original state was replaced or inferred")
		}
		if factual == "confirmed" {
			if envelope.Data.OriginalResponse == nil || envelope.Data.OriginalResponse.Status != 202 || envelope.Data.OriginalResponse.Body != committedBody {
				t.Fatal("immutable original response bytes changed")
			}
		} else if envelope.Data.OriginalResponse != nil {
			t.Fatal("unconfirmed state fabricated a successful receipt")
		}
	}
	truncated = true
	stdout, stderr, err := runCrmSource139(t, server.URL, raw, args...)
	if err == nil || exit.ForError(err) != exit.Network || stdout != "" {
		t.Fatal("incomplete read became confirmed or retried")
	}
	for _, private := range []string{raw, key, "fact_test_"} {
		if strings.Contains(stderr, private) {
			t.Error("failed transport leaked raw intent/key/credential")
		}
	}
	truncated = false
	denied = true
	stdout, _, err = runCrmSource139(t, server.URL, raw, args...)
	if err == nil || exit.ForError(err) != exit.Perm || stdout != "" || originals.Load() != 6 || writes.Load() != 0 {
		t.Fatal("fresh native denial used cached Original, Current or a POST recovery")
	}
}

func TestCrmSource139AgendaPreservesActualLayersEmptySelectionAndRefusesReplayOrFakePaging(t *testing.T) {
	var agendas, writes atomic.Int32
	mode := "partial"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
			t.Error("agenda reader wrote or called a provider")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/account" {
			fmt.Fprint(w, `{"data":{"api_key":{"scopes":["tasks:read"]}}}`)
			return
		}
		if r.URL.Path != "/v1/agenda" || r.URL.Query().Get("from") != "2026-10-01" || r.URL.Query().Get("to") != "2026-10-11" {
			t.Error("undeclared route/window/pagination")
			w.WriteHeader(404)
			return
		}
		agendas.Add(1)
		if mode == "unavailable" {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":{"type":"api_error","code":"internal_error","message":"native owner unavailable"}}`)
			return
		}
		if mode == "denied" {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"error":{"type":"authorization_error","code":"insufficient_scope","message":"native authority revoked"}}`)
			return
		}
		if mode == "empty" {
			if values, present := r.URL.Query()["sources"]; !present || len(values) != 1 || values[0] != "" {
				t.Error("explicit empty sources was omitted/defaulted")
			}
			fmt.Fprint(w, `{"data":[],"has_more":false,"next_cursor":null,"sources":[]}`)
			return
		}
		if r.URL.Query().Get("sources") != "tasks,holiday" || r.URL.Query().Get("assignee_id") != "me" {
			t.Error("native selection changed")
		}
		if mode == "fake_page" {
			fmt.Fprint(w, `{"data":[],"has_more":true,"next_cursor":"fake","sources":["holiday"]}`)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"holiday:ES:2026-10-09","object":"agenda_item","type":"holiday","title":"Fixture holiday","starts_on":"2026-10-09","ends_on":"2026-10-09","status":null,"url":"/tasks","task":null,"fiscal":null}],"has_more":false,"next_cursor":null,"sources":["holiday"]}`)
	}))
	defer server.Close()
	args := []string{"agenda", "source-read", "--from", "2026-10-01", "--to", "2026-10-11", "--sources", "tasks,holiday", "--assignee-id", "me", "--source-contracts"}
	stdout, stderr, err := runCrmSource139(t, server.URL, "", args...)
	if err != nil || stderr != "" || !strings.Contains(stdout, `"holiday:ES:2026-10-09"`) {
		t.Fatal("native non-UUID identity or actual served layer was replaced")
	}
	var result struct {
		Sources []string `json:"sources"`
	}
	if json.Unmarshal([]byte(stdout), &result) != nil || len(result.Sources) != 1 || result.Sources[0] != "holiday" {
		t.Fatal("requested unreadable task layer was fabricated")
	}
	mode = "empty"
	empty := []string{"agenda", "source-read", "--from", "2026-10-01", "--to", "2026-10-11", "--sources=", "--source-contracts"}
	stdout, _, err = runCrmSource139(t, server.URL, "", empty...)
	if err != nil || json.Unmarshal([]byte(stdout), &result) != nil || result.Sources == nil || len(result.Sources) != 0 {
		t.Fatal("explicit empty universe was changed")
	}
	for _, factual := range []string{"fake_page", "unavailable", "denied"} {
		mode = factual
		stdout, _, err = runCrmSource139(t, server.URL, "", args...)
		if err == nil || stdout != "" {
			t.Fatal("invalid/unavailable/denied agenda became usable")
		}
	}
	if agendas.Load() != 5 || writes.Load() != 0 {
		t.Fatal("read auto-retried, paged or wrote")
	}
}
