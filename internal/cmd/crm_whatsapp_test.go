package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const crmWhatsAppTestMessage = "01900000-0000-7000-8000-000000000002"
const crmWhatsAppTestEffect = "01900000-0000-7000-8000-000000000003"

func crmWhatsAppTestPreparation() crmWhatsAppPreparation {
	return crmWhatsAppPreparation{ConversationID: crmOAuthTestID, MessageID: crmWhatsAppTestMessage, EffectID: crmWhatsAppTestEffect, ExpectedVersion: 3, MessageVersion: 4, Purpose: "contact_request", Sender: crmWhatsAppAddress{Channel: "whatsapp", Phone: "+34611111111"}, Recipient: crmWhatsAppRecipient{Channel: "whatsapp", Phone: "+34622222222", Kind: "to"}, Body: "Hola 山", PreparationToken: "server-sealed-token", SnapshotDigest: strings.Repeat("a", 64), AuthorizationVersion: "current-C7"}
}
func crmWhatsAppTestJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}
func TestCRMWhatsAppExactPrepareSendOriginalCheckpoint(t *testing.T) {
	calls := []string{}
	var first string
	timestamp := "2026-10-10T11:00:00Z"
	provider := "wamid-native"
	receipt := crmWhatsAppReceipt{Status: "confirmed", ProviderMessageID: &provider, SubmittedAt: &timestamp}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer fact_test_"+strings.Repeat("a", 24) || r.Header.Get("Idempotency-Key") != "" || r.Header.Get("X-API-Key") != "" {
			t.Error("native authority/key changed")
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "preparation"):
			if len(r.URL.Query()) != 1 || r.URL.Query().Get("purpose") != "contact_request" {
				t.Error("preparation query changed")
			}
			crmWhatsAppTestJSON(w, crmWhatsAppTestPreparation())
		case strings.HasSuffix(r.URL.Path, "send"):
			raw, _ := io.ReadAll(r.Body)
			first = string(raw)
			if !crmWhatsAppValidWire(raw, crmWhatsAppTestPreparation()) {
				t.Error("send wire changed")
			}
			crmWhatsAppTestJSON(w, receipt)
		case strings.HasSuffix(r.URL.Path, "original"):
			if len(r.URL.Query()) != 1 || r.URL.Query().Get("original_wire") != first {
				t.Error("exact first wire changed")
			}
			crmWhatsAppTestJSON(w, crmWhatsAppOriginal{State: "confirmed", ConversationID: crmOAuthTestID, MessageID: crmWhatsAppTestMessage, EffectID: crmWhatsAppTestEffect, OriginalWireHash: crmWhatsAppHash([]byte(first)), PreparationDigest: strings.Repeat("a", 64), Receipt: &receipt})
		default:
			t.Error("unexpected route")
		}
	}))
	defer server.Close()
	setupCRMOAuthTest(t, server.URL)
	path := filepath.Join(t.TempDir(), "intent.json")
	if _, err := executeCRMOAuthTest(t, "whatsapp", "prepare", crmOAuthTestID, crmWhatsAppTestMessage, "--purpose", "contact_request", "--intent-file", path); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Error("checkpoint not private")
	}
	if _, err := executeCRMOAuthTest(t, "whatsapp", "send", "--intent-file", path, "--confirm", crmWhatsAppTestMessage); err != nil {
		t.Fatal(err)
	}
	if _, err := executeCRMOAuthTest(t, "whatsapp", "original", "--intent-file", path); err != nil {
		t.Fatal(err)
	}
	if _, err := executeCRMOAuthTest(t, "whatsapp", "send", "--intent-file", path, "--confirm", crmWhatsAppTestMessage); err == nil {
		t.Error("send repeated")
	}
	if len(calls) != 3 || !strings.HasPrefix(calls[0], "GET ") || !strings.HasPrefix(calls[1], "POST ") || !strings.HasPrefix(calls[2], "GET ") {
		t.Fatalf("calls: %v", calls)
	}
}
func TestCRMWhatsAppUnknownNeverReplaysAcrossProcesses(t *testing.T) {
	for _, failure := range []string{"503", "timeout", "pending"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if strings.HasSuffix(r.URL.Path, "preparation") {
					crmWhatsAppTestJSON(w, crmWhatsAppTestPreparation())
					return
				}
				switch failure {
				case "503":
					w.WriteHeader(503)
				case "timeout":
					conn, _, _ := w.(http.Hijacker).Hijack()
					conn.Close()
				case "pending":
					crmWhatsAppTestJSON(w, crmWhatsAppReceipt{Status: "pending"})
				}
			}))
			defer server.Close()
			setupCRMOAuthTest(t, server.URL)
			path := filepath.Join(t.TempDir(), "intent.json")
			if _, err := executeCRMOAuthTest(t, "whatsapp", "prepare", crmOAuthTestID, crmWhatsAppTestMessage, "--purpose", "contact_request", "--intent-file", path); err != nil {
				t.Fatal(err)
			}
			if out, err := executeCRMOAuthTest(t, "whatsapp", "send", "--intent-file", path, "--confirm", crmWhatsAppTestMessage); err == nil || strings.Contains(out, "sealed-token") {
				t.Error("ambiguous send not retained safely")
			}
			if _, err := executeCRMOAuthTest(t, "whatsapp", "send", "--intent-file", path, "--confirm", crmWhatsAppTestMessage); err == nil {
				t.Error("repeated SEND")
			}
			if calls != 2 {
				t.Fatalf("calls=%d", calls)
			}
			intent, err := crmWhatsAppLoad(path)
			if err != nil || !intent.Dispatched {
				t.Fatal("dispatch mark not persistent")
			}
		})
	}
}
func TestCRMWhatsAppNativeErrorsDoNotRetry(t *testing.T) {
	for _, status := range []int{401, 403, 409, 422, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if strings.HasSuffix(r.URL.Path, "preparation") {
					crmWhatsAppTestJSON(w, crmWhatsAppTestPreparation())
					return
				}
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":{"code":"native_rejection","message":"rejected"},"request_id":"native-request"}`)
			}))
			defer server.Close()
			setupCRMOAuthTest(t, server.URL)
			path := filepath.Join(t.TempDir(), "intent.json")
			_, err := executeCRMOAuthTest(t, "whatsapp", "prepare", crmOAuthTestID, crmWhatsAppTestMessage, "--purpose", "contact_request", "--intent-file", path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = executeCRMOAuthTest(t, "whatsapp", "send", "--intent-file", path, "--confirm", crmWhatsAppTestMessage)
			if err == nil || calls != 2 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}
func TestCRMWhatsAppCheckpointContextConfirmationAndTamper(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		crmWhatsAppTestJSON(w, crmWhatsAppTestPreparation())
	}))
	defer server.Close()
	setupCRMOAuthTest(t, server.URL)
	path := filepath.Join(t.TempDir(), "intent.json")
	if _, err := executeCRMOAuthTest(t, "whatsapp", "prepare", crmOAuthTestID, crmWhatsAppTestMessage, "--purpose", "contact_request", "--intent-file", path); err != nil {
		t.Fatal(err)
	}
	if _, err := executeCRMOAuthTest(t, "whatsapp", "prepare", crmOAuthTestID, crmWhatsAppTestMessage, "--purpose", "contact_request", "--intent-file", path); err == nil {
		t.Error("existing effect overwritten")
	}
	if _, err := executeCRMOAuthTest(t, "whatsapp", "send", "--intent-file", path, "--confirm", crmOAuthTestID); err == nil {
		t.Error("confirmation ignored")
	}
	t.Setenv("FACTUAREA_API_KEY", "fact_test_"+strings.Repeat("b", 24))
	if _, err := executeCRMOAuthTest(t, "whatsapp", "original", "--intent-file", path); err == nil {
		t.Error("credential context changed")
	}
	if calls != 1 {
		t.Error("context reject dispatched HTTP")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := crmWhatsAppLoad(path); err == nil {
		t.Error("public checkpoint accepted")
	}
}
func TestCRMWhatsAppOriginalStatesAndMismatch(t *testing.T) {
	for _, state := range []string{"not_found", "unconfirmed", "failed", "foreign"} {
		t.Run(state, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				p := crmWhatsAppTestPreparation()
				if strings.HasSuffix(r.URL.Path, "preparation") {
					crmWhatsAppTestJSON(w, p)
					return
				}
				raw := r.URL.Query().Get("original_wire")
				rstate := state
				var receipt *crmWhatsAppReceipt
				if state == "unconfirmed" || state == "failed" {
					reason := "native_reason"
					receipt = &crmWhatsAppReceipt{Status: state, ReasonCode: &reason}
				}
				hash := crmWhatsAppHash([]byte(raw))
				if state == "foreign" {
					rstate = "not_found"
					hash = strings.Repeat("b", 64)
				}
				crmWhatsAppTestJSON(w, crmWhatsAppOriginal{State: rstate, ConversationID: p.ConversationID, MessageID: p.MessageID, EffectID: p.EffectID, OriginalWireHash: hash, PreparationDigest: p.SnapshotDigest, Receipt: receipt})
			}))
			defer server.Close()
			setupCRMOAuthTest(t, server.URL)
			path := filepath.Join(t.TempDir(), "intent.json")
			if _, err := executeCRMOAuthTest(t, "whatsapp", "prepare", crmOAuthTestID, crmWhatsAppTestMessage, "--purpose", "contact_request", "--intent-file", path); err != nil {
				t.Fatal(err)
			}
			_, err := executeCRMOAuthTest(t, "whatsapp", "original", "--intent-file", path)
			if (err != nil) != (state == "foreign") || calls != 2 {
				t.Fatalf("state=%s err=%v calls=%d", state, err, calls)
			}
		})
	}
}
func TestCRMWhatsAppWireAndManifest(t *testing.T) {
	p := crmWhatsAppTestPreparation()
	raw, _ := json.Marshal(crmWhatsAppBody(p))
	spaced := append(append([]byte(" \n"), raw...), []byte("\n")...)
	if !crmWhatsAppValidWire(spaced, p) {
		t.Error("wire whitespace not preserved")
	}
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	body["body"] = "injected"
	invalid, _ := json.Marshal(body)
	if crmWhatsAppValidWire(invalid, p) {
		t.Error("extra field accepted")
	}
	entries := crmWhatsAppManifest()
	if len(entries) != 3 || !entries[1].Mutating || !entries[1].Irreversible || entries[2].Mutating || entries[2].RequiredScope != "conversations:read" {
		t.Error("native manifest incorrect")
	}
}
func TestCRMWhatsAppNativeTemplateAndShortPhone(t *testing.T) {
	p := crmWhatsAppTestPreparation()
	p.Sender.Phone = "+123456"
	p.Template = &crmWhatsAppTemplate{ID: "123", Name: strings.Repeat("a", 512), Language: "es_ES", Category: "UTILITY", Parameters: []string{"Hola 山"}}
	if !crmWhatsAppValidPreparation(p, p.ConversationID, p.MessageID) {
		t.Fatal("native template or E164 rejected")
	}
	raw, _ := json.Marshal(crmWhatsAppBody(p))
	if !crmWhatsAppValidWire(raw, p) {
		t.Fatal("native template changed")
	}
	p.Template.Parameters = []string{""}
	if crmWhatsAppValidTemplate(p.Template) {
		t.Fatal("blank native parameter accepted")
	}
}

func TestCRMWhatsAppRevalidationRetainsOriginalCheckpoint(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			if r.URL.Query().Get("preparation_token") != "server-sealed-token" {
				t.Error("same token missing")
			}
		}
		crmWhatsAppTestJSON(w, crmWhatsAppTestPreparation())
	}))
	defer server.Close()
	setupCRMOAuthTest(t, server.URL)
	path := filepath.Join(t.TempDir(), "intent.json")
	args := []string{"whatsapp", "prepare", crmOAuthTestID, crmWhatsAppTestMessage, "--purpose", "contact_request", "--intent-file", path}
	if _, err := executeCRMOAuthTest(t, args...); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if _, err := executeCRMOAuthTest(t, append(args, "--revalidate")...); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || calls != 2 {
		t.Error("revalidation changed original checkpoint")
	}
}
