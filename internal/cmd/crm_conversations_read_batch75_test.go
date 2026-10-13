package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/exit"
)

const conversationReadC7 = "current-C7:opaque+/"
const conversationReadCursor = "opaque+/%= native cursor"

func conversationReadFixture(detail bool) map[string]any {
	head := map[string]any{"id": crmTestID, "version": json.Number("9007199254740993"), "mailbox_id": crmTestOtherID, "subject": "Consulta literal", "status": "active", "owner_id": "F47AC10B-58CC-4372-A567-0E02B2C3D479", "team_id": nil, "last_message_at": nil, "links": []any{}}
	data := map[string]any{"items": []any{head}, "next_cursor": nil, "total": 1, "active_count": 1, "archived_count": 0, "limit": 20}
	if detail {
		message := map[string]any{"id": crmTestOtherID, "conversation_id": crmTestID, "version": 1, "direction": "outbound", "sender": map[string]any{"email": "private@example.test", "name": nil}, "recipients": []any{map[string]any{"email": "recipient@example.test", "name": nil, "kind": "to"}}, "subject": "Texto privado", "body": "<img src=\"https://remote.invalid/pixel\"> texto literal", "body_format": "plain_text", "composition_state": "send_intent", "delivery_status": "unconfirmed", "submitted_at": nil}
		attachment := map[string]any{"id": "019c0511-1111-7111-8111-333333333333", "conversation_id": crmTestID, "message_id": crmTestOtherID, "version": 1, "filename": "consulta.txt", "mime": "text/plain", "size": 7, "status": "quarantine"}
		data = map[string]any{"conversation": head, "messages": []any{message}, "attachments": []any{attachment}, "next_cursor": nil, "limit": 20}
	}
	readable := make([]any, len(conversationReadFields))
	for i, field := range conversationReadFields {
		readable[i] = field
	}
	return map[string]any{"data": data, "meta": map[string]any{"authorization_version": conversationReadC7, "readable_fields": readable, "filterable_fields": []any{"mailbox_uuid", "status", "subject", "links"}, "sortable_fields": []any{"uuid", "subject", "last_message_at"}}}
}
func conversationReadWire(value any) string { raw, _ := json.Marshal(value); return string(raw) }
func conversationReadArgs(detail bool, extra ...string) []string {
	args := []string{"crm", "conversations", "list"}
	if detail {
		args = []string{"crm", "conversations", "get", crmTestID}
	}
	return append(append(args, extra...), "--json")
}
func conversationReadHeadFixture(value map[string]any, detail bool) map[string]any {
	data := value["data"].(map[string]any)
	if detail {
		return data["conversation"].(map[string]any)
	}
	return data["items"].([]any)[0].(map[string]any)
}
func conversationReadDropField(value map[string]any, field string) {
	meta := value["meta"].(map[string]any)
	kept := []any{}
	for _, item := range meta["readable_fields"].([]any) {
		if item != field {
			kept = append(kept, item)
		}
	}
	meta["readable_fields"] = kept
}

func TestConversationReadNativeListAndDetail(t *testing.T) {
	for _, detail := range []bool{false, true} {
		t.Run(map[bool]string{false: "list", true: "detail"}[detail], func(t *testing.T) {
			fixture := conversationReadFixture(detail)
			calls := atomic.Int32{}
			filters := "{ \"state\" : \"active\", \"search\" : \"café + % /? ü\" }"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "conversations:read", "crm:read")
					return
				}
				calls.Add(1)
				expected := conversationReadPath
				if detail {
					expected += "/" + crmTestID
				}
				body, _ := io.ReadAll(r.Body)
				if r.Method != "GET" || r.URL.Path != expected || len(body) != 0 || r.Header.Get("Authorization") == "" || r.Header.Get("Idempotency-Key") != "" {
					t.Errorf("native request changed %s %s", r.Method, r.URL)
				}
				if r.URL.Query().Get("cursor") != conversationReadCursor || r.URL.Query().Get("limit") != "7" {
					t.Error("exact native cursor/limit changed")
				}
				if !detail && r.URL.Query().Get("filters") != filters {
					t.Error("filter JSON bytes changed")
				}
				if detail && len(r.URL.Query()) != 2 {
					t.Error("detail invented list filters")
				}
				fixture["data"].(map[string]any)["limit"] = 7
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, conversationReadWire(fixture))
			}))
			defer server.Close()
			extra := []string{"--cursor", conversationReadCursor, "--limit", "7"}
			if !detail {
				extra = append(extra, "--filters", filters)
			}
			out, stderr, err := crmRun(t, server, conversationReadArgs(detail, extra...)...)
			if err != nil || calls.Load() != 1 {
				t.Fatalf("native read %d %v %s", calls.Load(), err, stderr)
			}
			if !strings.Contains(out, "9007199254740993") || !strings.Contains(out, `"authorization_version":"current-C7:opaque+/"`) {
				t.Fatalf("integer/C7 changed %s", out)
			}
			if detail && (!strings.Contains(out, `"delivery_status":"unconfirmed"`) || !strings.Contains(out, `"submitted_at":null`) || !strings.Contains(out, `"status":"quarantine"`)) {
				t.Fatal("native uncertainty or attachment state changed")
			}
		})
	}
}

func TestConversationReadAllNativePagesAndAuthority(t *testing.T) {
	for _, detail := range []bool{false, true} {
		for _, failure := range []string{"", "c7", "mask", "cycle", "403"} {
			t.Run(map[bool]string{false: "list", true: "detail"}[detail]+"/"+failure, func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v1/account" {
						crmAccount(w, "*")
						return
					}
					n := calls.Add(1)
					value := conversationReadFixture(detail)
					data := value["data"].(map[string]any)
					if r.URL.Query().Get("limit") != "" {
						t.Error("server default invented in request")
					}
					if n == 1 {
						data["next_cursor"] = conversationReadCursor
						if !detail {
							data["total"] = 2
							data["active_count"] = 2
						}
					}
					if n == 2 {
						if r.URL.Query().Get("cursor") != conversationReadCursor {
							t.Error("native next cursor changed")
						}
						if failure == "403" {
							w.WriteHeader(403)
							io.WriteString(w, `{"error":{"type":"authorization_error","code":"crm_access_denied","message":"Revocado"}}`)
							return
						}
						if failure == "c7" {
							value["meta"].(map[string]any)["authorization_version"] = "changed-C7"
						}
						if failure == "mask" {
							value["meta"].(map[string]any)["sortable_fields"] = []any{"uuid"}
						}
						if failure == "cycle" {
							data["next_cursor"] = conversationReadCursor
							if !detail {
								data["total"] = 2
								data["active_count"] = 2
							}
						}
						if !detail {
							conversationReadHeadFixture(value, false)["id"] = crmTestOtherID
						}
					}
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, conversationReadWire(value))
				}))
				defer server.Close()
				out, stderr, err := crmRun(t, server, conversationReadArgs(detail, "--all")...)
				if calls.Load() != 2 {
					t.Fatalf("page count %d", calls.Load())
				}
				if failure != "" {
					if err == nil || out != "" {
						t.Fatalf("failed pages leaked stdout %v %s", err, out)
					}
					if failure == "403" {
						if exit.ForError(err) != exit.Perm {
							t.Fatal(err)
						}
					} else {
						var native *apierr.APIError
						if !errors.As(err, &native) || native.StatusCode != 502 || native.Code != "invalid_native_response" {
							t.Fatalf("invalid page error %v %s", err, stderr)
						}
					}
					return
				}
				var pages []map[string]any
				if err != nil || json.Unmarshal([]byte(out), &pages) != nil || len(pages) != 2 {
					t.Fatalf("native pages lost %v %s %s", err, out, stderr)
				}
				for _, page := range pages {
					if page["meta"].(map[string]any)["authorization_version"] != conversationReadC7 {
						t.Fatal("page metadata lost")
					}
				}
			})
		}
	}
}

func TestConversationReadMasksStayNullAndEmpty(t *testing.T) {
	for _, detail := range []bool{false, true} {
		t.Run(map[bool]string{false: "list", true: "detail"}[detail], func(t *testing.T) {
			value := conversationReadFixture(detail)
			head := conversationReadHeadFixture(value, detail)
			for public, field := range map[string]string{"subject": "subject", "owner_id": "owner_uuid", "team_id": "team_uuid", "last_message_at": "last_message_at"} {
				head[public] = nil
				conversationReadDropField(value, field)
			}
			conversationReadDropField(value, "links")
			if detail {
				for _, field := range []string{"messages", "attachments", "sender", "recipients", "body", "delivery_status"} {
					conversationReadDropField(value, field)
				}
				value["data"].(map[string]any)["messages"] = []any{}
				value["data"].(map[string]any)["attachments"] = []any{}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "*")
					return
				}
				io.WriteString(w, conversationReadWire(value))
			}))
			defer server.Close()
			out, stderr, err := crmRun(t, server, conversationReadArgs(detail)...)
			if err != nil || !strings.Contains(out, `"subject":null`) || strings.Contains(out, "private@example.test") {
				t.Fatalf("mask changed %v %s %s", err, out, stderr)
			}
			if detail && (!strings.Contains(out, `"messages":[]`) || !strings.Contains(out, `"attachments":[]`)) {
				t.Fatal("masked arrays defaulted")
			}
		})
	}
}

func TestConversationReadRejectsInvalidSelectionBeforeNetwork(t *testing.T) {
	cases := [][]string{
		{"--filters", "[]"}, {"--filters", `{"company_id":"88"}`}, {"--filters", `{"mailbox_id":"` + crmTestID + `"}`},
		{"--filters", `{"state":"deleted"}`}, {"--filters", `{"target_type":"contact"}`}, {"--filters", `{"search":""}`},
		{"--filters", `{"sort":"owner_uuid:asc"}`}, {"--filters", `{"state":"active","state":"archived"}`},
		{"--limit", "020"}, {"--limit", "0"}, {"--limit", "101"}, {"--limit", "20.0"}, {"--limit", "2e1"},
		{"--cursor", ""}, {"--cursor", "\x00"}, {"--company_id", "88"},
	}
	for i, extra := range cases {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); crmAccount(w, "*") }))
			defer server.Close()
			out, _, err := crmRun(t, server, conversationReadArgs(false, extra...)...)
			if exit.ForError(err) != exit.Usage || calls.Load() != 0 || out != "" {
				t.Fatalf("bad selection touched network %d %v %s", calls.Load(), err, out)
			}
		})
	}
	for _, args := range [][]string{{"crm", "conversations", "get", "uuidv4"}, {"crm", "conversations", "get", crmTestID, "--filters", "{}"}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected network") }))
		_, _, err := crmRun(t, server, append(args, "--json")...)
		server.Close()
		if exit.ForError(err) != exit.Usage {
			t.Fatal(err)
		}
	}
}

func TestConversationReadRequiresExistingScopes(t *testing.T) {
	for _, scopes := range [][]string{{"conversations:read"}, {"crm:read"}, {"crm:write"}, {"conversations.read"}, {}} {
		t.Run(strings.Join(scopes, ","), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, scopes...)
					return
				}
				calls.Add(1)
			}))
			defer server.Close()
			out, _, err := crmRun(t, server, conversationReadArgs(false)...)
			if exit.ForError(err) != exit.Perm || calls.Load() != 0 || out != "" {
				t.Fatalf("missing scope accepted %v %s", err, out)
			}
		})
	}
}

func TestConversationReadInvalidNativeProjections(t *testing.T) {
	cases := []struct {
		name   string
		detail bool
		mutate func(map[string]any)
	}{
		{"missing currentC7", false, func(v map[string]any) { v["meta"].(map[string]any)["authorization_version"] = "" }},
		{"noncurrent mask", false, func(v map[string]any) { conversationReadDropField(v, "uuid") }},
		{"extra secret", false, func(v map[string]any) { conversationReadHeadFixture(v, false)["storage_key"] = "SECRET" }},
		{"hidden subject", false, func(v map[string]any) { conversationReadDropField(v, "subject") }},
		{"hidden owner", false, func(v map[string]any) { conversationReadDropField(v, "owner_uuid") }},
		{"contradictory counts", false, func(v map[string]any) { v["data"].(map[string]any)["archived_count"] = 1 }},
		{"zero page fabricatedcursor", false, func(v map[string]any) {
			d := v["data"].(map[string]any)
			d["items"] = []any{}
			d["next_cursor"] = "fake"
			d["total"] = 0
			d["active_count"] = 0
		}},
		{"cursor without remaining cohort", false, func(v map[string]any) { v["data"].(map[string]any)["next_cursor"] = "fake" }},
		{"wrong limit", false, func(v map[string]any) { v["data"].(map[string]any)["limit"] = 21 }},
		{"duplicate row", false, func(v map[string]any) {
			d := v["data"].(map[string]any)
			d["items"] = append(d["items"].([]any), conversationReadHeadFixture(v, false))
			d["total"] = 2
			d["active_count"] = 2
		}},
		{"float integer", false, func(v map[string]any) { conversationReadHeadFixture(v, false)["version"] = 1.5 }},
		{"wrong thread", true, func(v map[string]any) { conversationReadHeadFixture(v, true)["id"] = crmTestOtherID }},
		{"hidden messages", true, func(v map[string]any) { conversationReadDropField(v, "messages") }},
		{"hidden attachment", true, func(v map[string]any) { conversationReadDropField(v, "attachments") }},
		{"hidden body", true, func(v map[string]any) { conversationReadDropField(v, "body") }},
		{"hidden sender", true, func(v map[string]any) { conversationReadDropField(v, "sender") }},
		{"hidden delivery", true, func(v map[string]any) { conversationReadDropField(v, "delivery_status") }},
		{"foreign message", true, func(v map[string]any) {
			v["data"].(map[string]any)["messages"].([]any)[0].(map[string]any)["conversation_id"] = crmTestOtherID
		}},
		{"foreign attachment parent", true, func(v map[string]any) {
			v["data"].(map[string]any)["attachments"].([]any)[0].(map[string]any)["conversation_id"] = crmTestOtherID
		}},
		{"foreign attachment message", true, func(v map[string]any) {
			v["data"].(map[string]any)["attachments"].([]any)[0].(map[string]any)["message_id"] = crmTestID
		}},
		{"attachment URL", true, func(v map[string]any) {
			v["data"].(map[string]any)["attachments"].([]any)[0].(map[string]any)["url"] = "https://SECRET"
		}},
		{"forged confirmation", true, func(v map[string]any) {
			v["data"].(map[string]any)["messages"].([]any)[0].(map[string]any)["delivery_status"] = "confirmed"
		}},
		{"draft delivery", true, func(v map[string]any) {
			v["data"].(map[string]any)["messages"].([]any)[0].(map[string]any)["composition_state"] = "draft"
		}},
		{"invalid timestamp", true, func(v map[string]any) {
			v["data"].(map[string]any)["messages"].([]any)[0].(map[string]any)["submitted_at"] = "2026-02-30T00:00:00Z"
		}},
		{"bytes instead of plaintext", true, func(v map[string]any) {
			v["data"].(map[string]any)["messages"].([]any)[0].(map[string]any)["body_format"] = "html"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := conversationReadFixture(tc.detail)
			tc.mutate(value)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "*")
					return
				}
				calls.Add(1)
				io.WriteString(w, conversationReadWire(value))
			}))
			defer server.Close()
			out, stderr, err := crmRun(t, server, conversationReadArgs(tc.detail)...)
			var native *apierr.APIError
			if !errors.As(err, &native) || native.StatusCode != 502 || native.Code != "invalid_native_response" || out != "" || calls.Load() != 1 {
				t.Fatalf("invalid projection accepted %v %s %s", err, out, stderr)
			}
			if strings.Contains(stderr, "SECRET") || strings.Contains(stderr, "private@example.test") {
				t.Fatal("invalid private payload leaked")
			}
		})
	}
}

func TestConversationReadNativeErrorsNoFallback(t *testing.T) {
	cases := []struct {
		status, code int
		kind, native string
	}{
		{401, exit.Auth, "authentication_error", "invalid_api_key"}, {403, exit.Perm, "authorization_error", "crm_access_denied"},
		{404, exit.NotFound, "not_found_error", "resource_not_found"}, {409, exit.Conflict, "conflict_error", "crm_version_conflict"},
		{422, exit.Validation, "invalid_request_error", "business_rule_violation"}, {429, exit.RateLimit, "rate_limit_error", "rate_limit_exceeded"},
		{500, exit.Server, "api_error", "internal_error"}, {503, exit.Server, "service_unavailable_error", "dependency_unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.native, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "*")
					return
				}
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				io.WriteString(w, conversationReadWire(map[string]any{"error": map[string]any{"type": tc.kind, "code": tc.native, "subcode": "native-original", "message": "No confirmado"}}))
			}))
			defer server.Close()
			out, stderr, err := crmRun(t, server, conversationReadArgs(true)...)
			if exit.ForError(err) != tc.code || calls.Load() != 1 || out != "" || !strings.Contains(stderr, tc.native) {
				t.Fatalf("native error changed %d %v %s %s", calls.Load(), err, out, stderr)
			}
		})
	}
	t.Run("redirect", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/account" {
				crmAccount(w, "*")
				return
			}
			calls.Add(1)
			w.Header().Set("Location", "/private-other")
			w.WriteHeader(307)
		}))
		defer server.Close()
		out, _, err := crmRun(t, server, conversationReadArgs(false)...)
		if err == nil || calls.Load() != 1 || out != "" {
			t.Fatalf("redirect followed %v %d", err, calls.Load())
		}
	})
}

func TestConversationReadDiscoveryPreservesExistingCommands(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"commands"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var entries []manifestEntry
	if json.Unmarshal(out.Bytes(), &entries) != nil || len(entries) != 600 {
		t.Fatalf("discovery count %d", len(entries))
	}
	byID := map[string]manifestEntry{}
	native := 0
	for _, entry := range entries {
		if _, duplicate := byID[entry.OperationID]; duplicate {
			t.Fatal("duplicate operationID")
		}
		byID[entry.OperationID] = entry
		if entry.CrmOperation != "" {
			native++
		}
	}
	if native != 116 {
		t.Fatalf("native count %d", native)
	}
	previous := append(append(append(append(nativeOwnerManifest(), taskTimeManifest()...), insightsManifest()...), serviceFormReferencesManifest()...), mailboxWriteOriginalManifest()...)
	for _, expected := range previous {
		if !bytes.Equal(nRaw(nativeSchema{"entry": expected}), nRaw(nativeSchema{"entry": byID[expected.OperationID]})) {
			t.Fatalf("prior owner contract changed %s", expected.OperationID)
		}
	}
	for _, entry := range conversationReadManifest() {
		actual := byID[entry.OperationID]
		if actual.Mutating || actual.IdempotencyRequired || actual.OriginalRecovery || actual.RequiresConfirmation || actual.RequestSchema != nil || actual.Method != "GET" || actual.AuthRealm != "staff" || !actual.Paginated || len(actual.RequiredScopes) != 2 || actual.ResponseSchemas["200"] == nil {
			t.Fatalf("invented read behavior %+v", actual)
		}
	}
}
