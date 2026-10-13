package cmd

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
)

const insightsInstantLayout = "2006-01-02T15:04:05.000000Z"

var insightsScopes = []string{"service_insights:read", "customer_service:read"}

type insightsOp struct {
	Action, ID, Operation, Path string
	Query                       []nativeQuery
	Response                    nativeSchema
}

func insightsMetric() nativeSchema { return nEnum("frt", "nrt", "mttr", "backlog", "sla_compliance") }
func insightsStatus() nativeSchema {
	return nEnum("new", "assigned", "pending", "closed", "rejected", "duplicate")
}
func insightsUserID() nativeSchema {
	return nativeSchema{"type": "string", "pattern": `(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`}
}
func insightsInstant() nativeSchema {
	return nativeSchema{"type": "string", "pattern": `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$`}
}
func insightsHash() nativeSchema { return nativeSchema{"type": "string", "pattern": `^[a-f0-9]{64}$`} }
func insightsInteger(minimum int) nativeSchema {
	return nativeSchema{"type": "integer", "minimum": minimum, "maximum": int64(9223372036854775807)}
}
func insightsDecimal(scale string) nativeSchema {
	return nNullable(nativeSchema{"type": "string", "pattern": `^[0-9]+\.[0-9]{` + scale + `}$`})
}

func insightsSelection() nativeSchema {
	return nObject(nativeSchema{"start": insightsInstant(), "end": insightsInstant(), "clock_mode": nEnum("natural", "business"), "agent_id": nNullable(insightsUserID()), "team_id": nNullable(nUUID()), "status": nNullable(insightsStatus())}, "start", "end", "clock_mode", "agent_id", "team_id", "status")
}

func insightsCsatSchema() nativeSchema {
	masks := nArray(nEnum("answered_at", "rating"))
	masks["uniqueItems"] = true
	return nEnvelope(nObject(nativeSchema{"metric": nativeSchema{"type": "string", "const": "csat"}, "metric_version": nativeSchema{"type": "integer", "const": 1}, "cohort_start": insightsInstant(), "cohort_end": insightsInstant(), "cohort_basis": nativeSchema{"type": "string", "const": "original_closed_at"}, "cycle_policy": nativeSchema{"type": "string", "const": "each_closure_participant"}, "clock_mode": nativeSchema{"type": "string", "const": "not_applicable"}, "invited": insightsInteger(0), "answered": nNullable(insightsInteger(0)), "mean": insightsDecimal("4"), "median": insightsDecimal("4"), "masked_fields": masks, "authorization_version": nText(1, 65536), "predicate_hash": insightsHash()}, "metric", "metric_version", "cohort_start", "cohort_end", "cohort_basis", "cycle_policy", "clock_mode", "invited", "answered", "mean", "median", "masked_fields", "authorization_version", "predicate_hash"))
}

func insightsMetricsSchema() nativeSchema {
	metric := nObject(nativeSchema{"eligible_count": insightsInteger(0), "sample_count": nNullable(insightsInteger(0)), "contributor_count": nNullable(insightsInteger(0)), "mean_microseconds": insightsDecimal("6"), "median_microseconds": insightsDecimal("6"), "value_count": nNullable(insightsInteger(0)), "compliant_count": nNullable(insightsInteger(0)), "missing_count": nNullable(insightsInteger(0)), "masked": nativeSchema{"type": "boolean"}}, "eligible_count", "sample_count", "contributor_count", "mean_microseconds", "median_microseconds", "value_count", "compliant_count", "missing_count", "masked")
	metrics := nObject(nativeSchema{"frt": metric, "nrt": metric, "mttr": metric, "backlog": metric, "sla_compliance": metric}, "frt", "nrt", "mttr", "backlog", "sla_compliance")
	return nEnvelope(nObject(nativeSchema{"selection": insightsSelection(), "cohort_basis": nativeSchema{"type": "string", "const": "ticket_created_at"}, "cycle_basis": nativeSchema{"type": "string", "const": "current_cycle"}, "as_of": insightsInstant(), "authorization_version": nText(1, 65536), "predicate_hash": insightsHash(), "cohort_count": insightsInteger(0), "metrics": metrics}, "selection", "cohort_basis", "cycle_basis", "as_of", "authorization_version", "predicate_hash", "cohort_count", "metrics"))
}

func insightsDrillSchema() nativeSchema {
	row := nObject(nativeSchema{"id": nUUID(), "version": insightsInteger(1), "cycle": insightsInteger(1), "label": nNullable(nativeSchema{"type": "string"}), "status": nNullable(insightsStatus()), "sample_count": insightsInteger(1)}, "id", "version", "cycle", "label", "status", "sample_count")
	return nEnvelope(nObject(nativeSchema{"metric": insightsMetric(), "as_of": insightsInstant(), "authorization_version": nText(1, 65536), "predicate_hash": insightsHash(), "total": insightsInteger(0), "page": nRange(1, 9999999), "per_page": nRange(1, 100), "items": nArray(row)}, "metric", "as_of", "authorization_version", "predicate_hash", "total", "page", "per_page", "items"))
}

func insightsOperations() []insightsOp {
	cohort := []nativeQuery{{"start", insightsInstant(), true, false}, {"end", insightsInstant(), true, false}}
	selection := append(append([]nativeQuery{}, cohort...), nativeQuery{"clock_mode", nEnum("natural", "business"), true, false}, nativeQuery{"agent_id", insightsUserID(), false, false}, nativeQuery{"team_id", nUUID(), false, false}, nativeQuery{"status", insightsStatus(), false, false})
	drill := append(append([]nativeQuery{}, selection...), nativeQuery{"as_of", insightsInstant(), true, false}, nativeQuery{"predicate_hash", insightsHash(), true, false}, nativeQuery{"metric", insightsMetric(), true, false}, nativeQuery{"page", nRange(1, 9999999), false, false}, nativeQuery{"per_page", nRange(1, 100), false, false})
	return []insightsOp{
		{"csat", "getCrmServiceInsights", "service_insights.metrics", "/v1/crm/service-insights", cohort, insightsCsatSchema()},
		{"metrics", "getCrmServiceMetrics", "service_insights.service_metrics", "/v1/crm/service-insights/service-metrics", selection, insightsMetricsSchema()},
		{"drill", "getCrmServiceInsightDrillThrough", "service_insights.drill", "/v1/crm/service-insights/drill", drill, insightsDrillSchema()},
	}
}

func insightsManifest() []manifestEntry {
	var result []manifestEntry
	for _, op := range insightsOperations() {
		e := manifestEntry{Command: "factuarea crm service-insights " + op.Action, OperationID: op.ID, CrmOperation: op.Operation, Summary: insightsSummary(op), Args: []string{}, Flags: []flagInfo{}, Method: "GET", NativePath: op.Path, AuthRealm: string(client.NativeStaff), OperationIDSource: "native_v1_surface", RequiredScope: insightsScopes[0], RequiredScopes: append([]string{}, insightsScopes...), ResponseSchemas: map[string]json.RawMessage{"200": nRaw(op.Response)}, QuerySchemas: map[string]json.RawMessage{}}
		for _, q := range op.Query {
			e.QuerySchemas[q.Name] = nRaw(q.Schema)
			e.Flags = append(e.Flags, flagInfo{q.Name, "string"})
		}
		result = append(result, e)
	}
	return result
}
func insightsSummary(op insightsOp) string {
	if op.Action == "drill" {
		return "Drill readonly del snapshot original as_of/predicate_hash, con page/per_page nativos"
	}
	if op.Action == "csat" {
		return "CSAT del cohort cerrado [start,end), con decimales y máscaras nativos"
	}
	return "Métricas de servicio readonly del cohort y clock_mode explícitos; sin agregaciones cliente"
}

func registerServiceInsightsCommands(root *cobra.Command) {
	var crm *cobra.Command
	for _, child := range root.Commands() {
		if child.Name() == "crm" {
			crm = child
			break
		}
	}
	if crm == nil {
		panic("native CRM registration required")
	}
	group := &cobra.Command{Use: "service-insights", Short: "Lecturas V1 nativas de Service Insights", Args: groupArgs}
	for _, op := range insightsOperations() {
		group.AddCommand(newInsightsCommand(op))
	}
	crm.AddCommand(group)
}

func newInsightsCommand(op insightsOp) *cobra.Command {
	cmd := &cobra.Command{Use: op.Action, Short: insightsSummary(op), Long: insightsSummary(op) + "\nConsulta el binding V1 declarado por su dueño. El montaje/flags y la autoridad actual C7/máscaras dependen del servidor; no declara disponibilidad ni concede acceso Task/Ticket. UTC6 y filtros exactos, sin cursor ni fallback SPA.", Args: UsageArgs(cobra.NoArgs)}
	f := cmd.Flags()
	for _, q := range op.Query {
		f.String(q.Name, "", "consulta nativa explícita; UTC con seis decimales para instantes")
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		query := url.Values{}
		input := map[string]any{}
		for _, q := range op.Query {
			if !f.Changed(q.Name) {
				if q.Required {
					return apierr.Usagef("falta --%s", q.Name)
				}
				continue
			}
			value, _ := f.GetString(q.Name)
			var typed any = value
			if q.Schema["type"] == "integer" {
				typed = json.Number(value)
			}
			if nativeOwnerSchema(q.Schema, typed) != nil {
				return apierr.Usagef("consulta Service Insights inválida: %s", q.Name)
			}
			if q.Name == "page" || q.Name == "per_page" {
				if value == "" || value[0] == '0' || strings.ContainsAny(value, ".+-eE") {
					return apierr.Usagef("página nativa debe ser integer decimal positivo")
				}
			}
			if q.Name == "start" || q.Name == "end" || q.Name == "as_of" {
				if _, err := parseInsightsInstant(value); err != nil {
					return apierr.Usagef("instante UTC6 nativo inválido: %s", q.Name)
				}
			}
			query.Set(q.Name, value)
			input[q.Name] = typed
		}
		if err := validateInsightsCohort(input["start"].(string), input["end"].(string)); err != nil {
			return err
		}
		g := globalsFrom(cmd)
		cc, err := newCLIContext(g, "")
		if err != nil {
			return err
		}
		scopes, err := cc.scopes(cmd.Context())
		if err != nil {
			return err
		}
		for _, scope := range insightsScopes {
			if !safety.HasScope(scopes, scope) {
				return apierr.Permf("falta scope nativo %s", scope)
			}
		}
		r, err := cc.client.DoNative(cmd.Context(), client.NativeRequest{Realm: client.NativeStaff, Method: "GET", Path: op.Path + "?" + query.Encode()})
		if err != nil {
			return reportNativeOwnerError(cmd, cc, nativeOwnerOp{Realm: client.NativeStaff}, err)
		}
		if err := validateInsightsResponse(op, r, input); err != nil {
			return reportNativeOwnerError(cmd, cc, nativeOwnerOp{Realm: client.NativeStaff}, err)
		}
		return output.PrintBody(cmd.OutOrStdout(), r.Body, cc.format)
	}
	return cmd
}

func parseInsightsInstant(value string) (time.Time, error) {
	if knowledgeSchema(string(nRaw(insightsInstant())), value) != nil {
		return time.Time{}, apierr.Usagef("instante UTC6 inválido")
	}
	at, err := time.Parse(insightsInstantLayout, value)
	if err != nil || at.Format(insightsInstantLayout) != value {
		return time.Time{}, apierr.Usagef("instante UTC6 inválido")
	}
	return at, nil
}
func validateInsightsCohort(start, end string) error {
	a, err := parseInsightsInstant(start)
	b, err2 := parseInsightsInstant(end)
	if err != nil || err2 != nil || !a.Before(b) || b.Sub(a) > 366*24*time.Hour {
		return apierr.Usagef("cohort nativo [start,end) inválido o mayor que 366 días")
	}
	return nil
}

func validateInsightsResponse(op insightsOp, r *client.Response, input map[string]any) error {
	invalid := func() error {
		return &apierr.APIError{StatusCode: 502, Type: "api_error", Code: "invalid_native_response", Message: "La respuesta Service Insights no coincide con el contrato y snapshot nativos."}
	}
	if r == nil {
		return invalid()
	}
	value, err := nativeOriginalJSON(r.Body)
	if r.StatusCode != 200 || err != nil || nativeOwnerSchema(op.Response, value) != nil {
		return invalid()
	}
	data := value.(map[string]any)["data"].(map[string]any)
	if op.Action == "csat" {
		if data["cohort_start"] != input["start"] || data["cohort_end"] != input["end"] {
			return invalid()
		}
		if !validInsightsCsatBoundary(data) {
			return invalid()
		}
		return nil
	}
	if _, err := parseInsightsInstant(data["as_of"].(string)); err != nil {
		return invalid()
	}
	if op.Action == "metrics" {
		selection := data["selection"].(map[string]any)
		for _, name := range []string{"start", "end", "clock_mode", "agent_id", "team_id", "status"} {
			if selection[name] != input[name] {
				return invalid()
			}
		}
		for name, raw := range data["metrics"].(map[string]any) {
			metric := raw.(map[string]any)
			if !validInsightsMetricBoundary(name, metric, nativeInteger(data["cohort_count"])) {
				return invalid()
			}
		}
		return nil
	}
	if data["as_of"] != input["as_of"] || data["predicate_hash"] != input["predicate_hash"] || data["metric"] != input["metric"] {
		return invalid()
	}
	for _, name := range []string{"page", "per_page"} {
		if expected, present := input[name]; present {
			n, ok := knowledgeInt(expected)
			actual, aok := knowledgeInt(data[name])
			if !ok || !aok || n != actual {
				return invalid()
			}
		}
	}
	items := data["items"].([]any)
	total := nativeInteger(data["total"])
	perPage := nativeInteger(data["per_page"])
	page := nativeInteger(data["page"])
	if int64(len(items)) > perPage || int64(len(items)) > total || !validInsightsDrillBoundary(data) {
		return invalid()
	}
	// Missing query values remain omitted on the wire; validate the native server defaults only in its response.
	if _, present := input["page"]; !present && page != 1 {
		return invalid()
	}
	if _, present := input["per_page"]; !present && perPage != 25 {
		return invalid()
	}
	seen := map[any]bool{}
	for _, raw := range items {
		row := raw.(map[string]any)
		if seen[row["id"]] {
			return invalid()
		}
		seen[row["id"]] = true
	}
	return nil
}
