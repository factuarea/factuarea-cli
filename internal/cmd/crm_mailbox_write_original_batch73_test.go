package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/exit"
)

const mailboxOriginalID = "019c0511-1111-7111-8111-111111111111"
const mailboxOriginalEffect = "019c0511-1111-7111-8111-222222222222"
const mailboxOriginalConnection = "019c0511-1111-7111-8111-333333333333"
const mailboxOriginalUser = "550e8400-e29b-41d4-a716-446655440000"
const mailboxOriginalKey = "Mailbox:Original:Exact73"

func mailboxOriginalIntent(update bool) map[string]any {
	expected := json.Number("0")
	var connection any
	if update {
		expected = json.Number("9007199254740993")
		connection = mailboxOriginalConnection
	}
	return map[string]any{"expected_version": expected, "provider": "gmail", "connection_id": connection,
		"kind": "personal", "team_id": nil, "allowed_user_ids": []any{mailboxOriginalUser}}
}

func mailboxOriginalJSON(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func mailboxOriginalReceipt(update bool) map[string]any {
	version := json.Number("1")
	if update {
		version = json.Number("9007199254740994")
	}
	return map[string]any{"data": map[string]any{"id": mailboxOriginalID, "effect_id": mailboxOriginalEffect,
		"connection_id": mailboxOriginalConnection, "version": version, "availability": "disconnected",
		"provider": "gmail", "confirmed_at": "2026-10-10T11:00:00Z"}}
}

func mailboxOriginalArgs(update bool, extra ...string) []string {
	args := []string{"crm", "mailboxes", "recover-create"}
	if update {
		args[2] = "recover-update"
		args = append(args, mailboxOriginalID)
	}
	return append(args, extra...)
}

func TestMailboxWriteOriginalClosedGetPreservesOriginalBytesAndFirstReceipt(t *testing.T) {
	for _, update := range []bool{false, true} {
		t.Run(fmt.Sprint(update), func(t *testing.T) {
			intent := mailboxOriginalIntent(update)
			wire := "\n\t" + strings.Replace(mailboxOriginalJSON(t, intent), `"provider"`, `"\u0070rovider"`, 1) + "\n"
			ack := mailboxOriginalJSON(t, mailboxOriginalReceipt(update))
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fact_test_aaaaaaaaaaaaaaaaaaaaaaaa" {
					t.Error("configured staff credential changed")
				}
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "crm:write")
					return
				}
				calls.Add(1)
				path := mailboxWriteOriginalPath + "/write-original"
				if update {
					path = mailboxWriteOriginalPath + "/" + mailboxOriginalID + "/write-original"
				}
				body, _ := io.ReadAll(r.Body)
				if r.Method != "GET" || r.URL.Path != path || len(body) != 0 || len(r.URL.Query()) != 1 || r.URL.Query().Get("original_wire") != wire || strings.Contains(r.URL.RawQuery, "&") {
					t.Error("exact original GET carrier changed")
				}
				if !reflect.DeepEqual(r.Header.Values("Idempotency-Key"), []string{mailboxOriginalKey}) {
					t.Error("raw single original key changed")
				}
				for _, header := range []string{"Content-Type", "X-Company-Id", "X-Tenant-Id"} {
					if r.Header.Get(header) != "" {
						t.Errorf("invented header %s", header)
					}
				}
				_, _ = w.Write([]byte(ack))
			}))
			defer s.Close()
			out, stderr, err := crmRun(t, s, mailboxOriginalArgs(update, "--original-wire", wire, "--idempotency-key", mailboxOriginalKey, "--json")...)
			if err != nil || calls.Load() != 1 || stderr != "" || strings.TrimSpace(out) != ack || strings.Contains(out, mailboxOriginalKey) || strings.Contains(out, "fingerprint") {
				t.Fatalf("original receipt changed: calls=%d out=%q stderr=%q err=%v", calls.Load(), out, stderr, err)
			}
		})
	}
}

func TestMailboxWriteOriginalExplicitSourcesAndMaximumBytes(t *testing.T) {
	for _, source := range []string{"files", "stdin", "maximum-bytes", "unicode-key", "maximum-key"} {
		t.Run(source, func(t *testing.T) {
			wire := "\n" + mailboxOriginalJSON(t, mailboxOriginalIntent(false)) + "\n"
			key := mailboxOriginalKey
			if source == "maximum-bytes" {
				wire += strings.Repeat(" ", 65536-len(wire))
			}
			if source == "unicode-key" {
				key = "Clávé:Original73"
			}
			if source == "maximum-key" {
				key = strings.Repeat("á", 256)
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "crm:write")
					return
				}
				if r.URL.Query().Get("original_wire") != wire || r.Header.Get("Idempotency-Key") != key {
					t.Error("original source was trimmed or reserialized")
				}
				_, _ = w.Write([]byte(mailboxOriginalJSON(t, mailboxOriginalReceipt(false))))
			}))
			defer s.Close()
			t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
			t.Setenv("FACTUAREA_BASE_URL", s.URL)
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			args := mailboxOriginalArgs(false, "--idempotency-key", key, "--original-wire", wire, "--json")
			if source == "files" {
				dir := t.TempDir()
				keyFile, wireFile := filepath.Join(dir, "key"), filepath.Join(dir, "wire.json")
				if os.WriteFile(keyFile, []byte(key), 0600) != nil || os.WriteFile(wireFile, []byte(wire), 0600) != nil {
					t.Fatal("fixture file failed")
				}
				args = mailboxOriginalArgs(false, "--idempotency-key-file", keyFile, "--original-wire-file", wireFile, "--json")
			} else if source == "stdin" {
				args = mailboxOriginalArgs(false, "--idempotency-key", key, "--original-wire", "-", "--json")
			}
			root := NewRootCmd()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetIn(strings.NewReader(wire))
			root.SetArgs(args)
			if err := root.Execute(); err != nil || out.Len() == 0 {
				t.Fatalf("explicit original source failed: %v", err)
			}
		})
	}
}

func TestMailboxWriteOriginalOwnerAcceptedBoundariesPreserveInt64AndReferences(t *testing.T) {
	for _, boundary := range []string{"maximum int64 receipt", "shared graph", "create existing connection", "historical user uuid", "empty users"} {
		t.Run(boundary, func(t *testing.T) {
			update := boundary == "maximum int64 receipt"
			intent := mailboxOriginalIntent(update)
			fixture := mailboxOriginalReceipt(update)
			data := fixture["data"].(map[string]any)
			switch boundary {
			case "maximum int64 receipt":
				intent["expected_version"] = json.Number("9223372036854775806")
				data["version"] = json.Number("9223372036854775807")
			case "shared graph":
				intent["kind"], intent["team_id"], intent["provider"] = "shared", mailboxOriginalEffect, "microsoft_graph"
				data["provider"], data["availability"] = "microsoft_graph", "active"
			case "create existing connection":
				intent["connection_id"] = mailboxOriginalConnection
			case "historical user uuid":
				intent["allowed_user_ids"] = []any{"6ba7b810-9dad-11d1-80b4-00c04fd430c8"}
			case "empty users":
				intent["allowed_user_ids"] = []any{}
			}
			wire, ack := mailboxOriginalJSON(t, intent), mailboxOriginalJSON(t, fixture)
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "crm:write")
					return
				}
				if r.URL.Query().Get("original_wire") != wire {
					t.Error("owner reference or int64 CAS changed")
				}
				_, _ = w.Write([]byte(ack))
			}))
			defer s.Close()
			out, _, err := crmRun(t, s, mailboxOriginalArgs(update, "--original-wire", wire, "--idempotency-key", mailboxOriginalKey, "--json")...)
			if err != nil || strings.TrimSpace(out) != ack {
				t.Fatalf("native boundary changed: out=%q err=%v", out, err)
			}
		})
	}
}

func TestMailboxWriteOriginalRejectsInvalidInputBeforeCredentialOrNetwork(t *testing.T) {
	valid := mailboxOriginalJSON(t, mailboxOriginalIntent(false))
	cases := map[string][]string{
		"no key":                mailboxOriginalArgs(false, "--original-wire", valid),
		"empty key":             mailboxOriginalArgs(false, "--original-wire", valid, "--idempotency-key", ""),
		"long key":              mailboxOriginalArgs(false, "--original-wire", valid, "--idempotency-key", strings.Repeat("A", 513)),
		"leading space":         mailboxOriginalArgs(false, "--original-wire", valid, "--idempotency-key", " key"),
		"embedded space":        mailboxOriginalArgs(false, "--original-wire", valid, "--idempotency-key", "a b"),
		"control key":           mailboxOriginalArgs(false, "--original-wire", valid, "--idempotency-key", "a\x7f"),
		"two key sources":       mailboxOriginalArgs(false, "--original-wire", valid, "--idempotency-key", mailboxOriginalKey, "--idempotency-key-file", "absent"),
		"no wire":               mailboxOriginalArgs(false, "--idempotency-key", mailboxOriginalKey),
		"two wire sources":      mailboxOriginalArgs(false, "--original-wire", valid, "--original-wire-file", "absent", "--idempotency-key", mailboxOriginalKey),
		"missing key file":      mailboxOriginalArgs(false, "--original-wire", valid, "--idempotency-key-file", filepath.Join(t.TempDir(), "missing")),
		"missing wire file":     mailboxOriginalArgs(false, "--original-wire-file", filepath.Join(t.TempDir(), "missing"), "--idempotency-key", mailboxOriginalKey),
		"extra query flag":      mailboxOriginalArgs(false, "--original-wire", valid, "--idempotency-key", mailboxOriginalKey, "--company_id", "99"),
		"body flag":             mailboxOriginalArgs(false, "--original-wire", valid, "--idempotency-key", mailboxOriginalKey, "--data", "{}"),
		"update no target":      {"crm", "mailboxes", "recover-update", "--original-wire", valid, "--idempotency-key", mailboxOriginalKey},
		"update invalid target": {"crm", "mailboxes", "recover-update", "../foreign", "--original-wire", valid, "--idempotency-key", mailboxOriginalKey},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
			defer s.Close()
			out, _, err := crmRun(t, s, args...)
			if out != "" || exit.ForError(err) != exit.Usage || calls.Load() != 0 {
				t.Fatalf("invalid input reached native request: calls=%d out=%q err=%v", calls.Load(), out, err)
			}
		})
	}
}

func TestMailboxWriteOriginalRejectsClosedWireAndAmbiguousCAS(t *testing.T) {
	mutations := map[string]func(map[string]any){
		"unknown field":         func(v map[string]any) { v["company_id"] = json.Number("7") },
		"missing null":          func(v map[string]any) { delete(v, "team_id") },
		"string version":        func(v map[string]any) { v["expected_version"] = "0" },
		"fraction version":      func(v map[string]any) { v["expected_version"] = json.Number("0.0") },
		"create nonzero CAS":    func(v map[string]any) { v["expected_version"] = json.Number("1") },
		"overflow CAS":          func(v map[string]any) { v["expected_version"] = json.Number("9223372036854775808") },
		"unknown provider":      func(v map[string]any) { v["provider"] = "smtp" },
		"unknown kind":          func(v map[string]any) { v["kind"] = "company" },
		"personal team":         func(v map[string]any) { v["team_id"] = mailboxOriginalID },
		"wrong connection UUID": func(v map[string]any) { v["connection_id"] = mailboxOriginalUser },
		"null users":            func(v map[string]any) { v["allowed_user_ids"] = nil },
		"duplicate users": func(v map[string]any) {
			v["allowed_user_ids"] = []any{mailboxOriginalUser, strings.ToUpper(mailboxOriginalUser)}
		},
		"numeric user": func(v map[string]any) { v["allowed_user_ids"] = []any{json.Number("7")} },
		"too many users": func(v map[string]any) {
			users := []any{}
			for i := 0; i < 1001; i++ {
				users = append(users, fmt.Sprintf("550e8400-e29b-41d4-a716-%012x", i))
			}
			v["allowed_user_ids"] = users
		},
	}
	wires := map[string]string{"empty": "", "array": "[]", "trailing JSON": "{}{}", "oversized": strings.Repeat(" ", 65537),
		"duplicate escaped field": strings.TrimSuffix(mailboxOriginalJSON(t, mailboxOriginalIntent(false)), "}") + `,"\u0070rovider":"gmail"}`,
		"invalid utf8":            "{\xff}", "malformed": "{private-body"}
	for name, mutate := range mutations {
		value := mailboxOriginalIntent(false)
		mutate(value)
		wires[name] = mailboxOriginalJSON(t, value)
	}
	for name, wire := range wires {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
			defer s.Close()
			out, _, err := crmRun(t, s, mailboxOriginalArgs(false, "--original-wire", wire, "--idempotency-key", mailboxOriginalKey)...)
			if exit.ForError(err) != exit.Usage || calls.Load() != 0 || out != "" || (wire == "{private-body" && strings.Contains(err.Error(), "private-body")) {
				t.Fatalf("closed original changed: calls=%d err=%v", calls.Load(), err)
			}
		})
	}
	for _, field := range []string{"connection_id", "expected_version"} {
		t.Run("update "+field, func(t *testing.T) {
			intent := mailboxOriginalIntent(true)
			intent[field] = nil
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
			defer s.Close()
			_, _, err := crmRun(t, s, mailboxOriginalArgs(true, "--original-wire", mailboxOriginalJSON(t, intent), "--idempotency-key", mailboxOriginalKey)...)
			if exit.ForError(err) != exit.Usage || calls.Load() != 0 {
				t.Fatalf("update admitted missing original %s: %v", field, err)
			}
		})
	}
}

func TestMailboxWriteOriginalNativeErrorsPreserve409AndUnconfirmedWithoutFallback(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		status, code              int
		kind, nativeCode, subcode string
	}{
		{"revoked key", 401, exit.Auth, "authentication_error", "credential_revoked", ""},
		{"revoked context", 403, exit.Perm, "authorization_error", "crm_context_revoked", "revoked"},
		{"missing original", 404, exit.NotFound, "not_found_error", "resource_not_found", "conversation_not_found"},
		{"different exact wire", 409, exit.Conflict, "idempotency_error", "idempotency_key_reused", "conversation_effect_conflict"},
		{"unconfirmed original", 409, exit.Conflict, "conflict_error", "crm_effect_unconfirmed", "unconfirmed"},
		{"invalid original", 422, exit.Validation, "invalid_request_error", "business_rule_violation", "conversation_invalid_field"},
		{"rate limit", 429, exit.RateLimit, "rate_limit_error", "rate_limit_exceeded", ""},
		{"native uncertain store", 500, exit.Server, "api_error", "internal_error", ""},
		{"producer unavailable", 503, exit.Server, "service_unavailable_error", "service_unavailable", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "crm:write")
					return
				}
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprintf(w, `{"error":{"type":%q,"code":%q,"subcode":%q,"message":"Rechazo nativo","request_id":"request73","details":{"version":9007199254740993}}}`, tc.kind, tc.nativeCode, tc.subcode)
			}))
			defer s.Close()
			out, stderr, err := crmRun(t, s, mailboxOriginalArgs(false, "--original-wire", mailboxOriginalJSON(t, mailboxOriginalIntent(false)), "--idempotency-key", mailboxOriginalKey, "--json")...)
			var native *apierr.APIError
			if calls.Load() != 1 || out != "" || exit.ForError(err) != tc.code || !errors.As(err, &native) || native.StatusCode != tc.status || native.Code != tc.nativeCode || native.Subcode != tc.subcode || native.RequestID != "request73" || !strings.Contains(stderr, "9007199254740993") || strings.Contains(stderr, mailboxOriginalKey) {
				t.Fatalf("native denial changed or retried: calls=%d out=%q stderr=%q err=%v", calls.Load(), out, stderr, err)
			}
		})
	}
}

func TestMailboxWriteOriginalReadScopeDoesNotGrantRecovery(t *testing.T) {
	for _, scope := range []string{"conversations:read", "crm:read"} {
		t.Run(scope, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/v1/account" {
					t.Error("read scope reached original")
				}
				crmAccount(w, scope)
			}))
			defer s.Close()
			out, _, err := crmRun(t, s, mailboxOriginalArgs(false, "--original-wire", mailboxOriginalJSON(t, mailboxOriginalIntent(false)), "--idempotency-key", mailboxOriginalKey)...)
			if calls.Load() != 1 || out != "" || exit.ForError(err) != exit.Perm {
				t.Fatalf("read scope recovered a write: %v", err)
			}
		})
	}
}

func TestMailboxWriteOriginalAccountRevocationStopsBeforeReceipt(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/account" {
			t.Error("revoked native credential read original")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","code":"credential_revoked","message":"Credencial revocada"}}`))
	}))
	defer s.Close()
	out, stderr, err := crmRun(t, s, mailboxOriginalArgs(false, "--original-wire", mailboxOriginalJSON(t, mailboxOriginalIntent(false)), "--idempotency-key", mailboxOriginalKey, "--json")...)
	if calls.Load() != 1 || out != "" || exit.ForError(err) != exit.Auth || !strings.Contains(stderr, "credential_revoked") {
		t.Fatalf("current credential bypassed: calls=%d err=%v", calls.Load(), err)
	}
}

func TestMailboxWriteOriginalWrongReceiptsNeverAcknowledgeCurrentHead(t *testing.T) {
	for _, field := range []string{"id", "connection_id", "version", "provider", "effect_id", "availability", "confirmed_at", "extra", "missing"} {
		t.Run(field, func(t *testing.T) {
			fixture := mailboxOriginalReceipt(true)
			data := fixture["data"].(map[string]any)
			switch field {
			case "id", "connection_id":
				data[field] = mailboxOriginalEffect
			case "version":
				data[field] = json.Number("9007199254740995")
			case "provider":
				data[field] = "microsoft_graph"
			case "effect_id":
				data[field] = mailboxOriginalUser
			case "availability":
				data[field] = "pending"
			case "confirmed_at":
				data[field] = "2026-02-30T11:00:00Z"
			case "extra":
				data["fingerprint"] = "private"
			case "missing":
				delete(data, "effect_id")
			}
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "crm:write")
					return
				}
				calls.Add(1)
				_, _ = w.Write([]byte(mailboxOriginalJSON(t, fixture)))
			}))
			defer s.Close()
			out, _, err := crmRun(t, s, mailboxOriginalArgs(true, "--original-wire", mailboxOriginalJSON(t, mailboxOriginalIntent(true)), "--idempotency-key", mailboxOriginalKey, "--json")...)
			var native *apierr.APIError
			if out != "" || calls.Load() != 1 || exit.ForError(err) != exit.Server || !errors.As(err, &native) || native.Code != "invalid_native_response" {
				t.Fatalf("wrong original acknowledged: %q %v", out, err)
			}
		})
	}
	for _, wire := range []string{"null", `{"data":null}`, `{"data":null,"data":{}}`, "{}{}"} {
		t.Run(wire, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "crm:write")
					return
				}
				_, _ = w.Write([]byte(wire))
			}))
			defer s.Close()
			out, _, err := crmRun(t, s, mailboxOriginalArgs(false, "--original-wire", mailboxOriginalJSON(t, mailboxOriginalIntent(false)), "--idempotency-key", mailboxOriginalKey)...)
			if out != "" || exit.ForError(err) != exit.Server {
				t.Fatalf("malformed original acknowledged: %q %v", out, err)
			}
		})
	}
}

func TestMailboxWriteOriginalTransportAndRedirectNeverRetryOrExposeOriginal(t *testing.T) {
	for _, response := range []string{"disconnect", "redirect"} {
		t.Run(response, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "crm:write")
					return
				}
				calls.Add(1)
				if response == "redirect" {
					w.Header().Set("Location", "/forbidden-fallback")
					w.WriteHeader(307)
					return
				}
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
			}))
			defer s.Close()
			wire := mailboxOriginalJSON(t, mailboxOriginalIntent(false))
			out, stderr, err := crmRun(t, s, mailboxOriginalArgs(false, "--original-wire", wire, "--idempotency-key", mailboxOriginalKey, "--json")...)
			code := exit.Network
			if response == "redirect" {
				code = exit.Validation
			}
			if out != "" || calls.Load() != 1 || exit.ForError(err) != code || strings.Contains(stderr, wire) || strings.Contains(stderr, "original_wire=") || strings.Contains(stderr, mailboxOriginalKey) {
				t.Fatalf("recovery retried or leaked original: calls=%d stderr=%q err=%v", calls.Load(), stderr, err)
			}
		})
	}
}

func TestMailboxWriteOriginalDiscoveryAndHelpKeepCurrentContractsOffline(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "unusable")
	t.Setenv("FACTUAREA_BASE_URL", "http://127.0.0.1:1")
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"commands", "--json"})
	if root.Execute() != nil {
		t.Fatal("offline discovery failed")
	}
	var entries []manifestEntry
	if json.Unmarshal(out.Bytes(), &entries) != nil {
		t.Fatal("invalid discovery")
	}
	byID := map[string]manifestEntry{}
	for _, entry := range entries {
		if _, exists := byID[entry.OperationID]; exists {
			t.Fatalf("duplicate discovery operation %s", entry.OperationID)
		}
		byID[entry.OperationID] = entry
	}
	for _, expected := range mailboxWriteOriginalManifest() {
		entry, exists := byID[expected.OperationID]
		actualJSON, _ := json.Marshal(entry)
		expectedJSON, _ := json.Marshal(expected)
		if !exists || !bytes.Equal(actualJSON, expectedJSON) || entry.Mutating || entry.Irreversible || entry.RequiresConfirmation || entry.RequestSchema != nil || entry.MaxBodyBytes != 0 || entry.RequiredScope != "crm:write" || !entry.OriginalRecovery || !entry.IdempotencyRequired || len(entry.QuerySchemas) != 1 {
			t.Fatalf("native recovery discovery changed: %+v", entry)
		}
	}
	previous := append(nativeOwnerManifest(), taskTimeManifest()...)
	previous = append(previous, insightsManifest()...)
	previous = append(previous, serviceFormReferencesManifest()...)
	for _, expected := range previous {
		actualJSON, _ := json.Marshal(byID[expected.OperationID])
		expectedJSON, _ := json.Marshal(expected)
		if !bytes.Equal(actualJSON, expectedJSON) {
			t.Fatalf("previous owner discovery changed: %s", expected.OperationID)
		}
	}
	for _, update := range []bool{false, true} {
		root := NewRootCmd()
		out.Reset()
		root.SetOut(&out)
		root.SetArgs(mailboxOriginalArgs(update, "--help"))
		if err := root.Execute(); err != nil || !strings.Contains(out.String(), "original-wire-file") || !strings.Contains(out.String(), "crm:write") {
			t.Fatalf("offline native help failed: %v", err)
		}
	}
}
