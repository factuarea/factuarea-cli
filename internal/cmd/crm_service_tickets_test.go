package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/output"
)

const crmTicketTestID = "01900000-0000-7000-8000-00000000000A"
const crmTicketTestOtherID = "01900000-0000-7000-8000-00000000000B"

func executeCRMServiceTicketsTest(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"--json", "crm", "service-tickets"}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}
func setupCRMServiceTicketsTest(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("FACTUAREA_API_KEY", "fact_test_"+strings.Repeat("a", 24))
	t.Setenv("FACTUAREA_BASE_URL", baseURL)
	t.Setenv("FACTUAREA_PROFILE", "tickets-native")
}
func crmTicketTestOp(action string) crmServiceTicketOperation {
	for _, op := range crmServiceTicketOperations {
		if op.action == action {
			return op
		}
	}
	panic("unknown test operation")
}
func crmTicketTestReceipt(id string, version int) string {
	return fmt.Sprintf(`{"data":{"id":%q,"version":%d}}`, id, version)
}
func crmTicketTestWriteArgs(t *testing.T, action, body string) []string {
	t.Helper()
	return []string{action, crmTicketTestID, "--data", body, "--idempotency-key", "Original-Ticket-Key !", "--intent-file", filepath.Join(t.TempDir(), "intent.json"), "--skip-scope-check"}
}

func TestCRMServiceTicketsSixNativeRoutes(t *testing.T) {
	for _, op := range crmServiceTicketOperations {
		t.Run(op.action, func(t *testing.T) {
			calls, preflight := 0, 0
			args := []string{}
			if len(op.args) > 0 {
				args = append(args, crmTicketTestID)
			}
			body := " \n" + crmServiceTicketExamples[op.action] + "\n "
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("Authorization") != "Bearer fact_test_"+strings.Repeat("a", 24) || r.Header.Get("Factuarea-Version") != "2026-10-09" || r.Header.Get("X-Active-Profile") != crmTicketTestOtherID {
					t.Error("native auth/profile/version changed")
				}
				if r.URL.Path == "/v1/account" {
					preflight++
					fmt.Fprintf(w, `{"data":{"api_key":{"scopes":[%q]}}}`, op.scope())
					return
				}
				calls++
				if r.Method != op.method || r.URL.Path != op.pathFor(args) {
					t.Errorf("wrong native route: %s %s", r.Method, r.URL.Path)
				}
				if op.mutating() {
					wire, _ := io.ReadAll(r.Body)
					if string(wire) != body || r.Header.Get("Idempotency-Key") != "Original-Ticket-Key !" || r.Header.Get("Content-Type") != "application/json" {
						t.Error("original bytes/key changed")
					}
					fmt.Fprint(w, crmTicketTestReceipt(crmTicketTestID, 2))
					return
				}
				if op.paginated {
					if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "3" {
						t.Error("native page query changed")
					}
					if op.action == "list" {
						if r.URL.Query().Get("status") != "pending" || r.URL.Query().Get("sort") != "updated_at" || r.URL.Query().Get("direction") != "desc" {
							t.Error("typed filter changed")
						}
						fmt.Fprintf(w, `{"data":{"items":[{"id":%q,"version":7}],"total":4,"page":2,"per_page":3}}`, crmTicketTestID)
					} else {
						fmt.Fprintf(w, `{"data":{"id":%q,"items":[{"version":1}],"total":4,"page":2,"per_page":3}}`, crmTicketTestID)
					}
					return
				}
				fmt.Fprint(w, crmTicketTestReceipt(crmTicketTestID, 7))
			}))
			defer server.Close()
			setupCRMServiceTicketsTest(t, server.URL)
			cli := append([]string{op.action}, args...)
			cli = append(cli, "--api-version", "2026-10-09", "--active-profile", crmTicketTestOtherID)
			intentFile := filepath.Join(t.TempDir(), "intent.json")
			if op.mutating() {
				cli = append(cli, "--data", body, "--idempotency-key", "Original-Ticket-Key !", "--intent-file", intentFile)
			}
			if op.paginated {
				cli = append(cli, "--page", "2", "--per-page", "3")
				if op.action == "list" {
					cli = append(cli, "--status", "pending", "--sort", "updated_at", "--direction", "desc")
				}
			}
			stdout, stderr, err := executeCRMServiceTicketsTest(t, cli...)
			if err != nil || calls != 1 || preflight != 1 || stderr != "" || !json.Valid([]byte(stdout)) {
				t.Fatalf("calls=%d preflight=%d err=%v stdout=%s stderr=%s", calls, preflight, err, stdout, stderr)
			}
			if op.mutating() {
				intent, err := loadCRMServiceTicketIntent(intentFile)
				if err != nil || intent.Group != "service-tickets" || intent.Body != body || intent.Key != "Original-Ticket-Key !" {
					t.Fatal("original intent changed")
				}
				st, _ := os.Stat(intentFile)
				wire, _ := os.ReadFile(intentFile)
				if st.Mode().Perm() != 0600 || strings.Contains(string(wire), "fact_test_") {
					t.Fatal("intent exposed credential or permissions")
				}
			}
			if !op.mutating() && strings.Contains(stdout, `"title"`) {
				t.Fatal("masked title fabricated")
			}
		})
	}
}

func TestCRMServiceTicketsWaitingBodyPresence(t *testing.T) {
	base := strings.TrimSuffix(crmServiceTicketExamples["stage"], "}")
	cases := []struct {
		name, suffix string
		valid        bool
	}{
		{"omitted", "}", true}, {"open", `,"waiting_reason":"waiting_customer"}`, true}, {"release", `,"waiting_reason":null}`, true},
		{"factual-release", `,"waiting_reason":null,"customer_message_id":"` + crmTicketTestOtherID + `"}`, true},
		{"null-message-omitted", `,"customer_message_id":null}`, true}, {"null-message-open", `,"waiting_reason":"waiting_customer","customer_message_id":null}`, true},
		{"nonnull-message-omitted", `,"customer_message_id":"` + crmTicketTestOtherID + `"}`, false}, {"nonnull-message-open", `,"waiting_reason":"waiting_customer","customer_message_id":"` + crmTicketTestOtherID + `"}`, false},
		{"invented-reason", `,"waiting_reason":"waiting_agent"}`, false}, {"empty-reason", `,"waiting_reason":""}`, false}, {"numeric-reason", `,"waiting_reason":0}`, false},
		{"native-message-required", `,"waiting_reason":null,"customer_message_id":"text"}`, false}, {"body-actor", `,"actor_id":"` + crmTicketTestOtherID + `"}`, false},
		{"body-status", `,"status":"pending"}`, false}, {"invented-date", `,"resolved_at":"2026-10-09T00:00:00Z"}`, false},
		{"duplicate", `,"waiting_reason":null,"waiting_reason":"waiting_customer"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := " \n" + base + tc.suffix + "\n "
			_, err := validateCRMServiceTicketBody("stage", []byte(body))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
			stdout, _, runErr := executeCRMServiceTicketsTest(t, "stage", crmTicketTestID, "--data", body, "--dry-run")
			if (runErr == nil) != tc.valid {
				t.Fatalf("dry-run=%v", runErr)
			}
			if tc.valid && stdout != body+"\n" {
				t.Fatalf("omitted/null/original bytes lost: %q", stdout)
			}
		})
	}
}

func TestCRMServiceTicketsRejectNonNativeBody(t *testing.T) {
	cases := []struct{ action, body string }{
		{"stage", strings.Replace(crmServiceTicketExamples["stage"], `"expected_version":1`, `"expected_version":1.0`, 1)},
		{"stage", strings.Replace(crmServiceTicketExamples["stage"], `"expected_version":1`, `"expected_version":"1"`, 1)},
		{"stage", strings.Replace(crmServiceTicketExamples["stage"], `"expected_version":1`, `"expected_version":0`, 1)},
		{"stage", strings.Replace(crmServiceTicketExamples["stage"], `"expected_version":1`, `"expected_version":9223372036854775807`, 1)},
		{"stage", strings.Replace(crmServiceTicketExamples["stage"], "-7000-", "-4000-", 1)},
		{"attach", strings.Replace(crmServiceTicketExamples["attach"], `"document_version":1`, `"document_version":1e0`, 1)},
		{"attach", strings.Replace(crmServiceTicketExamples["attach"], `"document_id"`, `"producer_reference_id"`, 1)},
		{"detach", strings.Replace(crmServiceTicketExamples["detach"], `"attachment_id"`, `"document_id"`, 1)},
		{"stage", crmServiceTicketExamples["stage"] + `{}`}, {"stage", `{"expected_version":1,"pipeline_id":null,"stage_id":null}`},
	}
	for i, tc := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if _, err := validateCRMServiceTicketBody(tc.action, []byte(tc.body)); err == nil {
				t.Fatal("accepted non-native body")
			}
		})
	}
}

func TestCRMServiceTicketsReceiptOriginalCAS(t *testing.T) {
	for _, tc := range []struct {
		action  string
		version int
		id      string
		valid   bool
	}{{"stage", 1, crmTicketTestID, true}, {"stage", 2, crmTicketTestID, true}, {"attach", 2, crmTicketTestID, true}, {"detach", 2, crmTicketTestID, true}, {"attach", 1, crmTicketTestID, false}, {"detach", 1, crmTicketTestID, false}, {"stage", 99, crmTicketTestID, false}, {"attach", 3, crmTicketTestID, false}, {"stage", 2, crmTicketTestOtherID, false}} {
		t.Run(fmt.Sprintf("%s-%d-%s", tc.action, tc.version, tc.id), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, crmTicketTestReceipt(tc.id, tc.version))
			}))
			defer server.Close()
			setupCRMServiceTicketsTest(t, server.URL)
			stdout, stderr, err := executeCRMServiceTicketsTest(t, crmTicketTestWriteArgs(t, tc.action, crmServiceTicketExamples[tc.action])...)
			if calls != 1 || (err == nil) != tc.valid {
				t.Fatalf("calls=%d valid=%t err=%v stderr=%s", calls, tc.valid, err, stderr)
			}
			if !tc.valid && (stdout != "" || !strings.Contains(stderr, `"unconfirmed":true`)) {
				t.Fatal("foreign/current-head receipt reported as confirmed")
			}
		})
	}
}

func TestCRMServiceTicketsRecoverOneOriginalEffect(t *testing.T) {
	for _, action := range []string{"stage", "attach", "detach"} {
		t.Run(action, func(t *testing.T) {
			body := " \n" + crmServiceTicketExamples[action] + "\n "
			if action == "stage" {
				body = strings.TrimSuffix(body, "}\n ") + `,"waiting_reason":null,"customer_message_id":"` + crmTicketTestOtherID + `"}` + "\n "
			}
			calls, effects := 0, 0
			op := crmTicketTestOp(action)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				wire, _ := io.ReadAll(r.Body)
				if r.Method != op.method || r.URL.Path != op.pathFor([]string{crmTicketTestID}) || string(wire) != body || r.Header.Get("Idempotency-Key") != "Original-Ticket-Key !" || r.Header.Get("Factuarea-Version") != "2026-10-09" || r.Header.Get("X-Active-Profile") != crmTicketTestOtherID {
					t.Error("original replay changed")
				}
				if calls == 1 {
					effects++
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, crmTicketTestReceipt(crmTicketTestID, 2))
			}))
			defer server.Close()
			setupCRMServiceTicketsTest(t, server.URL)
			path := filepath.Join(t.TempDir(), "original intent.json")
			args := []string{action, crmTicketTestID, "--data", body, "--idempotency-key", "Original-Ticket-Key !", "--intent-file", path, "--api-version", "2026-10-09", "--active-profile", crmTicketTestOtherID, "--skip-scope-check"}
			_, stderr, err := executeCRMServiceTicketsTest(t, args...)
			if err == nil || calls != 1 || effects != 1 || !strings.Contains(stderr, `"unconfirmed":true`) || !strings.Contains(stderr, "service-tickets recover") {
				t.Fatalf("initial dispatch calls=%d effects=%d err=%v stderr=%s", calls, effects, err, stderr)
			}
			before, _ := os.ReadFile(path)
			stdout, stderr, err := executeCRMServiceTicketsTest(t, "recover", "--intent-file", path, "--skip-scope-check")
			after, _ := os.ReadFile(path)
			if err != nil || calls != 2 || effects != 1 || !json.Valid([]byte(stdout)) || stderr != "" || !bytes.Equal(before, after) {
				t.Fatalf("recovery duplicated/changed effect: calls=%d effects=%d err=%v stderr=%s", calls, effects, err, stderr)
			}
		})
	}
}

func TestCRMServiceTicketsUnconfirmedAndTypedErrors(t *testing.T) {
	cases := []struct {
		name           string
		status         int
		body, location string
		unconfirmed    bool
	}{
		{"server", 503, `{"error":{"code":"unavailable","message":"native failure"}}`, "", true},
		{"forbidden", 403, `{"error":{"code":"forbidden","message":"native failure"}}`, "", true},
		{"missing", 404, `{"error":{"code":"not_found","message":"native failure"}}`, "", true},
		{"redirect", 307, `{}`, "/duplicate-effect", true},
		{"unconfirmed-conflict", 409, `{"error":{"code":"customer_service_conflict","subcode":"operation_unconfirmed","message":"native failure"}}`, "", true},
		{"revoked-conflict", 409, `{"error":{"code":"authority_revoked","message":"native failure"}}`, "", true},
		{"changed-conflict", 409, `{"error":{"code":"authorization_changed","message":"native failure"}}`, "", true},
		{"cas", 409, `{"error":{"code":"version_conflict","message":"native conflict"}}`, "", false},
		{"validation", 422, `{"error":{"code":"invalid_parameter","message":"native validation"}}`, "", false},
		{"rate", 429, `{"error":{"code":"rate_limit","message":"native throttle"}}`, "", false},
		{"wrong-success", 201, crmTicketTestReceipt(crmTicketTestID, 2), "", true},
		{"malformed", 200, `{"data":`, "", true},
		{"private-extra", 200, strings.TrimSuffix(crmTicketTestReceipt(crmTicketTestID, 2), "}}") + `,"storage_key":"private"}}`, "", true},
		{"duplicate-json", 200, `{"data":{"id":"` + crmTicketTestID + `","version":2,"version":2}}`, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-Id", "ticket-request-original")
				if tc.location != "" {
					w.Header().Set("Location", tc.location)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			setupCRMServiceTicketsTest(t, server.URL)
			stdout, stderr, err := executeCRMServiceTicketsTest(t, crmTicketTestWriteArgs(t, "attach", crmServiceTicketExamples["attach"])...)
			if calls != 1 || err == nil || stdout != "" || strings.Contains(stderr, `"unconfirmed":true`) != tc.unconfirmed {
				t.Fatalf("calls=%d err=%v unconfirmed=%t stderr=%s", calls, err, tc.unconfirmed, stderr)
			}
			if !tc.unconfirmed {
				var api *apierr.APIError
				if !errors.As(err, &api) || api.StatusCode != tc.status || api.RequestID != "ticket-request-original" {
					t.Fatal("typed native status/request ID lost")
				}
			}
		})
	}
	t.Run("truncated", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", "999")
			fmt.Fprint(w, crmTicketTestReceipt(crmTicketTestID, 2))
		}))
		defer server.Close()
		setupCRMServiceTicketsTest(t, server.URL)
		_, stderr, err := executeCRMServiceTicketsTest(t, crmTicketTestWriteArgs(t, "detach", crmServiceTicketExamples["detach"])...)
		if err == nil || calls != 1 || !strings.Contains(stderr, `"unconfirmed":true`) {
			t.Fatal("truncated transfer accepted/retried")
		}
	})
}

func TestCRMServiceTicketsRecoveryBindingAndPrivateFile(t *testing.T) {
	for _, kind := range []string{"key", "body", "path", "group", "header", "credential", "profile", "server", "mode", "symlink", "overwrite", "document-owner"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, crmTicketTestReceipt(crmTicketTestID, 2))
			}))
			defer server.Close()
			setupCRMServiceTicketsTest(t, server.URL)
			root := NewRootCmd()
			root.SetArgs([]string{"crm", "service-tickets", "get", crmTicketTestID})
			command, _, err := root.Find([]string{"crm", "service-tickets", "get"})
			if err != nil {
				t.Fatal(err)
			}
			command.SetContext(context.Background())
			cc, closeClient, err := newCRMDocumentContext(command, "2026-10-09", crmTicketTestOtherID)
			if err != nil {
				t.Fatal(err)
			}
			defer closeClient()
			intent := newCRMServiceTicketIntent(crmTicketTestOp("attach"), []string{crmTicketTestID}, []byte(crmServiceTicketExamples["attach"]), "Original-Ticket-Key !", "2026-10-09", crmTicketTestOtherID, cc)
			path := filepath.Join(t.TempDir(), "intent.json")
			if err := intent.save(path); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "key":
				intent.Key = "new-key"
			case "body":
				intent.Body = strings.Replace(intent.Body, `"expected_version":1`, `"expected_version":2`, 1)
			case "path":
				intent.Path = "/v1/crm/service-tickets/" + crmTicketTestOtherID + "/attachments"
			case "group":
				intent.Group = "documents"
			case "header":
				intent.ActiveProfile = crmTicketTestID
			case "credential":
				t.Setenv("FACTUAREA_API_KEY", "fact_test_"+strings.Repeat("b", 24))
			case "profile":
				t.Setenv("FACTUAREA_PROFILE", "other-profile")
			case "server":
				t.Setenv("FACTUAREA_BASE_URL", server.URL+"/other")
			case "mode":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				link := filepath.Join(t.TempDir(), "link.json")
				if err := os.Symlink(path, link); err != nil {
					t.Fatal(err)
				}
				path = link
			case "overwrite":
				if err := intent.save(path); err == nil {
					t.Fatal("checkpoint overwritten")
				}
				return
			case "document-owner":
				docOp := crmDocumentOperations[2]
				docIntent := newCRMDocumentIntent(docOp, nil, []byte(crmDocumentExamples["upload"]), "document-key", "2026-10-09", crmTicketTestOtherID, cc)
				wire, _ := json.Marshal(docIntent)
				if err := os.WriteFile(path, wire, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if crmDocumentEnum(kind, "key", "body", "path", "group", "header") {
				wire, _ := json.Marshal(intent)
				if err := os.WriteFile(path, wire, 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, _, err = executeCRMServiceTicketsTest(t, "recover", "--intent-file", path, "--skip-scope-check")
			if err == nil || calls != 0 {
				t.Fatalf("recovery binding accepted: kind=%s calls=%d err=%v", kind, calls, err)
			}
		})
	}
	t.Run("documents-reject-ticket-owner", func(t *testing.T) {
		setupCRMServiceTicketsTest(t, "http://127.0.0.1:1")
		root := NewRootCmd()
		cmd, _, _ := root.Find([]string{"crm", "service-tickets", "get"})
		cmd.SetContext(context.Background())
		cc, closeClient, err := newCRMDocumentContext(cmd, "", "")
		if err != nil {
			t.Fatal(err)
		}
		defer closeClient()
		intent := newCRMServiceTicketIntent(crmTicketTestOp("stage"), []string{crmTicketTestID}, []byte(crmServiceTicketExamples["stage"]), "original-key", "", "", cc)
		if _, _, err := crmDocumentIntent(intent).validate("documents", cc); err == nil {
			t.Fatal("document recovery accepted ticket owner")
		}
	})
}

func TestCRMServiceTicketsMaskedChildrenAndHistory(t *testing.T) {
	actor := "01900000-0000-4000-8000-00000000000A"
	full := fmt.Sprintf(`{"id":%q,"version":2,"title":"Native","status":"pending","creator_id":%q,"assignee_id":null,"pipeline_id":null,"comments":[{"id":%q,"visibility":"public","author_id":%q}],"attachments":[{"id":%q,"document_id":%q,"document_version":7,"created_by":%q}],"writable_fields":["title"],"authorization_version":"original-auth-version"}`, crmTicketTestID, actor, crmTicketTestOtherID, actor, crmTicketTestOtherID, crmTicketTestID, actor)
	timeline := fmt.Sprintf(`{"id":%q,"items":[{"version":2,"action":"stage_changed","actor_id":%q,"occurred_at":"2026-10-09T12:00:00+02:00","snapshot":%s}],"total":1,"page":1,"per_page":50}`, crmTicketTestID, actor, full)
	for _, tc := range []struct {
		name, action, data string
		valid              bool
	}{
		{"detail", "get", full, true}, {"minimal", "get", fmt.Sprintf(`{"id":%q,"version":2}`, crmTicketTestID), true}, {"historical", "timeline", timeline, true},
		{"current-head-substitution", "timeline", strings.Replace(timeline, `"snapshot":{"id":"`+crmTicketTestID+`","version":2`, `"snapshot":{"id":"`+crmTicketTestID+`","version":99`, 1), false},
		{"foreign-snapshot", "timeline", strings.Replace(timeline, `"snapshot":{"id":"`+crmTicketTestID, `"snapshot":{"id":"`+crmTicketTestOtherID, 1), false},
		{"private-storage", "get", strings.Replace(full, `"document_version":7`, `"document_version":7,"storage_key":"private"`, 1), false},
		{"half-authority", "get", strings.Replace(full, `,"authorization_version":"original-auth-version"`, "", 1), false},
		{"nullable-children", "get", strings.Replace(full, `"comments":[{"id":"`+crmTicketTestOtherID+`","visibility":"public","author_id":"`+actor+`"}]`, `"comments":null`, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := crmDocumentJSON([]byte(tc.data))
			if err != nil {
				t.Fatal(err)
			}
			q := url.Values{"page": {"1"}, "per_page": {"50"}}
			err = validateCRMServiceTicketRead(crmTicketTestOp(tc.action), []string{crmTicketTestID}, q, data)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
		})
	}
	t.Run("terminal-controls", func(t *testing.T) {
		data, _ := crmDocumentJSON([]byte(fmt.Sprintf(`{"id":%q,"version":1,"title":"safe\u001b[31m\nrow"}`, crmTicketTestID)))
		var out bytes.Buffer
		if err := printCRMServiceTicketBody(&out, nil, data, output.Human, crmTicketTestOp("get")); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "\x1b") || strings.Contains(out.String(), "\nrow") {
			t.Fatal("terminal control leaked")
		}
	})
}

func TestCRMServiceTicketsPaginationBounds(t *testing.T) {
	for _, kind := range []string{"list", "timeline", "limit", "empty", "wrong-page"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				w.Header().Set("Content-Type", "application/json")
				items := fmt.Sprintf(`[{"id":%q,"version":%d}]`, crmTicketTestID, page)
				id := ""
				if kind == "timeline" {
					id = fmt.Sprintf(`"id":%q,`, crmTicketTestID)
					items = fmt.Sprintf(`[{"version":%d}]`, page)
				}
				if kind == "empty" {
					items = `[]`
				}
				if kind == "wrong-page" {
					page++
				}
				fmt.Fprintf(w, `{"data":{%s"items":%s,"total":2,"page":%d,"per_page":1}}`, id, items, page)
			}))
			defer server.Close()
			setupCRMServiceTicketsTest(t, server.URL)
			action := "list"
			args := []string{action}
			if kind == "timeline" {
				action = "timeline"
				args = []string{action, crmTicketTestID}
			}
			args = append(args, "--all", "--per-page", "1", "--skip-scope-check")
			if kind == "limit" {
				args = append(args, "--max-pages", "1")
			}
			stdout, stderr, err := executeCRMServiceTicketsTest(t, args...)
			success := kind == "list" || kind == "timeline"
			if (err == nil) != success {
				t.Fatalf("kind=%s calls=%d err=%v stderr=%s", kind, calls, err, stderr)
			}
			if success && (calls != 2 || strings.Count(stdout, "\n") != 2) {
				t.Fatal("native pagination incomplete")
			}
			if !success && calls != 1 {
				t.Fatal("invalid/partial pagination retried")
			}
		})
	}
	for _, args := range [][]string{{"list", "--status", ""}, {"list", "--status", "invented"}, {"list", "--sort", "storage_key"}, {"list", "--direction", "random"}, {"list", "--page", "0"}, {"timeline", crmTicketTestID, "--per-page", "101"}, {"list", "--max-pages", "1"}, {"list", "--all", "--max-pages", "0"}, {"timeline", crmTicketTestID, "--status", "pending"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			if _, _, err := executeCRMServiceTicketsTest(t, args...); err == nil {
				t.Fatal("invalid native query accepted")
			}
		})
	}
}

func TestCRMServiceTicketsNativeSafetyAndOffline(t *testing.T) {
	for _, kind := range []string{"missing-scope", "live-guard", "key-required", "intent-required", "invalid-key", "offline-skeleton"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm_documents:write"]}}}`)
			}))
			defer server.Close()
			setupCRMServiceTicketsTest(t, server.URL)
			args := crmTicketTestWriteArgs(t, "detach", crmServiceTicketExamples["detach"])
			switch kind {
			case "missing-scope":
				args = args[:len(args)-1]
			case "live-guard":
				t.Setenv("FACTUAREA_API_KEY", "fact_live_"+strings.Repeat("a", 24))
			case "key-required":
				args[5] = ""
			case "intent-required":
				args[7] = ""
			case "invalid-key":
				args[5] = " outer-whitespace "
			case "offline-skeleton":
				args = []string{"stage", crmTicketTestID, "--skeleton"}
				t.Setenv("FACTUAREA_API_KEY", "")
			}
			_, _, err := executeCRMServiceTicketsTest(t, args...)
			success := kind == "offline-skeleton"
			if (err == nil) != success {
				t.Fatalf("kind=%s err=%v", kind, err)
			}
			wantCalls := 0
			if kind == "missing-scope" {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatal("mutation dispatched before native safety")
			}
		})
	}
}

func TestCRMServiceTicketsManifestAndClosedRecovery(t *testing.T) {
	entries := crmServiceTicketManifest()
	if len(entries) != 7 {
		t.Fatal("expected six real operations and one local recovery")
	}
	for i, entry := range entries {
		if i < 6 {
			op := crmServiceTicketOperations[i]
			if entry.OperationID != op.operationID || entry.RequiredScope != op.scope() || entry.Irreversible {
				t.Fatal("native metadata mismatch")
			}
		} else if entry.OperationID != "" {
			t.Fatal("invented receipt endpoint")
		}
	}
	stage := entries[0]
	if len(stage.BodyFields) != 5 || stage.BodyFields[3].Required || stage.BodyFields[3].Type != "string|null" || stage.BodyFields[4].Required {
		t.Fatal("waiting omitted/null manifest lost")
	}
	for _, flag := range []string{"--data", "--idempotency-key", "--api-version", "--active-profile", "--confirm"} {
		t.Run(flag, func(t *testing.T) {
			if _, _, err := executeCRMServiceTicketsTest(t, "recover", flag, "override"); err == nil {
				t.Fatal("recovery accepted overrides")
			}
		})
	}
	for _, op := range crmServiceTicketOperations {
		stdout, _, err := executeCRMServiceTicketsTest(t, op.action, "--help")
		if err != nil || !strings.Contains(stdout, op.summary) {
			t.Fatal("native help unavailable")
		}
	}
}
