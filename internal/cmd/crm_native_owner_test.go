package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

const nativePublicID = "public_booking_identifier_original_68"
const nativePublicKey = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const nativePrivateToken = "private-original-management-token-68"
const nativeSequenceDefinitionWire = `{"timezone":"Europe/Madrid","ambiguous_offset":"later","allowed_windows":[{"weekday":7,"start_minute":60,"end_minute":240}],"steps":[{"type":"wait","wait_seconds":3600,"recipe_id":null}],"stop_on_closure":true,"purpose":"follow_up","channel":"email","stop_conditions":["complaint","booking_confirmed"]}`
const nativeBookingCreateWire = " {\"slot\":{\"start_at\":\"2026-10-25T09:00:00Z\",\"end_at\":\"2026-10-25T09:30:00Z\"},\"visitor_fields\":{\"name\":\"Visita <Original>\",\"email\":\"private@example.com\"},\"notice_version\":\"notice-7\",\"captcha_token\":\"private-captcha\"}\n"

func nativeScheduleFixture(version int) map[string]any {
	return map[string]any{"id": crmTestID, "version": version, "authorization_version": "native-current-c7", "sandbox": true, "state": "active", "owner_id": crmTestOtherID, "team_id": nil, "fields": map[string]any{}}
}
func nativeEnrolFixture() map[string]any {
	return map[string]any{"id": crmTestOtherID, "version": 1, "authorization_version": "native-current-c7", "sandbox": true, "sequence_id": crmTestID, "subject_type": "lead", "subject_id": crmTestOtherID, "generation": 1, "definition_version": 7, "state": "active", "current_step": 0, "due_at": nil, "stop_reason": nil, "fields": map[string]any{}}
}
func nativePublicFixture(status string) map[string]any {
	return map[string]any{"id": crmTestID, "version": 4, "status": status, "start_at": "2026-10-25T09:00:00Z", "end_at": "2026-10-25T09:30:00Z", "effect_id": crmTestOtherID}
}
func nativeJSONEnvelope(data any) string {
	b, _ := json.Marshal(map[string]any{"data": data})
	return string(b)
}

type nativeBindingCase struct {
	command                            []string
	id, operation, method, path, scope string
	status                             int
	response, wire                     string
	original, visitor, recovery        bool
}

func nativeBindingCases() []nativeBindingCase {
	saveNew := ` {"expected_version":0,"definition":` + nativeSequenceDefinitionWire + `} `
	saveExisting := ` {"expected_version":7,"definition":` + nativeSequenceDefinitionWire + `} `
	enrol := ` {"subject_type":"lead","subject_id":"` + crmTestOtherID + `","expected_definition_version":7} `
	bookNew := ` {"definition":{"state":"draft","location":null}} `
	bookSave := ` {"expected_version":7,"definition":{"state":"paused"}} `
	management := nativePublicFixture("confirmed")
	delete(management, "effect_id")
	publicReceipt := nativePublicFixture("confirmed")
	publicReceipt["manage_token"] = nativePrivateToken
	publicCreateReceipt := nativePublicFixture("confirmed")
	publicCreateReceipt["version"] = 2
	publicCreateReceipt["manage_token"] = nativePrivateToken
	page := nativeJSONEnvelope(map[string]any{"items": []any{nativeScheduleFixture(7)}, "enrolments": []any{}, "resource": "schedules", "next_cursor": nil, "visible_count": 1})
	bookingPage := nativeJSONEnvelope(map[string]any{"items": []any{map[string]any{"id": crmTestID, "version": 7}}, "total": 1, "next_cursor": nil})
	config := nativeJSONEnvelope(map[string]any{"public_id": nativePublicID, "publication_version": 7, "business_timezone": "Europe/Madrid", "is_sandbox": true, "service_display": "Consulta", "duration_minutes": 30, "buffer_before": 0, "buffer_after": 0, "notice_version": "notice-7", "notice_copy": "Aviso original", "captcha": map[string]any{"provider": "none"}})
	availability := nativeJSONEnvelope(map[string]any{"public_id": nativePublicID, "publication_version": 7, "business_timezone": "Europe/Madrid", "visitor_timezone": "Europe/London", "slots": []any{}, "as_of": "2026-10-01T00:00:00Z", "notice_version": "notice-7", "is_sandbox": true})
	reschedule := ` {"manage_token":"` + nativePrivateToken + `","expected_version":3,"new_slot":{"start_at":"2026-10-25T09:00:00Z","end_at":"2026-10-25T09:30:00Z"}} `
	cancel := ` {"manage_token":"` + nativePrivateToken + `","expected_version":3} `
	return []nativeBindingCase{
		{[]string{"crm", "sequences", "list", "--filters", `{"resource":"schedules"}`, "--limit", "17"}, "v1.crm.sequences.list", "automation.crm_sequence_list", "GET", "/v1/crm/crmsequence", "automation:read", 200, page, "", false, false, false},
		{[]string{"crm", "sequences", "preview", crmTestID, "--subject_type", "lead", "--subject_id", crmTestOtherID, "--start_at", "2026-10-25T09:00:00+01:00"}, "v1.crm.sequences.preview", "automation.crm_sequence_preview", "GET", "/v1/crm/crmsequence/" + crmTestID + "/preview", "automation:read", 200, nativeJSONEnvelope(map[string]any{"sequence_id": crmTestID, "definition_version": 7, "subject_id": crmTestOtherID, "timezone": "Europe/Madrid", "steps": []any{}}), "", false, false, false},
		{[]string{"crm", "sequences", "create", "--confirm", "create"}, "v1.crm.sequences.save_new", "automation.crm_sequence_save", "POST", "/v1/crm/crmsequence", "automation:write", 200, nativeJSONEnvelope(nativeScheduleFixture(1)), saveNew, true, false, false},
		{[]string{"crm", "sequences", "save", crmTestID, "--confirm", crmTestID}, "v1.crm.sequences.save_existing", "automation.crm_sequence_save", "POST", "/v1/crm/crmsequence/" + crmTestID, "automation:write", 200, nativeJSONEnvelope(nativeScheduleFixture(8)), saveExisting, true, false, false},
		{[]string{"crm", "sequences", "enrol", crmTestID, "--confirm", crmTestID}, "v1.crm.sequences.enrol", "automation.crm_sequence_enrol", "POST", "/v1/crm/crmsequence/" + crmTestID + "/enrolments", "automation:write", 200, nativeJSONEnvelope(nativeEnrolFixture()), enrol, true, false, false},
		{[]string{"crm", "sequences", "recover-save"}, "v1.crm.sequences.original_save", "automation.crm_sequence_save_original", "GET", "/v1/crm/crmsequence/original", "automation:write", 200, nativeJSONEnvelope(nativeScheduleFixture(8)), "", true, false, false},
		{[]string{"crm", "sequences", "recover-enrol"}, "v1.crm.sequences.original_enrol", "automation.crm_sequence_enrol_original", "GET", "/v1/crm/crmsequence/enrolments/original", "automation:write", 200, nativeJSONEnvelope(nativeEnrolFixture()), "", true, false, false},
		{[]string{"crm", "sequences", "pause", crmTestOtherID, "--confirm", crmTestOtherID}, "v1.crm.sequences.pause", "automation.crm_sequence_pause", "PATCH", "/v1/crm/crmsequence/enrolments/" + crmTestOtherID + "/pause", "automation:write", 204, "", ` {"expected_version":3,"reason":"manual"} `, false, false, false},
		{[]string{"crm", "sequences", "resume", crmTestOtherID, "--confirm", crmTestOtherID}, "v1.crm.sequences.resume", "automation.crm_sequence_resume", "PATCH", "/v1/crm/crmsequence/enrolments/" + crmTestOtherID + "/resume", "automation:write", 204, "", ` {"expected_version":3} `, false, false, false},
		{[]string{"crm", "sequences", "cancel", crmTestOtherID, "--confirm", crmTestOtherID}, "v1.crm.sequences.cancel", "automation.crm_sequence_cancel", "PATCH", "/v1/crm/crmsequence/enrolments/" + crmTestOtherID + "/cancel", "automation:write", 204, "", ` {"expected_version":3} `, false, false, false},
		{[]string{"crm", "bookings", "list-services"}, "listBookingServices", "booking.list_services", "GET", "/v1/crm/booking/services", "booking:read", 200, bookingPage, "", false, false, false},
		{[]string{"crm", "bookings", "get-service", crmTestID}, "getBookingService", "booking.get_service", "GET", "/v1/crm/booking/services/" + crmTestID, "booking:read", 200, nativeJSONEnvelope(map[string]any{"id": crmTestID, "version": 7}), "", false, false, false},
		{[]string{"crm", "bookings", "create-service", "--confirm", "create"}, "createBookingService", "booking.save_service", "POST", "/v1/crm/booking/services", "booking:write", 201, nativeJSONEnvelope(map[string]any{"id": crmTestID, "version": 1}), bookNew, true, false, false},
		{[]string{"crm", "bookings", "save-service", crmTestID, "--confirm", crmTestID}, "saveBookingService", "booking.save_service", "PUT", "/v1/crm/booking/services/" + crmTestID, "booking:write", 200, nativeJSONEnvelope(map[string]any{"id": crmTestID, "version": 8}), bookSave, true, false, false},
		{[]string{"crm", "bookings", "recover-service", crmTestID}, "recoverBookingServiceReceipt", "booking.service_receipt", "POST", "/v1/crm/booking/services/receipts/" + crmTestID, "booking:write", 200, nativeJSONEnvelope(map[string]any{"id": crmTestID, "version": 8}), bookSave, true, false, true},
		{[]string{"crm", "bookings", "list"}, "listBookings", "booking.list_bookings", "GET", "/v1/crm/booking/bookings", "booking:read", 200, bookingPage, "", false, false, false},
		{[]string{"crm", "bookings", "public", "configuration", nativePublicID}, "crm.booking.public.configuration", "booking.public_configuration", "GET", "/public/crm/booking/" + nativePublicID + "/configuration", "", 200, config, "", false, true, false},
		{[]string{"crm", "bookings", "public", "availability", nativePublicID, "--publication_version", "7", "--from", "2026-10-25T00:00:00Z", "--to", "2026-10-26T00:00:00Z", "--visitor_timezone", "Europe/London"}, "crm.booking.public.availability", "booking.get_public_availability", "GET", "/public/crm/booking/" + nativePublicID + "/availability", "", 200, availability, "", false, true, false},
		{[]string{"crm", "bookings", "public", "create", nativePublicID, "--publication-version", "7", "--live"}, "crm.booking.public.create", "booking.create_public", "POST", "/public/crm/booking/" + nativePublicID + "/bookings", "", 201, nativeJSONEnvelope(publicCreateReceipt), nativeBookingCreateWire, true, true, false},
		{[]string{"crm", "bookings", "public", "recover", nativePublicID, "--publication-version", "7", "--operation", "booking.reschedule_public"}, "crm.booking.public.receipt", "booking.public_receipt", "POST", "/public/crm/booking/" + nativePublicID + "/receipts", "", 200, nativeJSONEnvelope(publicReceipt), reschedule, true, true, true},
		{[]string{"crm", "bookings", "public", "management", nativePublicID}, "crm.booking.public.management", "booking.get_public_management", "GET", "/public/crm/booking/" + nativePublicID + "/management", "", 200, nativeJSONEnvelope(management), "", false, true, false},
		{[]string{"crm", "bookings", "public", "reschedule", "--live"}, "crm.booking.public.reschedule", "booking.reschedule_public", "POST", "/public/crm/booking/manage/reschedule", "", 200, nativeJSONEnvelope(publicReceipt), reschedule, true, true, false},
		{[]string{"crm", "bookings", "public", "cancel", "--live", "--confirm", "cancel"}, "crm.booking.public.cancel", "booking.cancel_public", "POST", "/public/crm/booking/manage/cancel", "", 200, nativeJSONEnvelope(nativePublicFixture("cancelled")), cancel, true, true, false},
	}
}

func TestNativeOwner23ActualHTTPBindings(t *testing.T) {
	for _, test := range nativeBindingCases() {
		t.Run(test.id, func(t *testing.T) {
			calls, account := 0, 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					account++
					crmAccount(w, test.scope)
					return
				}
				calls++
				if r.Method != test.method || r.URL.Path != test.path {
					t.Errorf("native endpoint changed: %s %s", r.Method, r.URL.Path)
				}
				body, _ := io.ReadAll(r.Body)
				if test.recovery {
					var envelope map[string]string
					if json.Unmarshal(body, &envelope) != nil || envelope["original_wire_body"] != test.wire {
						t.Error("original wire recovery changed")
					}
					if test.visitor && envelope["operation"] != "booking.reschedule_public" {
						t.Error("original operation changed")
					}
					if len(r.URL.Query()) != 0 {
						t.Error("private recovery in query")
					}
				} else if string(body) != test.wire {
					t.Errorf("wire changed: %s", body)
				}
				if test.original {
					expected := crmTestKey
					if test.visitor {
						expected = nativePublicKey
					}
					if r.Header.Get("Idempotency-Key") != expected {
						t.Error("original key changed")
					}
				} else if _, has := r.Header["Idempotency-Key"]; has {
					t.Error("native header absent contract lost")
				}
				if test.visitor {
					if _, has := r.Header["Authorization"]; has {
						t.Error("visitor borrowed staff credential")
					}
				} else if r.Header.Get("Authorization") == "" {
					t.Error("staff credential lost")
				}
				if test.id == "v1.crm.sequences.list" && (r.URL.Query().Get("limit") != "17" || r.URL.Query().Get("filters") != `{"resource":"schedules"}`) {
					t.Error("explicit query changed")
				}
				if test.id == "listBookingServices" && len(r.URL.Query()) != 0 {
					t.Error("invented pagination default")
				}
				if test.id == "crm.booking.public.management" && (r.Header.Get("Booking-Management-Token") != nativePrivateToken || len(r.URL.Query()) != 0 || len(body) != 0) {
					t.Error("management token not header-only")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.response))
			}))
			defer s.Close()
			t.Setenv("FACTUAREA_BOOKING_MANAGEMENT_TOKEN", nativePrivateToken)
			args := append(append([]string{}, test.command...), "--json")
			if test.wire != "" {
				args = append(args, "--data", test.wire)
			}
			if test.original {
				key := crmTestKey
				if test.visitor {
					key = nativePublicKey
				}
				args = append(args, "--idempotency-key", key)
			}
			out, stderr, err := crmRun(t, s, args...)
			if err != nil || stderr != "" || calls != 1 {
				t.Fatalf("calls=%d error=%v stderr=%s", calls, err, stderr)
			}
			if test.visitor && account != 0 {
				t.Error("visitor queried corporate account")
			}
			if test.status == 204 && out != "" {
				t.Error("204 invented a receipt")
			}
			if test.visitor && (strings.Contains(out, nativePrivateToken) || strings.Contains(stderr, nativePublicKey) || strings.Contains(out, "private@example.com")) {
				t.Fatal("public secret exposed")
			}
		})
	}
}

func TestNativeOwnerDiscoveryPreservesOld77AndDeclares23RealContracts(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"commands"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var entries []manifestEntry
	if json.Unmarshal(out.Bytes(), &entries) != nil {
		t.Fatal("invalid discovery")
	}
	byID := map[string]manifestEntry{}
	native := 0
	for _, entry := range entries {
		if _, duplicate := byID[entry.OperationID]; duplicate {
			t.Fatalf("duplicate %s", entry.OperationID)
		}
		byID[entry.OperationID] = entry
		if entry.AuthRealm != "" && (strings.HasPrefix(entry.Command, "factuarea crm sequences ") || strings.HasPrefix(entry.Command, "factuarea crm bookings ")) {
			native++
		}
	}
	if native != 23 {
		t.Fatalf("native count=%d", native)
	}
	for _, test := range nativeBindingCases() {
		entry, exists := byID[test.id]
		if !exists || entry.CrmOperation != test.operation || entry.Method != test.method || entry.RequiredScope != test.scope || entry.IdempotencyRequired != test.original || len(entry.ResponseSchemas) == 0 {
			t.Fatalf("native discovery lost: %+v", entry)
		}
		if test.visitor && (entry.RequiredScope != "" || len(entry.RequiredScopes) > 0 || entry.AuthRealm != "public_booking") {
			t.Fatal("fake corporate visitor scope")
		}
		if test.visitor && entry.OperationIDSource != "native_public_route" {
			t.Fatal("public route anchor claimed protected V1/OAS operation")
		}
		if !test.visitor && entry.OperationIDSource != "native_v1_surface" {
			t.Fatal("native V1 identity lost")
		}
		if strings.Contains(entry.NativePath, "/api/") {
			t.Fatal("SPA invented as native API")
		}
	}
	oldCount := 0
	for _, op := range generatedOps() {
		if op.CrmOperation != "" {
			oldCount++
		}
		entry := byID[op.OperationID]
		if entry.Command != commandPath(op) || entry.CrmOperation != op.CrmOperation || entry.RequiredScope != op.RequiredScope || entry.Mutating != op.isMutating() {
			t.Fatalf("old command mutated %s", op.OperationID)
		}
	}
	if oldCount != 77 {
		t.Fatalf("old native baseline changed %d", oldCount)
	}
	for _, name := range []string{"pause", "resume", "cancel"} {
		entry := byID["v1.crm.sequences."+name]
		if entry.IdempotencyRequired || entry.OriginalRecovery || entry.ResponseSchemas["204"] == nil || !entry.RequiresConfirmation || !entry.LocalConfirmation {
			t.Fatal("controls invented original receipts")
		}
	}
}

func TestNativeOwnerMalformedInputsFailBeforeHTTP(t *testing.T) {
	cases := [][]string{
		{"crm", "sequences", "create", "--data", `{"expected_version":0,"definition":{},"confirmed":true}`, "--confirm", "create", "--idempotency-key", crmTestKey},
		{"crm", "sequences", "pause", crmTestID, "--data", `{"expected_version":3,"reason":"privacy"}`},
		{"crm", "sequences", "pause", crmTestID, "--data", `{"expected_version":3,"expected_\u0076ersion":4,"reason":"manual"}`},
		{"crm", "sequences", "resume", crmTestID, "--data", `{"expected_version":3}`, "--idempotency-key", crmTestKey},
		{"crm", "sequences", "list", "--filters", `{"resource":"schedules","stop_reason":"optout"}`, "--limit", "20"},
		{"crm", "sequences", "list", "--filters", `{"resource":"schedules"}`},
		{"crm", "sequences", "enrol", crmTestID, "--data", `{"subject_type":"lead","subject_uuid":"` + crmTestOtherID + `","expected_definition_version":7}`, "--confirm", crmTestID, "--idempotency-key", crmTestKey},
		{"crm", "bookings", "recover-service", crmTestID, "--data", `{"definition":{}}`, "--idempotency-key", crmTestKey},
		{"crm", "bookings", "public", "create", nativePublicID, "--data", nativeBookingCreateWire, "--publication-version", "7", "--idempotency-key", "unissued-key", "--live"},
		{"crm", "bookings", "public", "recover", nativePublicID, "--data", nativeBookingCreateWire, "--publication-version", "7", "--operation", "booking.cancel_public", "--idempotency-key", nativePublicKey},
		{"crm", "bookings", "create-service", "--data", `{"definition":{}}`, "--confirm", "create", "--idempotency-key", " trimmed-original"},
	}
	for i, args := range cases {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
			defer s.Close()
			_, _, err := crmRun(t, s, args...)
			if err == nil || calls != 0 {
				t.Fatalf("invalid input reached network: %d %v", calls, err)
			}
		})
	}
}

func TestNativeOwnerOriginalRecoveryCreateHasNoTargetAndPreservesNull(t *testing.T) {
	calls := 0
	wire := " {\"definition\":{\"state\":\"draft\"}}\n"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "booking:write")
			return
		}
		calls++
		if r.Method != "POST" || r.URL.Path != "/v1/crm/booking/services/receipts" {
			t.Error("new receipt target fabricated")
		}
		var original map[string]string
		_ = json.NewDecoder(r.Body).Decode(&original)
		if original["original_wire_body"] != wire {
			t.Error("wire normalized")
		}
		_, _ = w.Write([]byte(`{"data":null}`))
	}))
	defer s.Close()
	out, _, err := crmRun(t, s, "crm", "bookings", "recover-service", "--data", wire, "--idempotency-key", crmTestKey, "--json")
	if err != nil || calls != 1 || strings.TrimSpace(out) != `{"data":null}` {
		t.Fatalf("absence falsely confirmed: %q %v", out, err)
	}
}

func TestNativeOwnerUnknownControlNeverConfirmsOrReplays(t *testing.T) {
	for _, response := range []string{`{"data":{"id":"` + crmTestID + `","confirmed":true}}`, `{broken`} {
		t.Run(response, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "automation:write")
					return
				}
				calls++
				w.WriteHeader(200)
				_, _ = w.Write([]byte(response))
			}))
			defer s.Close()
			out, stderr, err := crmRun(t, s, "crm", "sequences", "pause", crmTestID, "--confirm", crmTestID, "--data", `{"expected_version":3,"reason":"manual"}`, "--json")
			if exit.ForError(err) != exit.Network || calls != 1 || out != "" || !strings.Contains(stderr, "resultado desconocido") || strings.Contains(stderr, "recibo") {
				t.Fatalf("ambiguous control was confirmed/recovered %d %q %q %v", calls, out, stderr, err)
			}
		})
	}
}

func TestNativeOwnerNativeErrorsKeepCodesCASAndNoReplay(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "booking:write")
			return
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":{"type":"conflict_error","code":"booking_cas_conflict","message":"Versión obsoleta","subcode":"cas","param":"expected_version","request_id":"native-request-68","details":{"expected_version":7}}}`))
	}))
	defer s.Close()
	out, stderr, err := crmRun(t, s, "crm", "bookings", "save-service", crmTestID, "--data", `{"definition":{"state":"paused"},"expected_version":7}`, "--confirm", crmTestID, "--idempotency-key", crmTestKey, "--json")
	if calls != 1 || out != "" || exit.ForError(err) != exit.Conflict || !strings.Contains(stderr, "booking_cas_conflict") || !strings.Contains(stderr, "native-request-68") || !strings.Contains(stderr, "expected_version") {
		t.Fatalf("native error lost %d %q %v", calls, stderr, err)
	}
}

func TestNativeOwnerPublicTokenExportIsPrivateExclusiveAndRedacted(t *testing.T) {
	file := filepath.Join(t.TempDir(), "native-token")
	calls := 0
	response := nativePublicFixture("confirmed")
	response["version"] = 2
	response["manage_token"] = nativePrivateToken
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/v1/account" {
			t.Fatal("visitor read staff account")
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(nativeJSONEnvelope(response)))
	}))
	defer s.Close()
	args := []string{"crm", "bookings", "public", "create", nativePublicID, "--data", nativeBookingCreateWire, "--publication-version", "7", "--idempotency-key", nativePublicKey, "--live", "--secret-output-file", file, "--json"}
	out, stderr, err := crmRun(t, s, args...)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	info, _ := os.Stat(file)
	if string(b) != nativePrivateToken || info.Mode().Perm() != 0600 || strings.Contains(out, nativePrivateToken) || strings.Contains(stderr, nativePublicKey) || !strings.Contains(out, "[redacted]") {
		t.Fatal("private token leaked/lost")
	}
	_, _, err = crmRun(t, s, args...)
	if err == nil || calls != 1 {
		t.Fatal("existing secret output overwritten or second original sent")
	}
}

func TestNativeOwnerPublicDenialRedactsNativePrivateDetails(t *testing.T) {
	t.Setenv("FACTUAREA_BOOKING_MANAGEMENT_TOKEN", nativePrivateToken)
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"data":{"manage_token":"` + nativePrivateToken + `"},"error":{"type":"rate_limit_error","code":"booking_unavailable","message":"` + nativePrivateToken + `","details":{"key":"` + nativePublicKey + `"}}}`))
	}))
	defer s.Close()
	out, stderr, err := crmRun(t, s, "crm", "bookings", "public", "management", nativePublicID, "--json")
	if calls != 1 || exit.ForError(err) != exit.RateLimit || !strings.Contains(stderr, "booking_unavailable") {
		t.Fatalf("native denial/retry contract lost: calls=%d err=%v stderr=%s", calls, err, stderr)
	}
	if out != "" || strings.Contains(stderr, nativePrivateToken) || strings.Contains(stderr, nativePublicKey) {
		t.Fatal("public denial leaked secret")
	}
}

func TestNativeOwnerDiscoveryIsDeterministic(t *testing.T) {
	first, _ := json.Marshal(nativeOwnerManifest())
	second, _ := json.Marshal(nativeOwnerManifest())
	if !reflect.DeepEqual(first, second) {
		t.Fatal("discovery unstable between reads")
	}
}

func TestNativeOwnerInt64CASAndExactFileBytes(t *testing.T) {
	wire := " {\"expected_version\":9007199254740993,\"definition\":" + nativeSequenceDefinitionWire + "}\n"
	file := filepath.Join(t.TempDir(), "original.json")
	if os.WriteFile(file, []byte(wire), 0600) != nil {
		t.Fatal("fixture file")
	}
	keyFile := filepath.Join(t.TempDir(), "original.key")
	if os.WriteFile(keyFile, []byte(crmTestKey), 0600) != nil {
		t.Fatal("fixture key")
	}
	response := nativeScheduleFixture(1)
	response["version"] = json.Number("9007199254740994")
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "automation:write")
			return
		}
		calls++
		b, _ := io.ReadAll(r.Body)
		if string(b) != wire || r.Header.Get("Idempotency-Key") != crmTestKey {
			t.Fatal("original file bytes/key changed")
		}
		_, _ = w.Write([]byte(nativeJSONEnvelope(response)))
	}))
	defer s.Close()
	out, _, err := crmRun(t, s, "crm", "sequences", "save", crmTestID, "--data-file", file, "--idempotency-key-file", keyFile, "--confirm", crmTestID, "--json")
	if err != nil || calls != 1 || !strings.Contains(out, "9007199254740994") {
		t.Fatalf("CAS rounded %q %v", out, err)
	}
}

func TestNativeOwnerRejectsDifferentHeadAndForeignReceipt(t *testing.T) {
	for _, mutate := range []func(map[string]any){func(v map[string]any) { v["version"] = 19 }, func(v map[string]any) { v["id"] = crmTestOtherID }, func(v map[string]any) { v["confirmed"] = true }} {
		response := nativeScheduleFixture(8)
		mutate(response)
		calls := 0
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/account" {
				crmAccount(w, "automation:write")
				return
			}
			calls++
			_, _ = w.Write([]byte(nativeJSONEnvelope(response)))
		}))
		out, stderr, err := crmRun(t, s, "crm", "sequences", "save", crmTestID, "--data", `{"expected_version":7,"definition":`+nativeSequenceDefinitionWire+`}`, "--confirm", crmTestID, "--idempotency-key", crmTestKey, "--json")
		s.Close()
		if calls != 1 || out != "" || exit.ForError(err) != exit.Network || !strings.Contains(stderr, "recover") || strings.Contains(stderr, crmTestKey) {
			t.Fatalf("head/foreign receipt confirmed %q %v", out, err)
		}
	}
}

func TestNativeOwnerInsufficientFineScopeNeverWrites(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "automation:read")
			return
		}
		calls++
	}))
	defer s.Close()
	_, _, err := crmRun(t, s, "crm", "sequences", "pause", crmTestID, "--confirm", crmTestID, "--data", `{"expected_version":3,"reason":"manual"}`)
	if calls != 0 || exit.ForError(err) != exit.Perm {
		t.Fatalf("invented write grant: %d %v", calls, err)
	}
}

func TestNativeOwnerVisitorDoesNotResolveInvalidCorporateKey(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "invalid-corporate-key-must-not-be-used")
	t.Setenv("FACTUAREA_BOOKING_MANAGEMENT_TOKEN", nativePrivateToken)
	calls := 0
	response := nativePublicFixture("confirmed")
	delete(response, "effect_id")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/public/crm/booking/"+nativePublicID+"/management" || r.Header.Get("Authorization") != "" {
			t.Error("visitor borrowed staff realm")
		}
		_, _ = w.Write([]byte(nativeJSONEnvelope(response)))
	}))
	defer s.Close()
	t.Setenv("FACTUAREA_BASE_URL", s.URL)
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"crm", "bookings", "public", "management", nativePublicID, "--json"})
	if err := root.Execute(); err != nil || calls != 1 {
		t.Fatalf("visitor required corporate credential: %d %v", calls, err)
	}
}

func TestNativeOwnerNativeBookingMapRejectsPrivateProjectionAndMalformedStatuses(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "booking:read")
			return
		}
		calls++
		_, _ = w.Write([]byte(nativeJSONEnvelope(map[string]any{"items": []any{map[string]any{"id": crmTestID, "version": 7, "source_statuses": map[string]any{"booking": map[string]any{"verified": true, "revision": strings.Repeat("a", 64), "reason": nil, "private_host_calendar": "secret"}}}}, "total": 1, "next_cursor": nil})))
	}))
	defer s.Close()
	out, _, err := crmRun(t, s, "crm", "bookings", "list-services", "--json")
	if err == nil || out != "" || calls != 1 {
		t.Fatal("native projection gained undeclared fields")
	}
}

func TestNativeOwnerServerAmbiguityPreservesNativeCodeAndHasNoControlRecovery(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "automation:write")
			return
		}
		calls++
		if _, present := r.Header["Idempotency-Key"]; present {
			t.Error("CAS-only control got original header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":{"type":"api_error","code":"native_effect_unconfirmed","message":"No confirmado","request_id":"request-68"}}`))
	}))
	defer s.Close()
	out, stderr, err := crmRun(t, s, "crm", "sequences", "resume", crmTestID, "--confirm", crmTestID, "--data", `{"expected_version":3}`, "--json")
	if calls != 1 || out != "" || exit.ForError(err) != exit.Server || !strings.Contains(stderr, "native_effect_unconfirmed") || !strings.Contains(stderr, "resultado desconocido") || !strings.Contains(stderr, "estado actual") || strings.Contains(stderr, "recover") {
		t.Fatalf("control ambiguity changed: %q %v", stderr, err)
	}
}
