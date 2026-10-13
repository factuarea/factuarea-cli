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

const insightsStart = "2026-01-01T00:00:00.123456Z"
const insightsEnd = "2026-01-02T00:00:00.123457Z"
const insightsAsOf = "2026-01-02T12:00:00.654321Z"
const insightsPredicate = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const insightsHistoricalAgent = "F47AC10B-58CC-4372-A567-0E02B2C3D479"

func insightsCsatFixture() map[string]any {
	return map[string]any{"metric": "csat", "metric_version": 1, "cohort_start": insightsStart, "cohort_end": insightsEnd, "cohort_basis": "original_closed_at", "cycle_policy": "each_closure_participant", "clock_mode": "not_applicable", "invited": json.Number("9007199254740993"), "answered": 2, "mean": "4.5000", "median": "4.5000", "masked_fields": []any{}, "authorization_version": "native-current-c7", "predicate_hash": insightsPredicate}
}
func insightsMetricsFixture() map[string]any {
	metrics := map[string]any{}
	for _, name := range []string{"frt", "nrt", "mttr", "backlog", "sla_compliance"} {
		metrics[name] = map[string]any{"eligible_count": 3, "sample_count": 2, "contributor_count": 2, "mean_microseconds": "9007199254740993.123456", "median_microseconds": "1200.000001", "value_count": nil, "compliant_count": nil, "missing_count": 0, "masked": false}
	}
	// Count metrics have no duration mean/median in the native calculator.
	metrics["backlog"] = map[string]any{"eligible_count": 3, "sample_count": 3, "contributor_count": 2, "mean_microseconds": nil, "median_microseconds": nil, "value_count": 2, "compliant_count": nil, "missing_count": 0, "masked": false}
	metrics["sla_compliance"] = map[string]any{"eligible_count": 3, "sample_count": 2, "contributor_count": 2, "mean_microseconds": nil, "median_microseconds": nil, "value_count": nil, "compliant_count": 1, "missing_count": 0, "masked": false}
	return map[string]any{"selection": map[string]any{"start": insightsStart, "end": insightsEnd, "clock_mode": "natural", "agent_id": nil, "team_id": nil, "status": nil}, "cohort_basis": "ticket_created_at", "cycle_basis": "current_cycle", "as_of": insightsAsOf, "authorization_version": "native-current-c7", "predicate_hash": insightsPredicate, "cohort_count": 3, "metrics": metrics}
}
func insightsDrillFixture() map[string]any {
	return map[string]any{"metric": "frt", "as_of": insightsAsOf, "authorization_version": "native-current-c7", "predicate_hash": insightsPredicate, "total": 1, "page": 1, "per_page": 25, "items": []any{map[string]any{"id": crmTestID, "version": 7, "cycle": 2, "label": nil, "status": nil, "sample_count": 1}}}
}
func insightsArgs(action string) []string {
	args := []string{"crm", "service-insights", action, "--start", insightsStart, "--end", insightsEnd, "--json"}
	if action != "csat" {
		args = append(args, "--clock_mode", "natural")
	}
	if action == "drill" {
		args = append(args, "--as_of", insightsAsOf, "--predicate_hash", insightsPredicate, "--metric", "frt")
	}
	return args
}

func TestServiceInsightsThreeNativeGets(t *testing.T) {
	tests := []struct {
		action, path string
		data         map[string]any
	}{{"csat", "/v1/crm/service-insights", insightsCsatFixture()}, {"metrics", "/v1/crm/service-insights/service-metrics", insightsMetricsFixture()}, {"drill", "/v1/crm/service-insights/drill", insightsDrillFixture()}}
	for _, test := range tests {
		t.Run(test.action, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "service_insights:read", "customer_service:read")
					return
				}
				calls.Add(1)
				body, _ := io.ReadAll(r.Body)
				if r.Method != "GET" || r.URL.Path != test.path || len(body) != 0 || r.Header.Get("Authorization") == "" {
					t.Errorf("native read changed: %s %s", r.Method, r.URL)
				}
				if _, present := r.Header["Idempotency-Key"]; present {
					t.Error("GET invented original header")
				}
				q := r.URL.Query()
				if q.Get("start") != insightsStart || q.Get("end") != insightsEnd {
					t.Error("UTC6 cohort changed")
				}
				if _, present := q["page"]; present {
					t.Error("invented pagination default")
				}
				if _, present := q["per_page"]; present {
					t.Error("invented page size")
				}
				if _, present := q["cursor"]; present {
					t.Error("cursor invented")
				}
				if test.action == "drill" && (q.Get("as_of") != insightsAsOf || q.Get("predicate_hash") != insightsPredicate || q.Get("metric") != "frt") {
					t.Error("original snapshot changed")
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, nativeJSONEnvelope(test.data))
			}))
			defer server.Close()
			out, stderr, err := crmRun(t, server, insightsArgs(test.action)...)
			if err != nil || calls.Load() != 1 {
				t.Fatalf("actual read failed %d %v %s", calls.Load(), err, stderr)
			}
			if test.action == "csat" && (!strings.Contains(out, "9007199254740993") || strings.Contains(out, `"as_of"`)) {
				t.Fatal("exact CSAT count/as_of contract changed")
			}
			if test.action == "metrics" && !strings.Contains(out, `"9007199254740993.123456"`) {
				t.Fatal("duration decimal converted to Float")
			}
			if test.action == "drill" && (!strings.Contains(out, `"label":null`) || !strings.Contains(out, `"status":null`)) {
				t.Fatal("masked fields defaulted")
			}
		})
	}
}

func TestServiceInsightsDiscoveryPreservesPrevious108(t *testing.T) {
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
	count := 0
	for _, e := range entries {
		if _, exists := byID[e.OperationID]; exists {
			t.Fatalf("duplicate %s", e.OperationID)
		}
		byID[e.OperationID] = e
		if strings.HasPrefix(e.Command, "factuarea crm service-insights ") {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("native Insights count %d", count)
	}
	for _, op := range insightsOperations() {
		e := byID[op.ID]
		if e.NativePath != op.Path || e.CrmOperation != op.Operation || e.Method != "GET" || e.AuthRealm != "staff" || e.OperationIDSource != "native_v1_surface" || len(e.RequiredScopes) != 2 || e.RequiredScopes[0] != "service_insights:read" || e.RequiredScopes[1] != "customer_service:read" || e.Mutating || e.IdempotencyRequired || e.LocalConfirmation || e.RequiresConfirmation || e.OriginalRecovery || e.RequestSchema != nil || e.ResponseSchemas["200"] == nil {
			t.Fatalf("native manifest contract lost %+v", e)
		}
		if _, invented := e.QuerySchemas["cursor"]; invented {
			t.Fatal("invented pagination cursor")
		}
	}
	previous := append(nativeOwnerManifest(), taskTimeManifest()...)
	if len(previous) != 31 {
		t.Fatal("previous owner31 changed")
	}
	for _, entry := range previous {
		a, _ := json.Marshal(entry)
		b, _ := json.Marshal(byID[entry.OperationID])
		if !bytes.Equal(a, b) {
			t.Fatalf("frozen previous schema changed %s", entry.OperationID)
		}
	}
	old := 0
	for _, op := range generatedOps() {
		if op.CrmOperation != "" {
			old++
		}
		e := byID[op.OperationID]
		if e.Command != commandPath(op) || e.RequiredScope != op.RequiredScope || e.CrmOperation != op.CrmOperation {
			t.Fatal("native77 changed")
		}
	}
	if old != 77 {
		t.Fatalf("old native cohort %d", old)
	}
}

func TestServiceInsightsRequiresBothNativeScopes(t *testing.T) {
	for _, scope := range []string{"service_insights:read", "customer_service:read"} {
		t.Run(scope, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, scope)
					return
				}
				calls.Add(1)
			}))
			defer server.Close()
			_, _, err := crmRun(t, server, insightsArgs("csat")...)
			if err == nil || exit.ForError(err) != exit.Perm || calls.Load() != 0 {
				t.Fatalf("one scope became authority %d %v", calls.Load(), err)
			}
		})
	}
}

func TestServiceInsightsClosedQueriesRejectBeforeNetwork(t *testing.T) {
	tests := []struct {
		name, action string
		extra        []string
	}{
		{"UTC_millisecond_precision", "csat", []string{"--start", "2026-01-01T00:00:00.123Z"}},
		{"UTC_invalid_day", "csat", []string{"--start", "2026-02-30T00:00:00.123456Z"}},
		{"UTC_offset_alias", "csat", []string{"--start", "2026-01-01T00:00:00.123456+00:00"}},
		{"reversed_cohort", "csat", []string{"--end", insightsStart}},
		{"too_long_cohort", "csat", []string{"--end", "2027-01-03T00:00:00.123456Z"}},
		{"CSAT_agent_forbidden", "csat", []string{"--agent_id", crmTestID}},
		{"SPA_alias_forbidden", "metrics", []string{"--agent_uuid", crmTestID}},
		{"null_filter_forbidden", "metrics", []string{"--team_id", "null"}},
		{"empty_filter_forbidden", "metrics", []string{"--status", ""}},
		{"team_historical_forbidden", "metrics", []string{"--team_id", insightsHistoricalAgent}},
		{"clock_mode_default_forbidden", "metrics", []string{"--clock_mode", "calendar"}},
		{"unknown_status", "metrics", []string{"--status", "resolved"}},
		{"cursor_forbidden", "drill", []string{"--cursor", "opaque"}},
		{"page_leading_zero", "drill", []string{"--page", "01"}},
		{"page_plus_alias", "drill", []string{"--page", "+1"}},
		{"page_over_native_max", "drill", []string{"--page", "10000000"}},
		{"size_over_native_max", "drill", []string{"--per_page", "101"}},
		{"predicate_invalid", "drill", []string{"--predicate_hash", strings.Repeat("B", 64)}},
		{"metric_invalid", "drill", []string{"--metric", "csat"}},
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := append(insightsArgs(test.action), test.extra...)
			_, _, err := crmRun(t, server, args...)
			if err == nil {
				t.Fatal("invalid query accepted")
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid query contacted native server %d", calls.Load())
	}
}

func TestServiceInsightsCurrentSelectionAndExplicitNumberPage(t *testing.T) {
	for _, action := range []string{"metrics", "drill"} {
		t.Run(action, func(t *testing.T) {
			data := insightsMetricsFixture()
			if action == "metrics" {
				selection := data["selection"].(map[string]any)
				selection["agent_id"] = insightsHistoricalAgent
				selection["team_id"] = crmTestOtherID
				selection["status"] = "closed"
			} else {
				data = insightsDrillFixture()
				data["page"], data["per_page"] = 2, 17
				data["total"] = 18
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, insightsScopes...)
					return
				}
				q := r.URL.Query()
				if q.Get("agent_id") != insightsHistoricalAgent || q.Get("team_id") != crmTestOtherID || q.Get("status") != "closed" {
					t.Error("public filters lost/normalized")
				}
				if action == "drill" && (q.Get("page") != "2" || q.Get("per_page") != "17") {
					t.Error("numeric page changed")
				}
				io.WriteString(w, nativeJSONEnvelope(data))
			}))
			defer server.Close()
			args := append(insightsArgs(action), "--agent_id", insightsHistoricalAgent, "--team_id", crmTestOtherID, "--status", "closed")
			if action == "drill" {
				args = append(args, "--page", "2", "--per_page", "17")
			}
			_, stderr, err := crmRun(t, server, args...)
			if err != nil {
				t.Fatalf("selection failed %v %s", err, stderr)
			}
		})
	}
}

func TestServiceInsightsMaskedNativeValuesStayNull(t *testing.T) {
	for _, action := range []string{"csat", "metrics"} {
		t.Run(action, func(t *testing.T) {
			data := insightsCsatFixture()
			if action == "csat" {
				data["masked_fields"] = []any{"answered_at", "rating"}
				data["answered"], data["mean"], data["median"] = nil, nil, nil
			} else {
				data = insightsMetricsFixture()
				row := data["metrics"].(map[string]any)["frt"].(map[string]any)
				row["masked"] = true
				for _, name := range []string{"sample_count", "contributor_count", "mean_microseconds", "median_microseconds", "value_count", "compliant_count", "missing_count"} {
					row[name] = nil
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, insightsScopes...)
					return
				}
				io.WriteString(w, nativeJSONEnvelope(data))
			}))
			defer server.Close()
			out, stderr, err := crmRun(t, server, insightsArgs(action)...)
			if err != nil {
				t.Fatalf("actual masked data rejected %v %s", err, stderr)
			}
			if action == "csat" && (!strings.Contains(out, `"answered":null`) || !strings.Contains(out, `"mean":null`)) {
				t.Fatal("CSAT null fabricated zero")
			}
			if action == "metrics" && !strings.Contains(out, `"mean_microseconds":null`) {
				t.Fatal("masked duration fabricated zero")
			}
		})
	}
}

func TestServiceInsightsRejectsUnqualifiedOrPrivateResponses(t *testing.T) {
	cases := []struct {
		name, action string
		data         map[string]any
	}{
		{"different_cohort", "csat", insightsCsatFixture()},
		{"masked_answer_leak", "csat", insightsCsatFixture()},
		{"masked_rating_leak", "csat", insightsCsatFixture()},
		{"float_decimal", "csat", insightsCsatFixture()},
		{"wrong_scale", "csat", insightsCsatFixture()},
		{"CSAT_as_of_forbidden", "csat", insightsCsatFixture()},
		{"selection_changed", "metrics", insightsMetricsFixture()},
		{"masked_metric_leak", "metrics", insightsMetricsFixture()},
		{"timestamp_truncated", "metrics", insightsMetricsFixture()},
		{"original_as_of_changed", "drill", insightsDrillFixture()},
		{"original_predicate_changed", "drill", insightsDrillFixture()},
		{"original_metric_changed", "drill", insightsDrillFixture()},
		{"page_changed", "drill", insightsDrillFixture()},
		{"duplicate_contributor", "drill", insightsDrillFixture()},
		{"private_body", "drill", insightsDrillFixture()},
		{"SPA_id_alias", "drill", insightsDrillFixture()},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			switch test.name {
			case "different_cohort":
				test.data["cohort_end"] = insightsAsOf
			case "masked_answer_leak":
				test.data["masked_fields"] = []any{"answered_at"}
				test.data["mean"], test.data["median"] = nil, nil
			case "masked_rating_leak":
				test.data["masked_fields"] = []any{"rating"}
			case "float_decimal":
				test.data["mean"] = 4.5
			case "wrong_scale":
				test.data["mean"] = "4.50"
			case "CSAT_as_of_forbidden":
				test.data["as_of"] = insightsAsOf
			case "selection_changed":
				test.data["selection"].(map[string]any)["clock_mode"] = "business"
			case "masked_metric_leak":
				test.data["metrics"].(map[string]any)["frt"].(map[string]any)["masked"] = true
			case "timestamp_truncated":
				test.data["as_of"] = "2026-01-02T12:00:00.654Z"
			case "original_as_of_changed":
				test.data["as_of"] = "2026-01-02T12:00:00.654322Z"
			case "original_predicate_changed":
				test.data["predicate_hash"] = strings.Repeat("a", 64)
			case "original_metric_changed":
				test.data["metric"] = "nrt"
			case "page_changed":
				test.data["page"] = 2
			case "duplicate_contributor":
				test.data["items"] = append(test.data["items"].([]any), test.data["items"].([]any)[0])
				test.data["total"] = 2
			case "private_body":
				test.data["items"].([]any)[0].(map[string]any)["body"] = "private original ticket"
			case "SPA_id_alias":
				row := test.data["items"].([]any)[0].(map[string]any)
				row["uuid"] = row["id"]
				delete(row, "id")
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, insightsScopes...)
					return
				}
				calls.Add(1)
				io.WriteString(w, nativeJSONEnvelope(test.data))
			}))
			defer server.Close()
			out, stderr, err := crmRun(t, server, insightsArgs(test.action)...)
			if err == nil || calls.Load() != 1 || out != "" || !strings.Contains(stderr, "invalid_native_response") {
				t.Fatalf("unqualified/private response accepted %d %v %s", calls.Load(), err, out)
			}
		})
	}
}

func TestServiceInsightsNativeUnavailableDeniedAndNoRootMountClaim(t *testing.T) {
	for _, status := range []int{403, 404, 409, 422, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, insightsScopes...)
					return
				}
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				io.WriteString(w, `{"error":{"type":"api_error","code":"service_insights_native_denial","message":"Native current producer unavailable or denied","subcode":"current_scope","request_id":"insights-native-request","param":"predicate_hash"}}`)
			}))
			defer server.Close()
			out, stderr, err := crmRun(t, server, insightsArgs("drill")...)
			var native *apierr.APIError
			if err == nil || !errors.As(err, &native) || native.StatusCode != status || calls.Load() != 1 || out != "" || !strings.Contains(stderr, "service_insights_native_denial") || !strings.Contains(stderr, "insights-native-request") || !strings.Contains(stderr, "predicate_hash") {
				t.Fatalf("native denial masked/fallback %d %v %s", calls.Load(), err, stderr)
			}
		})
	}
}

func TestServiceInsightsMissingRequiredSelectionAndNativeMicrosecondBoundary(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, args := range [][]string{{"crm", "service-insights", "csat", "--start", insightsStart}, {"crm", "service-insights", "metrics", "--start", insightsStart, "--end", insightsEnd}, {"crm", "service-insights", "drill", "--start", insightsStart, "--end", insightsEnd, "--clock_mode", "natural"}} {
		_, _, err := crmRun(t, server, args...)
		if err == nil || exit.ForError(err) != exit.Usage {
			t.Fatalf("missing native predicate accepted %v", err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("incomplete query contacted server")
	}
	if validateInsightsCohort("2026-01-01T00:00:00.000001Z", "2026-01-01T00:00:00.000002Z") != nil {
		t.Fatal("one microsecond cohort truncated")
	}
	if validateInsightsCohort("2026-01-01T00:00:00.000001Z", "2027-01-02T00:00:00.000001Z") != nil {
		t.Fatal("native inclusive366day maximum changed")
	}
	if validateInsightsCohort("2026-01-01T00:00:00.000001Z", "2027-01-02T00:00:00.000002Z") == nil {
		t.Fatal("one microsecond beyond native maximum accepted")
	}
}
