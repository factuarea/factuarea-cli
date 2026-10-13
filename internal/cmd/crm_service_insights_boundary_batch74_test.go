package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/exit"
)

func insightsBoundaryEmptyMetrics() map[string]any {
	data := insightsMetricsFixture()
	data["cohort_count"] = 0
	for name, raw := range data["metrics"].(map[string]any) {
		metric := raw.(map[string]any)
		metric["eligible_count"], metric["sample_count"], metric["contributor_count"] = 0, 0, 0
		metric["mean_microseconds"], metric["median_microseconds"] = nil, nil
		if name == "backlog" {
			metric["value_count"] = 0
		}
		if name == "sla_compliance" {
			metric["compliant_count"] = 0
		}
	}
	return data
}

func insightsBoundaryRun(t *testing.T, action string, data map[string]any, accepted bool) {
	t.Helper()
	var reads atomic.Int32
	wire := nativeJSONEnvelope(data)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, insightsScopes...)
			return
		}
		reads.Add(1)
		if r.Method != "GET" || r.Header.Get("Idempotency-Key") != "" {
			t.Error("native Insights read acquired effect semantics")
		}
		_, _ = io.WriteString(w, wire)
	}))
	defer s.Close()
	out, stderr, err := crmRun(t, s, insightsArgs(action)...)
	if reads.Load() != 1 {
		t.Fatalf("native read retried or fell back: %d", reads.Load())
	}
	if accepted {
		if err != nil || strings.TrimSpace(out) != wire || stderr != "" {
			t.Fatalf("native denominator/null projection changed: %q %q %v", out, stderr, err)
		}
		return
	}
	var native *apierr.APIError
	if out != "" || !errors.As(err, &native) || native.Code != "invalid_native_response" || native.StatusCode != 502 || exit.ForError(err) != exit.Server {
		t.Fatalf("invalid metric escaped: out=%q stderr=%q err=%v", out, stderr, err)
	}
}

func TestServiceInsightsBoundaryCSATPreservesZeroAndIndependentMasks(t *testing.T) {
	for _, state := range []string{"zero invited", "zero answers", "answered hidden", "rating hidden", "both hidden", "score minimum", "score maximum"} {
		t.Run(state, func(t *testing.T) {
			data := insightsCsatFixture()
			switch state {
			case "zero invited":
				data["invited"], data["answered"], data["mean"], data["median"] = 0, 0, nil, nil
			case "zero answers":
				data["answered"], data["mean"], data["median"] = 0, nil, nil
			case "answered hidden":
				data["masked_fields"], data["answered"], data["mean"], data["median"] = []any{"answered_at"}, nil, nil, nil
			case "rating hidden":
				data["masked_fields"], data["mean"], data["median"] = []any{"rating"}, nil, nil
			case "both hidden":
				data["masked_fields"], data["answered"], data["mean"], data["median"] = []any{"answered_at", "rating"}, nil, nil, nil
			case "score minimum":
				data["mean"], data["median"] = "1.0000", "1.0000"
			case "score maximum":
				data["mean"], data["median"] = "5.0000", "5.0000"
			}
			insightsBoundaryRun(t, "csat", data, true)
		})
	}
}

func TestServiceInsightsBoundaryCSATRejectsInventedMeansAndDenominators(t *testing.T) {
	for _, state := range []string{"zero answers mean", "zero answers median", "answers exceed invited", "visible answered null", "rating mask hides answered", "missing mean", "missing median", "rating below scale", "rating above scale", "oversized decimal", "mask duplicates"} {
		t.Run(state, func(t *testing.T) {
			data := insightsCsatFixture()
			switch state {
			case "zero answers mean":
				data["answered"], data["mean"], data["median"] = 0, "0.0000", nil
			case "zero answers median":
				data["answered"], data["mean"], data["median"] = 0, nil, "0.0000"
			case "answers exceed invited":
				data["invited"] = 1
			case "visible answered null":
				data["answered"], data["mean"], data["median"] = nil, nil, nil
			case "rating mask hides answered":
				data["masked_fields"], data["answered"], data["mean"], data["median"] = []any{"rating"}, nil, nil, nil
			case "missing mean":
				data["mean"] = nil
			case "missing median":
				data["median"] = nil
			case "rating below scale":
				data["mean"] = "0.9999"
			case "rating above scale":
				data["median"] = "5.0001"
			case "oversized decimal":
				data["mean"] = "9007199254740993.0000"
			case "mask duplicates":
				data["masked_fields"], data["mean"], data["median"] = []any{"rating", "rating"}, nil, nil
			}
			insightsBoundaryRun(t, "csat", data, false)
		})
	}
}

func TestServiceInsightsBoundaryMetricsKeepNativeZeroSamplesAndRepeatedIntervals(t *testing.T) {
	for _, state := range []string{"empty cohort", "no completed duration", "multiple NRT intervals", "multiple SLA evaluations", "measured zero", "missing backlog inputs", "masked zero cohort"} {
		t.Run(state, func(t *testing.T) {
			data := insightsMetricsFixture()
			metrics := data["metrics"].(map[string]any)
			switch state {
			case "empty cohort":
				data = insightsBoundaryEmptyMetrics()
			case "no completed duration":
				metric := metrics["frt"].(map[string]any)
				metric["sample_count"], metric["contributor_count"], metric["mean_microseconds"], metric["median_microseconds"] = 0, 0, nil, nil
			case "multiple NRT intervals":
				metric := metrics["nrt"].(map[string]any)
				metric["sample_count"], metric["contributor_count"] = 7, 2
			case "multiple SLA evaluations":
				metric := metrics["sla_compliance"].(map[string]any)
				metric["sample_count"], metric["compliant_count"] = 7, 6
			case "measured zero":
				metric := metrics["mttr"].(map[string]any)
				metric["mean_microseconds"], metric["median_microseconds"] = "0.000000", "0.000000"
			case "missing backlog inputs":
				metric := metrics["backlog"].(map[string]any)
				metric["sample_count"], metric["contributor_count"], metric["value_count"], metric["missing_count"] = 1, 1, 1, 2
			case "masked zero cohort":
				data = insightsBoundaryEmptyMetrics()
				metric := data["metrics"].(map[string]any)["frt"].(map[string]any)
				metric["masked"] = true
				for _, field := range []string{"sample_count", "contributor_count", "missing_count"} {
					metric[field] = nil
				}
			}
			insightsBoundaryRun(t, "metrics", data, true)
		})
	}
}

func TestServiceInsightsBoundaryMetricsRejectImpossibleDenominators(t *testing.T) {
	for _, state := range []string{"zero sample mean", "zero sample median", "eligible differs cohort", "unmasked samples null", "unmasked contributors null", "unmasked missing null", "contributors exceed eligible", "missing exceeds eligible", "contributors plus missing exceed eligible", "contributors exceed samples", "samples without contributors", "FRT multiple samples", "MTTR multiple samples", "duration missing mean", "duration missing median", "duration above int64", "duration has value count", "duration has compliant count", "backlog incorrect sample denominator", "backlog incorrect value count", "backlog has duration", "SLA compliant exceeds samples", "SLA compliant missing", "SLA has duration", "masked missing leak"} {
		t.Run(state, func(t *testing.T) {
			data := insightsMetricsFixture()
			metrics := data["metrics"].(map[string]any)
			metric := metrics["frt"].(map[string]any)
			switch state {
			case "zero sample mean":
				metric["sample_count"], metric["contributor_count"], metric["median_microseconds"] = 0, 0, nil
			case "zero sample median":
				metric["sample_count"], metric["contributor_count"], metric["mean_microseconds"] = 0, 0, nil
			case "eligible differs cohort":
				metric["eligible_count"] = 4
			case "unmasked samples null":
				metric["sample_count"] = nil
			case "unmasked contributors null":
				metric["contributor_count"] = nil
			case "unmasked missing null":
				metric["missing_count"] = nil
			case "contributors exceed eligible":
				metric["contributor_count"], metric["sample_count"] = 4, 4
			case "missing exceeds eligible":
				metric["missing_count"] = 4
			case "contributors plus missing exceed eligible":
				metric["missing_count"] = 2
			case "contributors exceed samples":
				metric["sample_count"] = 1
			case "samples without contributors":
				metric["contributor_count"] = 0
			case "FRT multiple samples":
				metric["sample_count"] = 3
			case "MTTR multiple samples":
				metrics["mttr"].(map[string]any)["sample_count"] = 3
			case "duration missing mean":
				metric["mean_microseconds"] = nil
			case "duration missing median":
				metric["median_microseconds"] = nil
			case "duration above int64":
				metric["mean_microseconds"] = "9223372036854775807.000001"
			case "duration has value count":
				metric["value_count"] = 0
			case "duration has compliant count":
				metric["compliant_count"] = 0
			case "backlog incorrect sample denominator":
				metrics["backlog"].(map[string]any)["sample_count"] = 2
			case "backlog incorrect value count":
				metrics["backlog"].(map[string]any)["value_count"] = 1
			case "backlog has duration":
				metrics["backlog"].(map[string]any)["mean_microseconds"] = "1.000000"
			case "SLA compliant exceeds samples":
				metrics["sla_compliance"].(map[string]any)["compliant_count"] = 3
			case "SLA compliant missing":
				metrics["sla_compliance"].(map[string]any)["compliant_count"] = nil
			case "SLA has duration":
				metrics["sla_compliance"].(map[string]any)["median_microseconds"] = "0.000000"
			case "masked missing leak":
				metric["masked"] = true
				for _, field := range []string{"sample_count", "contributor_count", "mean_microseconds", "median_microseconds", "value_count", "compliant_count"} {
					metric[field] = nil
				}
			}
			insightsBoundaryRun(t, "metrics", data, false)
		})
	}
}

func TestServiceInsightsBoundaryDrillNativePageAndSampleCounts(t *testing.T) {
	for _, state := range []string{"zero contributors", "NRT repeated intervals", "SLA repeated evaluations", "missing page row", "scalar repeated sample"} {
		t.Run(state, func(t *testing.T) {
			data := insightsDrillFixture()
			accepted := true
			switch state {
			case "zero contributors":
				data["total"], data["items"] = 0, []any{}
			case "NRT repeated intervals":
				data["metric"] = "nrt"
				data["items"].([]any)[0].(map[string]any)["sample_count"] = 7
			case "SLA repeated evaluations":
				data["metric"] = "sla_compliance"
				data["items"].([]any)[0].(map[string]any)["sample_count"] = 7
			case "missing page row":
				data["items"] = []any{}
				accepted = false
			case "scalar repeated sample":
				data["items"].([]any)[0].(map[string]any)["sample_count"] = 2
				accepted = false
			}
			if data["metric"] == "frt" {
				insightsBoundaryRun(t, "drill", data, accepted)
				return
			}
			var op insightsOp
			for _, candidate := range insightsOperations() {
				if candidate.Action == "drill" {
					op = candidate
				}
			}
			input := map[string]any{"as_of": insightsAsOf, "predicate_hash": insightsPredicate, "metric": data["metric"]}
			err := validateInsightsResponse(op, &client.Response{StatusCode: 200, Body: []byte(nativeJSONEnvelope(data))}, input)
			if (err == nil) != accepted {
				t.Fatalf("native repeated interval rejected: %v", err)
			}
		})
	}
	for _, tc := range []struct {
		name                    string
		total, page, size, rows int
		valid                   bool
	}{
		{"full first page", 25, 1, 25, 25, true}, {"full later page", 51, 2, 25, 25, true},
		{"last partial page", 51, 3, 25, 1, true}, {"after final page", 51, 4, 25, 0, true},
		{"missing first page rows", 25, 1, 25, 24, false}, {"extra last page rows", 51, 3, 25, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := insightsDrillFixture()
			items := []any{}
			for i := 0; i < tc.rows; i++ {
				items = append(items, map[string]any{"sample_count": json.Number("1")})
			}
			data["total"], data["page"], data["per_page"], data["items"] = json.Number(strconv.Itoa(tc.total)), json.Number(strconv.Itoa(tc.page)), json.Number(strconv.Itoa(tc.size)), items
			if validInsightsDrillBoundary(data) != tc.valid {
				t.Fatal("native page differs from array_slice contract")
			}
		})
	}
}

func TestServiceInsightsBoundaryRejectsMoneyAndExportFlagsOffline(t *testing.T) {
	for _, flag := range []string{"currency", "money", "export", "cursor"} {
		t.Run(flag, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
			defer s.Close()
			out, _, err := crmRun(t, s, append(insightsArgs("metrics"), "--"+flag, "EUR")...)
			if out != "" || calls.Load() != 0 || exit.ForError(err) != exit.Usage {
				t.Fatalf("unavailable native concept became a query: %v", err)
			}
		})
	}
	data := insightsMetricsFixture()
	data["currency"] = "EUR"
	insightsBoundaryRun(t, "metrics", data, false)
}

func TestServiceInsightsBoundaryDiscoveryKeepsPrevious598AndRecoveryExact(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"commands", "--json"})
	if root.Execute() != nil {
		t.Fatal("offline discovery failed")
	}
	var entries []manifestEntry
	if json.Unmarshal(out.Bytes(), &entries) != nil || len(entries) != 600 {
		t.Fatal("unexpected command addition/removal")
	}
	byID := map[string]manifestEntry{}
	for _, entry := range entries {
		byID[entry.OperationID] = entry
	}
	for _, expected := range append(insightsManifest(), mailboxWriteOriginalManifest()...) {
		actualJSON, _ := json.Marshal(byID[expected.OperationID])
		expectedJSON, _ := json.Marshal(expected)
		if !bytes.Equal(actualJSON, expectedJSON) {
			t.Fatalf("current recovery or Insights metadata changed: %s", expected.OperationID)
		}
	}
}

func TestServiceInsightsBoundaryMissingResponseIsTypedFailure(t *testing.T) {
	op := insightsOperations()[0]
	err := validateInsightsResponse(op, nil, map[string]any{"start": insightsStart, "end": insightsEnd})
	var native *apierr.APIError
	if !errors.As(err, &native) || native.Code != "invalid_native_response" || exit.ForError(err) != exit.Server {
		t.Fatalf("missing producer response became a panic or success: %v", err)
	}
}
