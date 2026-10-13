package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/exit"
)

const taskTimeBindingID = "019c0511-1111-7111-8111-333333333333"
const taskTimeEffectID = "019c0511-1111-7111-8111-444444444444"
const taskTimeEntryID = "019c0511-1111-7111-8111-555555555555"
const taskTimeRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const taskTimeKey = "task-time original / ASCII : 70"

func taskTimeWriteWire(action string) string {
	binding := ""
	if action == "unlink" {
		binding = `,"binding_id":"` + taskTimeBindingID + `","expected_binding_version":3`
	}
	return " \n" + `{"task_id":"` + crmTestOtherID + `","expected_ticket_version":7,"expected_task_version":11,"confirmed":true` + binding + "}\n"
}
func taskTimeRecoveryWire(action string) string {
	version := ""
	if action == "unlink" {
		version = `,"expected_binding_version":3`
	}
	return ` {"task_id":"` + crmTestOtherID + `","expected_ticket_version":7,"expected_task_version":11,"binding_id":"` + taskTimeBindingID + `","effect_id":"` + taskTimeEffectID + `","action":"` + action + `"` + version + "} \n"
}
func taskTimePreviewWire() string {
	return ` {"task_id":"` + crmTestOtherID + `","expected_ticket_version":7,"expected_task_version":11,"entries":[{"id":"` + taskTimeEntryID + `","revision":"` + taskTimeRevision + `"}],"grouping":"per_entry"} `
}
func taskTimeReceiptFixture(action string) map[string]any {
	var archived any
	version := 1
	if action == "unlink" {
		archived = "2026-10-10T09:00:00+00:00"
		version = 4
	}
	return map[string]any{"ticket_id": crmTestID, "ticket_version": 8, "authorization_version": "current-native-c7", "status": "confirmed", "effect_id": taskTimeEffectID, "operation": "customer_service.task_time_" + action, "confirmed_at": "2026-10-10T09:00:00+00:00", "result": map[string]any{"binding": map[string]any{"id": taskTimeBindingID, "task_id": crmTestOtherID, "version": version, "archived_at": archived}, "task_version": 11}}
}
func taskTimeReadFixture(caseName string) map[string]any {
	base := map[string]any{"ticket_id": crmTestID, "ticket_version": 7, "authorization_version": "current-native-c7"}
	if caseName == "preview" {
		base["preview"] = map[string]any{"contact": map[string]any{"id": crmTestOtherID, "name": "Native contact"}, "lines": []any{map[string]any{"description": "Horas originales", "quantity": "0.333333", "unit": "h", "unit_price": "9007199254740993.0100", "amount": "3002399751580331.0000"}}, "total_seconds": 1200, "total_hours": "0.333333", "time_entry_ids": []any{taskTimeEntryID}, "warnings": []any{}, "totals": nil}
	} else {
		base["revision"] = taskTimeRevision
		if caseName == "tasks" {
			base["tasks"] = []any{map[string]any{"id": taskTimeBindingID, "task_id": crmTestOtherID}}
		} else {
			base["entries"] = []any{map[string]any{"id": taskTimeEntryID, "task_id": crmTestOtherID, "duration_seconds": nil, "description": nil, "ended_at": nil, "invoice_id": nil}}
			base["next_cursor"] = nil
		}
	}
	return base
}

func TestTaskTimeBillingEightNativeBindings(t *testing.T) {
	tests := []struct {
		action, id, method, path, scope, wire, response string
		original                                        bool
	}{
		{"link", "linkServiceTicketTask", "POST", "tasks/link", "customer_service:write", taskTimeWriteWire("link"), nativeJSONEnvelope(taskTimeReceiptFixture("link")), true},
		{"unlink", "unlinkServiceTicketTask", "POST", "tasks/unlink", "customer_service:write", taskTimeWriteWire("unlink"), nativeJSONEnvelope(taskTimeReceiptFixture("unlink")), true},
		{"tasks", "listServiceTicketTasks", "GET", "tasks", "customer_service:read", "", nativeJSONEnvelope(taskTimeReadFixture("tasks")), false},
		{"billable", "getServiceBillableTime", "GET", "billable-time", "customer_service:read", "", nativeJSONEnvelope(taskTimeReadFixture("billable")), false},
		{"timesheet", "getServiceTimesheet", "GET", "timesheet", "customer_service:read", "", nativeJSONEnvelope(taskTimeReadFixture("timesheet")), false},
		{"preview", "previewServiceTimeInvoice", "POST", "time-invoice-preview", "customer_service:read", taskTimePreviewWire(), nativeJSONEnvelope(taskTimeReadFixture("preview")), false},
		{"recover-link", "getOriginalServiceTicketTaskLinkReceipt", "POST", "task-time/link/original-receipt", "customer_service:write", taskTimeRecoveryWire("link"), nativeJSONEnvelope(taskTimeReceiptFixture("link")), false},
		{"recover-unlink", "getOriginalServiceTicketTaskUnlinkReceipt", "POST", "task-time/unlink/original-receipt", "customer_service:write", taskTimeRecoveryWire("unlink"), nativeJSONEnvelope(taskTimeReceiptFixture("unlink")), false},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, test.scope)
					return
				}
				calls.Add(1)
				body, _ := io.ReadAll(r.Body)
				if r.Method != test.method || r.URL.Path != "/v1/crm/service-tickets/"+crmTestID+"/"+test.path || len(r.URL.Query()) != 0 || string(body) != test.wire {
					t.Errorf("native route or exact wire changed: %s %s %q", r.Method, r.URL, string(body))
				}
				if r.Header.Get("Authorization") == "" {
					t.Error("existing staff auth lost")
				}
				if test.original {
					if r.Header.Get("Idempotency-Key") != taskTimeKey {
						t.Error("original key changed")
					}
				} else if _, present := r.Header["Idempotency-Key"]; present {
					t.Error("read allocated a key")
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, test.response)
			}))
			defer server.Close()
			args := []string{"crm", "task-time-billing", test.action, crmTestID, "--json"}
			if test.wire != "" {
				args = append(args, "--data", test.wire)
			}
			if test.original {
				args = append(args, "--idempotency-key", taskTimeKey)
			}
			out, stderr, err := crmRun(t, server, args...)
			if err != nil || calls.Load() != 1 {
				t.Fatalf("calls=%d err=%v stderr=%s", calls.Load(), err, stderr)
			}
			var result map[string]any
			if json.Unmarshal([]byte(out), &result) != nil {
				t.Fatalf("native response absent: %s", out)
			}
			if test.action == "billable" && strings.Contains(out, "total_billable_closed_seconds") {
				t.Fatal("hidden aggregate fabricated")
			}
			if test.action == "preview" && (!strings.Contains(out, "9007199254740993.0100") || !strings.Contains(out, `"totals":null`)) {
				t.Fatal("Money string/null changed")
			}
		})
	}
}

func TestTaskTimeBillingDiscoveryKeepsNativeSchemasAndExistingCohorts(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"commands"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var entries []manifestEntry
	if json.Unmarshal(out.Bytes(), &entries) != nil {
		t.Fatal("discovery invalid")
	}
	byID := map[string]manifestEntry{}
	count := 0
	for _, entry := range entries {
		if _, exists := byID[entry.OperationID]; exists {
			t.Fatalf("duplicate %s", entry.OperationID)
		}
		byID[entry.OperationID] = entry
		if strings.HasPrefix(entry.Command, "factuarea crm task-time-billing ") {
			count++
		}
	}
	if count != 8 {
		t.Fatalf("task native command count %d", count)
	}
	for _, op := range taskTimeOperations() {
		e := byID[op.ID]
		if e.NativePath != op.Path || e.Method != op.Method || e.CrmOperation != op.Operation || e.AuthRealm != "staff" || e.RequiredScope != op.Scope || e.Mutating != op.Mutating || e.IdempotencyRequired != op.Mutating || e.OriginalRecovery != op.Recovery || e.RequiresConfirmation || e.LocalConfirmation || len(e.ResponseSchemas) != 1 || e.ResponseSchemas["200"] == nil {
			t.Fatalf("native metadata changed %+v", e)
		}
		if op.Body != nil && (e.MaxBodyBytes != 262144 || e.RequestSchema == nil || e.OriginalBodySchema != nil) {
			t.Fatal("native wire limit or recovery semantics changed")
		}
	}
	for _, entry := range nativeOwnerManifest() {
		current := byID[entry.OperationID]
		a, _ := json.Marshal(entry)
		b, _ := json.Marshal(current)
		if !bytes.Equal(a, b) {
			t.Fatalf("existing23 metadata altered %s", entry.OperationID)
		}
	}
	old := 0
	for _, op := range generatedOps() {
		if op.CrmOperation != "" {
			old++
		}
		entry := byID[op.OperationID]
		if entry.CrmOperation != op.CrmOperation || entry.Command != commandPath(op) {
			t.Fatal("old native77 changed")
		}
	}
	if old != 77 {
		t.Fatalf("native original baseline %d", old)
	}
}

func TestTaskTimeBillingInvalidInputsNeverDispatch(t *testing.T) {
	valid := taskTimeWriteWire("link")
	recovery := taskTimeRecoveryWire("link")
	cases := []struct{ name, action, wire, key string }{
		{"missing_confirmation", "link", strings.Replace(valid, `,"confirmed":true`, "", 1), taskTimeKey},
		{"false_confirmation", "link", strings.Replace(valid, `"confirmed":true`, `"confirmed":false`, 1), taskTimeKey},
		{"client_effect", "link", strings.Replace(valid, `"confirmed":true`, `"confirmed":true,"effect_id":"`+taskTimeEffectID+`"`, 1), taskTimeKey},
		{"spa_alias", "link", strings.Replace(valid, `"task_id"`, `"task_uuid"`, 1), taskTimeKey},
		{"duplicate_alias", "link", strings.Replace(valid, `"task_id":"`+crmTestOtherID+`"`, `"task_id":"`+crmTestOtherID+`","task_\u0069d":"`+crmTestOtherID+`"`, 1), taskTimeKey},
		{"zero_cas", "link", strings.Replace(valid, `"expected_ticket_version":7`, `"expected_ticket_version":0`, 1), taskTimeKey},
		{"max_php_cas", "link", strings.Replace(valid, `"expected_ticket_version":7`, `"expected_ticket_version":9223372036854775807`, 1), taskTimeKey},
		{"partial_binding", "link", strings.Replace(valid, `"confirmed":true`, `"confirmed":true,"binding_id":"`+taskTimeBindingID+`"`, 1), taskTimeKey},
		{"missing_key", "link", valid, ""},
		{"nonascii_key", "link", valid, "original-ñ"},
		{"edge_space_key", "link", valid, " original"},
		{"wrong_action", "recover-link", strings.Replace(recovery, `"action":"link"`, `"action":"unlink"`, 1), ""},
		{"unknown_effect", "recover-link", strings.Replace(recovery, `,"effect_id":"`+taskTimeEffectID+`"`, "", 1), ""},
		{"wire_recovery_forbidden", "recover-link", `{"original_wire_body":"{}"}`, ""},
		{"missing_binding_cas", "recover-unlink", taskTimeRecoveryWire("link"), ""},
		{"duplicate_entries", "preview", strings.Replace(taskTimePreviewWire(), `"entries":[`, `"entries":[{"id":"`+taskTimeEntryID+`","revision":"`+taskTimeRevision+`"},`, 1), ""},
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			args := []string{"crm", "task-time-billing", test.action, crmTestID, "--data", test.wire, "--json"}
			if test.key != "" {
				args = append(args, "--idempotency-key", test.key)
			}
			_, _, err := crmRun(t, server, args...)
			if err == nil || exit.ForError(err) != exit.Usage {
				t.Fatalf("invalid native input accepted %v", err)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid input contacted server %d", calls.Load())
	}
}

func TestTaskTimeBillingOriginalFileBytesAndNativeLargeWire(t *testing.T) {
	wire := strings.Repeat(" ", 70000) + strings.Replace(taskTimeWriteWire("link"), `"confirmed":true`, `"confirmed":true,"binding_id":null,"expected_binding_version":null`, 1)
	dir := t.TempDir()
	bodyFile, keyFile := filepath.Join(dir, "body.json"), filepath.Join(dir, "key.txt")
	if os.WriteFile(bodyFile, []byte(wire), 0600) != nil || os.WriteFile(keyFile, []byte(taskTimeKey), 0600) != nil {
		t.Fatal("fixture file write failed")
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "customer_service:write")
			return
		}
		calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		if string(raw) != wire || r.Header.Get("Idempotency-Key") != taskTimeKey {
			t.Error("original files normalized")
		}
		io.WriteString(w, nativeJSONEnvelope(taskTimeReceiptFixture("link")))
	}))
	defer server.Close()
	_, stderr, err := crmRun(t, server, "crm", "task-time-billing", "link", crmTestID, "--data-file", bodyFile, "--idempotency-key-file", keyFile, "--json")
	if err != nil || calls.Load() != 1 {
		t.Fatalf("large native body refused %v %s", err, stderr)
	}
}

func TestTaskTimeBillingReadPostOptionalHTTPKeyAndPagination(t *testing.T) {
	for _, action := range []string{"preview", "recover-link", "recover-unlink"} {
		t.Run(action, func(t *testing.T) {
			wire, response := taskTimePreviewWire(), nativeJSONEnvelope(taskTimeReadFixture("preview"))
			if action != "preview" {
				caseName := strings.TrimPrefix(action, "recover-")
				wire, response = taskTimeRecoveryWire(caseName), nativeJSONEnvelope(taskTimeReceiptFixture(caseName))
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "customer_service:read", "customer_service:write")
					return
				}
				if r.Header.Get("Idempotency-Key") != taskTimeKey {
					t.Error("explicit HTTP policy key discarded")
				}
				io.WriteString(w, response)
			}))
			defer server.Close()
			_, stderr, err := crmRun(t, server, "crm", "task-time-billing", action, crmTestID, "--data", wire, "--idempotency-key", taskTimeKey, "--json")
			if err != nil {
				t.Fatalf("readonly POST key %v %s", err, stderr)
			}
		})
	}
	cursor := taskTimeRevision + ":" + taskTimeEntryID
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "customer_service:read")
			return
		}
		if r.URL.Query().Get("cursor") != cursor || r.URL.Query().Get("limit") != "17" {
			t.Error("native page changed")
		}
		io.WriteString(w, nativeJSONEnvelope(taskTimeReadFixture("timesheet")))
	}))
	defer server.Close()
	_, stderr, err := crmRun(t, server, "crm", "task-time-billing", "timesheet", crmTestID, "--cursor", cursor, "--limit", "17", "--json")
	if err != nil {
		t.Fatalf("cursor rejected %v %s", err, stderr)
	}
}

func TestTaskTimeBillingLostAckNeverAllocatesOrResends(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "customer_service:write")
			return
		}
		calls.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		connection.Close()
	}))
	defer server.Close()
	out, stderr, err := crmRun(t, server, "crm", "task-time-billing", "link", crmTestID, "--data", taskTimeWriteWire("link"), "--idempotency-key", taskTimeKey, "--json")
	if err == nil || exit.ForError(err) != exit.Network || calls.Load() != 1 || out != "" || !strings.Contains(stderr, "sin esos IDs") || strings.Contains(stderr, "bookings") || strings.Contains(stderr, taskTimeKey) {
		t.Fatalf("unknown falsely reconciled/replayed %d %v %s", calls.Load(), err, stderr)
	}
}

func TestTaskTimeBillingNativeErrorsPreserveCodeAndNoReplay(t *testing.T) {
	for _, status := range []int{401, 403, 404, 409, 422, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "customer_service:write")
					return
				}
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				io.WriteString(w, `{"error":{"type":"api_error","code":"crm_operation_unavailable","message":"Native original failure","subcode":"not_implemented","param":"expected_version","request_id":"native-task-request","details":{"safe":"retained"}}}`)
			}))
			defer server.Close()
			_, stderr, err := crmRun(t, server, "crm", "task-time-billing", "link", crmTestID, "--data", taskTimeWriteWire("link"), "--idempotency-key", taskTimeKey, "--json")
			if err == nil || calls.Load() != 1 || !strings.Contains(stderr, "crm_operation_unavailable") || !strings.Contains(stderr, "not_implemented") || !strings.Contains(stderr, "expected_version") || !strings.Contains(stderr, "native-task-request") {
				t.Fatalf("native code lost/replayed %d %v %s", calls.Load(), err, stderr)
			}
			if status < 500 && strings.Contains(stderr, "resultado no confirmado") {
				t.Fatal("native business denial overwritten")
			}
			var native *apierr.APIError
			if !errors.As(err, &native) || native.StatusCode != status {
				t.Fatalf("native HTTP status changed %v: %s", err, stderr)
			}
		})
	}
}

func TestTaskTimeBillingRejectsForeignReceiptsAndPrivateFields(t *testing.T) {
	tests := []struct {
		name, action, wire string
		data               map[string]any
		mutating           bool
	}{
		{"different_effect", "recover-link", taskTimeRecoveryWire("link"), taskTimeReceiptFixture("link"), false},
		{"different_ticket", "link", taskTimeWriteWire("link"), taskTimeReceiptFixture("link"), true},
		{"current_head_substitute", "link", taskTimeWriteWire("link"), taskTimeReceiptFixture("link"), true},
		{"task_alias", "tasks", "", taskTimeReadFixture("tasks"), false},
		{"private_description", "tasks", "", taskTimeReadFixture("tasks"), false},
		{"money_float", "preview", taskTimePreviewWire(), taskTimeReadFixture("preview"), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			switch test.name {
			case "different_effect":
				test.data["effect_id"] = taskTimeEntryID
			case "different_ticket":
				test.data["ticket_id"] = crmTestOtherID
			case "current_head_substitute":
				test.data["ticket_version"] = 9
			case "task_alias":
				test.data["tasks"].([]any)[0].(map[string]any)["task_uuid"] = crmTestOtherID
			case "private_description":
				test.data["tasks"].([]any)[0].(map[string]any)["description"] = "hidden task content"
			case "money_float":
				test.data["preview"].(map[string]any)["lines"].([]any)[0].(map[string]any)["amount"] = 1.25
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "customer_service:read", "customer_service:write")
					return
				}
				calls.Add(1)
				io.WriteString(w, nativeJSONEnvelope(test.data))
			}))
			defer server.Close()
			args := []string{"crm", "task-time-billing", test.action, crmTestID, "--json"}
			if test.wire != "" {
				args = append(args, "--data", test.wire)
			}
			if test.mutating {
				args = append(args, "--idempotency-key", taskTimeKey)
			}
			out, _, err := crmRun(t, server, args...)
			if err == nil || out != "" || calls.Load() != 1 {
				t.Fatalf("foreign/private response accepted %d %v %s", calls.Load(), err, out)
			}
			if test.mutating && exit.ForError(err) != exit.Network {
				t.Fatal("invalid ACK invented confirmation")
			}
		})
	}
}

func TestTaskTimeBillingExplicitAggregationAndMoneyNullOmission(t *testing.T) {
	for _, state := range []string{"omitted", "null", "value"} {
		t.Run(state, func(t *testing.T) {
			preview := taskTimeReadFixture("preview")
			body := preview["preview"].(map[string]any)
			if state == "omitted" {
				delete(body, "totals")
			}
			if state == "value" {
				body["totals"] = map[string]any{"subtotal": "100.0100", "vat": "21.0021", "surcharge": "0.0000", "retention": "0.0000", "total": "121.0121", "vat_rate": 21, "surcharge_rate": nil, "retention_rate": nil}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "customer_service:read")
					return
				}
				io.WriteString(w, nativeJSONEnvelope(preview))
			}))
			defer server.Close()
			out, stderr, err := crmRun(t, server, "crm", "task-time-billing", "preview", crmTestID, "--data", taskTimePreviewWire(), "--json")
			if err != nil {
				t.Fatalf("Money projection rejected %v %s", err, stderr)
			}
			var envelope map[string]any
			json.Unmarshal([]byte(out), &envelope)
			p := envelope["data"].(map[string]any)["preview"].(map[string]any)
			value, present := p["totals"]
			if (state == "omitted" && present) || (state == "null" && (!present || value != nil)) || (state == "value" && value.(map[string]any)["total"] != "121.0121") {
				t.Fatal("Money omission/null/string changed")
			}
		})
	}
	data := taskTimeReadFixture("billable")
	data["total_billable_closed_seconds"] = 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "customer_service:read")
			return
		}
		io.WriteString(w, nativeJSONEnvelope(data))
	}))
	defer server.Close()
	out, stderr, err := crmRun(t, server, "crm", "task-time-billing", "billable", crmTestID, "--json")
	if err != nil || !strings.Contains(out, `"total_billable_closed_seconds":0`) {
		t.Fatalf("explicit native zero lost %v %s", err, stderr)
	}
}

func TestTaskTimeBillingInsufficientScopesAndKnownBindingNoop(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "customer_service:read")
			return
		}
		writes.Add(1)
	}))
	defer server.Close()
	_, _, err := crmRun(t, server, "crm", "task-time-billing", "unlink", crmTestID, "--data", taskTimeWriteWire("unlink"), "--idempotency-key", taskTimeKey, "--json")
	if err == nil || exit.ForError(err) != exit.Perm || writes.Load() != 0 {
		t.Fatal("read scope dispatched native writer")
	}
	data := taskTimeReceiptFixture("link")
	binding := data["result"].(map[string]any)["binding"].(map[string]any)
	binding["version"] = 3
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "customer_service:write")
			return
		}
		io.WriteString(w, nativeJSONEnvelope(data))
	}))
	defer server2.Close()
	wire := strings.Replace(taskTimeWriteWire("link"), `"confirmed":true`, `"confirmed":true,"binding_id":"`+taskTimeBindingID+`","expected_binding_version":3`, 1)
	_, stderr, err := crmRun(t, server2, "crm", "task-time-billing", "link", crmTestID, "--data", wire, "--idempotency-key", taskTimeKey, "--json")
	if err != nil {
		t.Fatalf("native same-state binding transition guessed increment %v %s", err, stderr)
	}
}
