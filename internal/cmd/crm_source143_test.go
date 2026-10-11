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

func TestCrmSource143ApplyUsesCurrentThenLiteral202AndUnknownRecoversOriginalWithoutResend(t *testing.T) {
	const run = "01900000-0000-7000-8000-000000000143"
	const blob = "01900000-0000-7000-8000-000000000144"
	const effect = "01900000-0000-7000-8000-000000000145"
	const key = "import143 original internal spaces"
	hash := strings.Repeat("a", 64)
	raw := "{\n \"expected_version\":1,\"preview_hash\":\"" + hash + "\",\"effect_uuid\":\"" + effect + "\",\"mode\":\"import\",\"confirmed\":true\n}\n"
	data := map[string]any{"id": run, "source_blob_id": blob, "entity_type": "lead", "version": 1, "state": "planned", "preview_hash": hash, "counts": map[string]any{"total": 1}, "blocking_codes": []any{}, "progress": map[string]any{"total": 1, "processed": 0, "pending": 1}}
	originalJSON, _ := json.Marshal(map[string]any{"data": data})
	originalBody := string(originalJSON) + "\n"
	var posts, heads, originals atomic.Int32
	truncated := false
	state := "confirmed"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/account" {
			fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm_imports:read","crm_imports:write"]}}}`)
			return
		}
		if r.URL.Path == "/v1/crm/imports/"+run && r.Method == http.MethodGet {
			heads.Add(1)
			head := map[string]any{}
			for k, v := range data {
				head[k] = v
			}
			head["errors"] = map[string]any{"version": 1, "items": []any{}, "has_more": false, "next_row": nil, "next_cursor": nil}
			json.NewEncoder(w).Encode(map[string]any{"data": head})
			return
		}
		if r.URL.Path == "/v1/crm/imports/"+run+"/apply" && r.Method == http.MethodPost {
			posts.Add(1)
			sent, _ := io.ReadAll(r.Body)
			if string(sent) != raw || r.Header.Get("Idempotency-Key") != key || heads.Load() != posts.Load() {
				t.Error("Apply dispatched before Current or changed raw/key")
			}
			if truncated {
				w.Header().Set("Content-Length", strconv.Itoa(len(originalBody)+8))
			}
			w.WriteHeader(202)
			fmt.Fprint(w, originalBody)
			return
		}
		if r.URL.Path == "/v1/crm/imports/"+run+"/apply/original" && r.Method == http.MethodGet {
			originals.Add(1)
			if r.ContentLength > 0 || r.Header.Get("Idempotency-Key") != key || r.URL.Query().Get("original_body_sha256") != client.CrmOriginalBodyHash([]byte(raw)) {
				t.Error("Original request changed or uploaded")
			}
			var receipt any
			if state == "confirmed" {
				receipt = map[string]any{"status": 202, "body": originalBody}
			}
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"state": state, "authorization_version": "current-native-c7", "original_body_sha256": client.CrmOriginalBodyHash([]byte(raw)), "original_response": receipt}})
			return
		}
		t.Error("undeclared recovery route/method")
		w.WriteHeader(404)
	}))
	defer server.Close()
	args := []string{"crm", "imports", "source-apply", run, "--source-contracts", "--confirmed", "--data", "-", "--idempotency-key", key}
	out, _, err := runCrmSource139(t, server.URL, raw, args...)
	var response struct {
		Data struct {
			Progress struct {
				Pending int `json:"pending"`
			} `json:"progress"`
		} `json:"data"`
	}
	if err != nil || json.Unmarshal([]byte(out), &response) != nil || response.Data.Progress.Pending != 1 || posts.Load() != 1 {
		t.Fatal("native202/pending state replaced")
	}
	truncated = true
	out, _, err = runCrmSource139(t, server.URL, raw, args...)
	if err == nil || exit.ForError(err) != exit.Network || out != "" || posts.Load() != 2 {
		t.Fatal("unknown202 retried or confirmed")
	}
	recovery := []string{"crm", "imports", "source-original", run, "--source-contracts", "--data", "-", "--idempotency-key", key}
	for _, factual := range []string{"missing", "in_progress", "confirmed"} {
		state = factual
		out, _, err = runCrmSource139(t, server.URL, raw, recovery...)
		var payload struct {
			Data struct {
				State    string `json:"state"`
				Response *struct {
					Body   string `json:"body"`
					Status int    `json:"status"`
				} `json:"original_response"`
			} `json:"data"`
		}
		if err != nil || json.Unmarshal([]byte(out), &payload) != nil || payload.Data.State != factual {
			t.Fatal("Original state fabricated")
		}
		if factual == "confirmed" && (payload.Data.Response == nil || payload.Data.Response.Body != originalBody || payload.Data.Response.Status != 202) {
			t.Fatal("original202 bytes changed")
		}
	}
	if originals.Load() != 3 || posts.Load() != 2 || heads.Load() != 2 {
		t.Fatal("Original read Current or resent Apply")
	}
}

func TestCrmSource143RestoreReceiptCASConflictRevocationAndUnknownNeverUseCurrentAsOriginal(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000146"
	const key = "restore143 retained intent"
	raw := "{\n\"expected_version\":3,\"confirmed\":true\n}\n"
	var posts, heads atomic.Int32
	mode := "confirmed"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/account" {
			fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm_documents:read","crm_documents:write"]}}}`)
			return
		}
		if r.URL.Path == "/v1/crm/documents/"+id && r.Method == http.MethodGet {
			heads.Add(1)
			if mode == "revoked" {
				w.WriteHeader(403)
				fmt.Fprint(w, `{"error":{"type":"authorization_error","code":"insufficient_scope","message":"current C7 revoked"}}`)
				return
			}
			fmt.Fprint(w, `{"data":{"id":"`+id+`","version":3,"archived_at":"2026-10-11T00:00:00Z"}}`)
			return
		}
		if r.URL.Path != "/v1/crm/documents/"+id+"/restore" || r.Method != http.MethodPost {
			t.Error("invented Original endpoint/readback")
			w.WriteHeader(404)
			return
		}
		posts.Add(1)
		sent, _ := io.ReadAll(r.Body)
		if string(sent) != raw || r.Header.Get("Idempotency-Key") != key {
			t.Error("restore intent changed")
		}
		if mode == "conflict" {
			w.WriteHeader(409)
			fmt.Fprint(w, `{"error":{"type":"conflict_error","code":"crm_version_conflict","message":"native head changed"}}`)
			return
		}
		receipt := `{"data":{"id":"` + id + `","version":4}}` + "\n"
		if mode == "unknown" {
			w.Header().Set("Content-Length", strconv.Itoa(len(receipt)+8))
		}
		fmt.Fprint(w, receipt)
	}))
	defer server.Close()
	args := []string{"crm", "documents", "restore", id, "--source-contracts", "--confirmed", "--data", "-", "--idempotency-key", key}
	out, _, err := runCrmSource139(t, server.URL, raw, args...)
	var response struct {
		Data struct {
			Version int `json:"version"`
		} `json:"data"`
	}
	if err != nil || json.Unmarshal([]byte(out), &response) != nil || response.Data.Version != 4 || posts.Load() != 1 || heads.Load() != 1 {
		t.Fatal("native receipt200 replaced by Current")
	}
	mode = "conflict"
	out, _, err = runCrmSource139(t, server.URL, raw, args...)
	if err == nil || exit.ForError(err) != exit.Conflict || out != "" || posts.Load() != 2 || heads.Load() != 2 {
		t.Fatal("server409 retried/rebased")
	}
	mode = "revoked"
	out, _, err = runCrmSource139(t, server.URL, raw, args...)
	if err == nil || exit.ForError(err) != exit.Perm || out != "" || posts.Load() != 2 || heads.Load() != 3 {
		t.Fatal("C7 revoked still wrote")
	}
	mode = "unknown"
	out, _, err = runCrmSource139(t, server.URL, raw, args...)
	if err == nil || exit.ForError(err) != exit.Network || out != "" || posts.Load() != 3 || heads.Load() != 4 {
		t.Fatal("UNKNOWN inferred Current or resent POST")
	}
}
