package cmd

import "math/big"

// These checks compare the received projection with the native aggregators'
// denominator and null semantics. They do not calculate or unmask any metric.
func validInsightsCsatBoundary(data map[string]any) bool {
	invited := nativeInteger(data["invited"])
	masks := map[string]bool{}
	for _, mask := range data["masked_fields"].([]any) {
		masks[mask.(string)] = true
	}
	if masks["answered_at"] {
		return data["answered"] == nil && data["mean"] == nil && data["median"] == nil
	}
	answered, present := knowledgeInt(data["answered"])
	if !present || answered > invited {
		return false
	}
	if masks["rating"] || answered == 0 {
		return data["mean"] == nil && data["median"] == nil
	}
	return validInsightsDecimalRange(data["mean"], 1, 5) && validInsightsDecimalRange(data["median"], 1, 5)
}

func validInsightsMetricBoundary(name string, metric map[string]any, cohort int64) bool {
	if nativeInteger(metric["eligible_count"]) != cohort {
		return false
	}
	if metric["masked"] == true {
		for _, field := range []string{"sample_count", "contributor_count", "mean_microseconds", "median_microseconds", "value_count", "compliant_count", "missing_count"} {
			if metric[field] != nil {
				return false
			}
		}
		return true
	}
	samples, hasSamples := knowledgeInt(metric["sample_count"])
	contributors, hasContributors := knowledgeInt(metric["contributor_count"])
	missing, hasMissing := knowledgeInt(metric["missing_count"])
	if !hasSamples || !hasContributors || !hasMissing || missing > cohort || contributors > cohort-missing || contributors > samples {
		return false
	}
	if name == "backlog" {
		value, hasValue := knowledgeInt(metric["value_count"])
		return hasValue && samples == cohort-missing && value == contributors && metric["compliant_count"] == nil && metric["mean_microseconds"] == nil && metric["median_microseconds"] == nil
	}
	if (samples == 0) != (contributors == 0) || metric["value_count"] != nil {
		return false
	}
	if name == "sla_compliance" {
		compliant, hasCompliant := knowledgeInt(metric["compliant_count"])
		return hasCompliant && compliant <= samples && metric["mean_microseconds"] == nil && metric["median_microseconds"] == nil
	}
	if metric["compliant_count"] != nil || ((name == "frt" || name == "mttr") && samples != contributors) {
		return false
	}
	if samples == 0 {
		return metric["mean_microseconds"] == nil && metric["median_microseconds"] == nil
	}
	return validInsightsDecimalRange(metric["mean_microseconds"], 0, 9223372036854775807) && validInsightsDecimalRange(metric["median_microseconds"], 0, 9223372036854775807)
}

func validInsightsDecimalRange(value any, minimum, maximum int64) bool {
	text, present := value.(string)
	if !present {
		return false
	}
	number, valid := new(big.Rat).SetString(text)
	return valid && number.Cmp(big.NewRat(minimum, 1)) >= 0 && number.Cmp(big.NewRat(maximum, 1)) <= 0
}

func validInsightsDrillBoundary(data map[string]any) bool {
	items := data["items"].([]any)
	total, size, page := nativeInteger(data["total"]), nativeInteger(data["per_page"]), nativeInteger(data["page"])
	// Native page/size limits keep the offset within int64, even when the
	// authorized contributor total uses the full native integer range.
	remaining := total - (page-1)*size
	expected := remaining
	if expected < 0 {
		expected = 0
	}
	if expected > size {
		expected = size
	}
	if int64(len(items)) != expected {
		return false
	}
	metric := data["metric"].(string)
	for _, item := range items {
		row := item.(map[string]any)
		if (metric == "frt" || metric == "mttr" || metric == "backlog") && nativeInteger(row["sample_count"]) != 1 {
			return false
		}
	}
	return true
}
