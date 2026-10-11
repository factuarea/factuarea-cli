package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/exit"
)

// Prepared HTTP source cases: the fixture supplies wire responses, not grants,
// a replacement issuer, a React/query result or a cached Original.
func TestCrmJourney140UnknownLeadCreateHasOneLiteralDispatchAndRecoveryOnlyReadsOriginal(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000140"
	const key = "journey140 retained internal spaces"
	raw := "{\n  \"id\":\"" + id + "\", \"name\":\"Native fixture lead\", \"source\":\"manual\"\n}\n"
	originalBody := `{"data":{"id":"` + id + `","confirmed":true,"representation_available":false}}` + "\n"
	var posts, originals, redirects atomic.Int32
	state := "missing"
	denied := false
	mode := "truncated"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/account" {
			fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm_leads:write","crm_leads:read"]}}}`)
			return
		}
		if r.URL.Path == "/should-not-follow" {
			redirects.Add(1)
			t.Error("native writer followed redirect")
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/crm/leads" {
			posts.Add(1)
			sent, _ := io.ReadAll(r.Body)
			if string(sent) != raw || r.Header.Get("Idempotency-Key") != key {
				t.Error("raw bytes/key changed")
			}
			switch mode {
			case "redirect":
				w.Header().Set("Location", "/should-not-follow")
				w.WriteHeader(307)
			case "unavailable":
				w.WriteHeader(503)
				fmt.Fprint(w, `{"error":{"type":"api_error","code":"internal_error","message":"native unavailable"}}`)
			default:
				w.Header().Set("Content-Length", strconv.Itoa(len(originalBody)+8))
				w.WriteHeader(201)
				fmt.Fprint(w, originalBody)
			}
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/v1/crm/leads/create/original" {
			t.Error("unexpected effect/Current fallback")
			w.WriteHeader(404)
			return
		}
		originals.Add(1)
		if r.ContentLength > 0 || len(r.URL.Query()) != 1 || r.URL.Query().Get("original_body_sha256") != client.CrmOriginalBodyHash([]byte(raw)) || r.Header.Get("Idempotency-Key") != key {
			t.Error("recovery changed Original intent")
		}
		if denied {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"error":{"type":"authorization_error","code":"insufficient_scope","message":"native current C7 denied"}}`)
			return
		}
		var receipt any
		if state == "confirmed" {
			receipt = map[string]any{"status": 201, "body": originalBody}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"state": state, "original_body_sha256": client.CrmOriginalBodyHash([]byte(raw)), "original_response": receipt}, "meta": map[string]any{"authorization_version": "current-native-c7"}})
	}))
	defer server.Close()
	args := []string{"crm", "lead", "create", "--source-contracts", "--confirmed", "--data", "-", "--idempotency-key", key}
	// Default Source/confirmation and dry-run each remain zero dispatch.
	_, _, err := runCrmSource139(t, server.URL, raw, "crm", "lead", "create", "--source-contracts", "--data", "-", "--idempotency-key", key)
	if err == nil || posts.Load() != 0 {
		t.Fatal("missing human confirmation dispatched")
	}
	_, _, err = runCrmSource139(t, server.URL, raw, append(args, "--dry-run")...)
	if err != nil || posts.Load() != 0 {
		t.Fatal("dry-run dispatched HTTP")
	}
	for _, factual := range []string{"truncated", "redirect", "unavailable"} {
		mode = factual
		out, diag, err := runCrmSource139(t, server.URL, raw, args...)
		if err == nil || out != "" {
			t.Fatal("unknown write became confirmed")
		}
		for _, private := range []string{key, raw, "fact_test_"} {
			if strings.Contains(diag, private) {
				t.Error("failure exposed original input")
			}
		}
	}
	if posts.Load() != 3 || redirects.Load() != 0 {
		t.Fatal("writer retried or followed redirect")
	}
	recovery := []string{"crm", "lead", "original", "--source-contracts", "--data", "-", "--idempotency-key", key}
	for _, factual := range []string{"missing", "in_progress", "confirmed"} {
		state = factual
		out, _, err := runCrmSource139(t, server.URL, raw, recovery...)
		var payload struct {
			Data struct {
				State    string `json:"state"`
				Response *struct {
					Body string `json:"body"`
				} `json:"original_response"`
			} `json:"data"`
		}
		if err != nil || json.Unmarshal([]byte(out), &payload) != nil || payload.Data.State != factual {
			t.Fatal("Original state replaced")
		}
		if factual == "confirmed" && (payload.Data.Response == nil || payload.Data.Response.Body != originalBody) {
			t.Fatal("original immutable body changed")
		}
		if factual != "confirmed" && payload.Data.Response != nil {
			t.Fatal("uncertainty became receipt")
		}
	}
	denied = true
	out, _, err := runCrmSource139(t, server.URL, raw, recovery...)
	if err == nil || exit.ForError(err) != exit.Perm || out != "" || posts.Load() != 3 || originals.Load() != 4 {
		t.Fatal("current denial used cached recovery or resent")
	}
}

func TestCrmJourney140ActivityFreshCurrentCASPreservesServer409AndRevocationWithoutRepost(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000141"
	const key = "activity140 CAS intent"
	raw := "{\n\"expected_version\":1,\"changes\":{\"title\":\"Human change\"}\n}\n"
	var patches, current atomic.Int32
	denied := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/account" {
			fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm_activities:write","crm_activities:read"]}}}`)
			return
		}
		if r.URL.Path != "/v1/crm/crmactivity/"+id {
			t.Error("non-native route")
			w.WriteHeader(404)
			return
		}
		if r.Method == http.MethodGet {
			current.Add(1)
			if denied {
				w.WriteHeader(403)
				fmt.Fprint(w, `{"error":{"type":"authorization_error","code":"insufficient_scope","message":"current C7 revoked"}}`)
				return
			}
			activity := map[string]any{"id": id, "version": 1, "type": "note", "title": "Native fixture", "body": nil, "owner_id": nil, "team_id": nil, "status": "pending", "due_at": nil, "timezone": "Europe/Madrid", "completed_at": nil, "reminder_at": nil, "task_id": nil, "targets": []any{}}
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"activity": activity, "comments": []any{}, "attachments": []any{}}})
			return
		}
		if r.Method != http.MethodPatch {
			t.Error("CAS command changed method")
			w.WriteHeader(405)
			return
		}
		patches.Add(1)
		sent, _ := io.ReadAll(r.Body)
		if string(sent) != raw || r.Header.Get("Idempotency-Key") != key {
			t.Error("CAS/raw intent changed")
		}
		// Owner changed head after the authorized GET. Only server409 is reported.
		w.WriteHeader(409)
		fmt.Fprint(w, `{"error":{"type":"conflict_error","code":"crm_version_conflict","message":"native CAS changed","param":"expected_version"}}`)
	}))
	defer server.Close()
	args := []string{"crm", "activity", "update", id, "--source-contracts", "--confirmed", "--data", "-", "--idempotency-key", key}
	out, _, err := runCrmSource139(t, server.URL, raw, args...)
	if err == nil || exit.ForError(err) != exit.Conflict || out != "" || patches.Load() != 1 || current.Load() != 1 {
		t.Fatal("CAS409 refreshed or retried mutation")
	}
	denied = true
	out, _, err = runCrmSource139(t, server.URL, raw, args...)
	if err == nil || exit.ForError(err) != exit.Perm || out != "" || patches.Load() != 1 || current.Load() != 2 {
		t.Fatal("revoked Current allowed PATCH or used old head")
	}
}
