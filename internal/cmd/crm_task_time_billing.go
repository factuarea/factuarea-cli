package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
)

const taskTimeMaximumWire = 262144

type taskTimeOp struct {
	Action, ID, Operation, Path, Case, Scope, Method string
	Mutating, Recovery                               bool
	Body, Response                                   nativeSchema
}

func taskTimeRequest(caseName string) nativeSchema {
	base := nativeSchema{"task_id": nUUID(), "expected_ticket_version": nInt(1), "expected_task_version": nInt(1)}
	required := []string{"task_id", "expected_ticket_version", "expected_task_version"}
	if caseName == "preview" {
		entries := nArray(nObject(nativeSchema{"id": nUUID(), "revision": nativeSchema{"type": "string", "pattern": `^[a-f0-9]{64}$`}}, "id", "revision"))
		entries["minItems"], entries["maxItems"] = 1, 500
		base["entries"], base["grouping"] = entries, nEnum("per_task", "per_entry", "single_line")
		return nObject(base, append(required, "entries", "grouping")...)
	}
	base["binding_id"], base["expected_binding_version"] = nUUID(), nInt(1)
	if strings.HasPrefix(caseName, "recover-") {
		action := strings.TrimPrefix(caseName, "recover-")
		base["effect_id"], base["action"] = nUUID(), nativeSchema{"type": "string", "const": action}
		required = append(required, "binding_id", "effect_id", "action")
		if action == "unlink" {
			required = append(required, "expected_binding_version")
		}
		return nObject(base, required...)
	}
	base["confirmed"] = nativeSchema{"type": "boolean", "const": true}
	required = append(required, "confirmed")
	if caseName == "unlink" {
		required = append(required, "binding_id", "expected_binding_version")
	} else {
		base["binding_id"], base["expected_binding_version"] = nNullable(nUUID()), nNullable(nInt(1))
	}
	return nObject(base, required...)
}

func taskTimeResponse(caseName string) nativeSchema {
	text := nativeSchema{"type": "string"}
	integer := nativeSchema{"type": "integer"}
	version := nativeSchema{"type": "integer", "minimum": 1, "maximum": int64(9223372036854775807)}
	base := nativeSchema{"ticket_id": nUUID(), "ticket_version": version, "authorization_version": text}
	required := []string{"ticket_id", "ticket_version", "authorization_version"}
	if caseName == "link" || caseName == "unlink" {
		binding := nObject(nativeSchema{"id": nUUID(), "task_id": nUUID(), "version": version, "archived_at": nNullable(text)}, "id", "task_id", "version")
		base["status"], base["effect_id"] = nativeSchema{"type": "string", "const": "confirmed"}, nUUID()
		base["operation"], base["confirmed_at"] = nativeSchema{"type": "string", "const": "customer_service.task_time_" + caseName}, nNullable(text)
		base["result"] = nObject(nativeSchema{"binding": binding, "task_version": version}, "binding")
		return nEnvelope(nObject(base, append(required, "status", "effect_id", "operation", "confirmed_at", "result")...))
	}
	if caseName == "preview" {
		line := nObject(nativeSchema{"description": text, "quantity": text, "unit": text, "unit_price": text, "amount": text}, "description", "quantity", "unit", "unit_price", "amount")
		totals := nObject(nativeSchema{"subtotal": text, "vat": text, "surcharge": text, "retention": text, "total": text, "vat_rate": nNullable(nativeSchema{"type": "number"}), "surcharge_rate": nNullable(nativeSchema{"type": "number"}), "retention_rate": nNullable(nativeSchema{"type": "number"})}, "subtotal", "vat", "surcharge", "retention", "total", "vat_rate", "surcharge_rate", "retention_rate")
		base["preview"] = nObject(nativeSchema{"contact": nObject(nativeSchema{"id": nHost(), "name": text}, "id", "name"), "lines": nArray(line), "total_seconds": integer, "total_hours": text, "time_entry_ids": nArray(nUUID()), "warnings": nArray(text), "totals": nNullable(totals)}, "contact", "lines", "total_seconds", "total_hours", "time_entry_ids", "warnings")
		return nEnvelope(nObject(base, append(required, "preview")...))
	}
	base["revision"] = text
	required = append(required, "revision")
	if caseName == "tasks" {
		base["tasks"] = nArray(nObject(nativeSchema{"id": nUUID(), "task_id": nUUID(), "version": version, "archived_at": nNullable(text), "task_title": text, "task_version": version}, "id", "task_id"))
		return nEnvelope(nObject(base, append(required, "tasks")...))
	}
	base["entries"] = nArray(nObject(nativeSchema{"id": nUUID(), "task_id": nUUID(), "revision": text, "started_at": text, "ended_at": nNullable(text), "duration_seconds": nNullable(integer), "description": nNullable(text), "billable": nativeSchema{"type": "boolean"}, "user_id": nHost(), "invoice_id": nNullable(nHost()), "billing_batch_id": nNullable(nHost())}, "id", "task_id"))
	base["next_cursor"] = nNullable(text)
	total := "total_closed_seconds"
	if caseName == "billable" {
		total = "total_billable_closed_seconds"
	}
	base[total] = integer
	return nEnvelope(nObject(base, append(required, "entries", "next_cursor")...))
}

func taskTimeOperations() []taskTimeOp {
	base := "/v1/crm/service-tickets/{ticket}/"
	return []taskTimeOp{
		{"link", "linkServiceTicketTask", "customer_service.task_time_link", base + "tasks/link", "link", "customer_service:write", "POST", true, false, taskTimeRequest("link"), taskTimeResponse("link")},
		{"unlink", "unlinkServiceTicketTask", "customer_service.task_time_unlink", base + "tasks/unlink", "unlink", "customer_service:write", "POST", true, false, taskTimeRequest("unlink"), taskTimeResponse("unlink")},
		{"tasks", "listServiceTicketTasks", "customer_service.task_time_list", base + "tasks", "tasks", "customer_service:read", "GET", false, false, nil, taskTimeResponse("tasks")},
		{"billable", "getServiceBillableTime", "customer_service.task_time_list", base + "billable-time", "billable", "customer_service:read", "GET", false, false, nil, taskTimeResponse("billable")},
		{"timesheet", "getServiceTimesheet", "customer_service.task_time_list", base + "timesheet", "timesheet", "customer_service:read", "GET", false, false, nil, taskTimeResponse("timesheet")},
		{"preview", "previewServiceTimeInvoice", "customer_service.task_time_preview", base + "time-invoice-preview", "preview", "customer_service:read", "POST", false, false, taskTimeRequest("preview"), taskTimeResponse("preview")},
		{"recover-link", "getOriginalServiceTicketTaskLinkReceipt", "customer_service.task_time_link", base + "task-time/link/original-receipt", "link", "customer_service:write", "POST", false, true, taskTimeRequest("recover-link"), taskTimeResponse("link")},
		{"recover-unlink", "getOriginalServiceTicketTaskUnlinkReceipt", "customer_service.task_time_unlink", base + "task-time/unlink/original-receipt", "unlink", "customer_service:write", "POST", false, true, taskTimeRequest("recover-unlink"), taskTimeResponse("unlink")},
	}
}

func taskTimeManifest() []manifestEntry {
	var entries []manifestEntry
	for _, op := range taskTimeOperations() {
		e := manifestEntry{Command: "factuarea crm task-time-billing " + op.Action, OperationID: op.ID, CrmOperation: op.Operation, Summary: taskTimeSummary(op), Args: []string{"ticket"}, Flags: []flagInfo{}, Mutating: op.Mutating, RequiredScope: op.Scope, RequiredScopes: []string{op.Scope}, IdempotencyRequired: op.Mutating, Method: op.Method, NativePath: op.Path, AuthRealm: string(client.NativeStaff), OperationIDSource: "native_v1_surface", OriginalRecovery: op.Recovery, ResponseSchemas: map[string]json.RawMessage{"200": nRaw(op.Response)}, QuerySchemas: map[string]json.RawMessage{}}
		if op.Body != nil {
			e.RequestSchema = nRaw(op.Body)
			e.MaxBodyBytes = taskTimeMaximumWire
			e.Flags = append(e.Flags, flagInfo{"data", "string"}, flagInfo{"data-file", "string"})
		}
		if op.Method == "POST" {
			e.Flags = append(e.Flags, flagInfo{"idempotency-key", "string"}, flagInfo{"idempotency-key-file", "string"})
		}
		if op.Case == "timesheet" || op.Case == "billable" {
			e.QuerySchemas["cursor"] = nRaw(taskTimeCursor())
			e.QuerySchemas["limit"] = nRaw(nRange(1, 100))
			e.Flags = append(e.Flags, flagInfo{"cursor", "string"}, flagInfo{"limit", "string"})
		}
		entries = append(entries, e)
	}
	return entries
}

func taskTimeCursor() nativeSchema {
	return nativeSchema{"type": "string", "pattern": `^[a-f0-9]{64}:[a-f0-9-]{36}$`}
}
func taskTimeSummary(op taskTimeOp) string {
	if op.Mutating {
		return "Vínculo Task nativo con CAS, confirmed:true, clave y JSON originales; sin reenvío"
	}
	if op.Recovery {
		return "Query del recibo ORIGINAL por efecto/binding/CAS ya conocidos; no reserva ni reenvía"
	}
	if op.Case == "preview" {
		return "Preview Invoice readonly; Money conserva strings; no factura ni crea efectos"
	}
	return "Lectura Task nativa con máscaras actuales; ausencia y null permanecen distintos"
}

func registerTaskTimeBillingCommands(root *cobra.Command) {
	var crm *cobra.Command
	for _, child := range root.Commands() {
		if child.Name() == "crm" {
			crm = child
			break
		}
	}
	if crm == nil {
		panic("native CRM command registration required")
	}
	group := &cobra.Command{Use: "task-time-billing", Short: "Tasks y tiempo del Ticket por el transporte V1 nativo", Args: groupArgs}
	for _, op := range taskTimeOperations() {
		group.AddCommand(newTaskTimeCommand(op))
	}
	crm.AddCommand(group)
}

func newTaskTimeCommand(op taskTimeOp) *cobra.Command {
	var data, file, key, keyFile string
	cmd := &cobra.Command{Use: op.Action + " <ticket>", Short: taskTimeSummary(op), Long: taskTimeSummary(op) + "\nEl servidor decide C7, máscaras y permisos Task/Invoice. Si se pierde el ACK antes de recibir los IDs originales, conserva la intención: no se inventan IDs ni se reenvía. Preview/recuperación aceptan una clave HTTP explícita opcional si la política nativa la requiere; no es un claim del dominio.", Args: UsageArgs(cobra.ExactArgs(1))}
	f := cmd.Flags()
	if op.Body != nil {
		f.StringVarP(&data, "data", "d", "", "JSON nativo exacto, @fichero o - para stdin")
		f.StringVar(&file, "data-file", "", "fichero con los bytes JSON nativos")
	}
	if op.Method == "POST" {
		f.StringVar(&key, "idempotency-key", "", "clave HTTP explícita; obligatoria y ORIGINAL sólo para link/unlink")
		f.StringVar(&keyFile, "idempotency-key-file", "", "fichero de clave exacta; sin recortar ni generar")
	}
	if op.Case == "timesheet" || op.Case == "billable" {
		f.String("cursor", "", "cursor opaco nativo de revisión actual")
		f.String("limit", "", "límite 1..100; omisión conserva el default del servidor")
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if knowledgeSchema(string(nRaw(nUUID())), args[0]) != nil {
			return apierr.Usagef("ticket debe ser un UUID v7 nativo")
		}
		path := strings.ReplaceAll(op.Path, "{ticket}", url.PathEscape(args[0]))
		query := url.Values{}
		for _, name := range []string{"cursor", "limit"} {
			if f.Lookup(name) == nil || !f.Changed(name) {
				continue
			}
			v, _ := f.GetString(name)
			var value any = v
			s := taskTimeCursor()
			if name == "limit" {
				value = json.Number(v)
				s = nRange(1, 100)
			}
			if nativeOwnerSchema(s, value) != nil {
				return apierr.Usagef("consulta nativa inválida: %s", name)
			}
			query.Set(name, v)
		}
		if len(query) > 0 {
			path += "?" + query.Encode()
		}
		if keyFile != "" {
			if key != "" {
				return apierr.Usagef("usa una sola fuente para la clave HTTP")
			}
			b, err := nativeReadBoundedFile(keyFile, 255)
			if err != nil {
				return apierr.ReadFileUsagef(keyFile, err)
			}
			key = string(b)
		}
		if op.Mutating || key != "" {
			if err := validateTaskTimeKey(key); err != nil {
				return err
			}
		} else if f.Lookup("idempotency-key") != nil && (f.Changed("idempotency-key") || f.Changed("idempotency-key-file")) {
			return apierr.Usagef("clave HTTP explícita vacía")
		}
		var body []byte
		var input map[string]any
		if op.Body != nil {
			if data != "" && file != "" {
				return apierr.Usagef("usa --data o --data-file")
			}
			var err error
			switch {
			case file != "":
				body, err = nativeReadBoundedFile(file, taskTimeMaximumWire)
			case data == "-":
				body, err = io.ReadAll(io.LimitReader(cmd.InOrStdin(), taskTimeMaximumWire+1))
			case strings.HasPrefix(data, "@"):
				body, err = nativeReadBoundedFile(strings.TrimPrefix(data, "@"), taskTimeMaximumWire)
			case data != "":
				body = []byte(data)
			default:
				return apierr.Usagef("falta --data o --data-file nativo")
			}
			if err != nil || len(body) > taskTimeMaximumWire {
				return apierr.Usagef("no se pudo leer el JSON nativo acotado")
			}
			value, e := nativeOriginalJSON(body)
			input, _ = value.(map[string]any)
			if e != nil || input == nil || nativeOwnerSchema(op.Body, value) != nil {
				return apierr.Usagef("el JSON no cumple el contrato nativo V1 cerrado")
			}
			if err := validateTaskTimeInput(op, input); err != nil {
				return err
			}
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
		if !safety.HasScope(scopes, op.Scope) {
			return apierr.Permf("falta scope nativo %s", op.Scope)
		}
		if op.Mutating {
			if err := safety.RequireLive(cc.res.Environment, g.Live); err != nil {
				return err
			}
		}
		r, err := cc.client.DoNative(cmd.Context(), client.NativeRequest{Realm: client.NativeStaff, Method: op.Method, Path: path, Body: body, OriginalKey: key})
		if err != nil {
			return reportTaskTimeError(cmd, cc, op, err)
		}
		if err = validateTaskTimeResponse(op, r, args[0], input); err != nil {
			return reportTaskTimeError(cmd, cc, op, err)
		}
		return output.PrintBody(cmd.OutOrStdout(), r.Body, cc.format)
	}
	return cmd
}

func validateTaskTimeKey(key string) error {
	if key == "" || len(key) > 255 || strings.TrimSpace(key) == "" {
		return apierr.Usagef("falta clave original ASCII visible 1..255; no se genera")
	}
	// The configured HTTP client trims header edge spaces: reject rather than change the original key.
	if strings.Trim(key, " ") != key {
		return apierr.Usagef("la clave original no puede representarse sin cambios como cabecera HTTP")
	}
	for i := range len(key) {
		if key[i] < 32 || key[i] > 126 {
			return apierr.Usagef("clave original ASCII visible inválida")
		}
	}
	return nil
}

func validateTaskTimeInput(op taskTimeOp, input map[string]any) error {
	if op.Mutating && op.Case == "link" {
		if (input["binding_id"] == nil) != (input["expected_binding_version"] == nil) {
			return apierr.Usagef("binding_id y expected_binding_version deben acreditar el mismo original o permanecer ausentes/null")
		}
	}
	if op.Case == "preview" {
		seen := map[any]bool{}
		for _, raw := range input["entries"].([]any) {
			entry := raw.(map[string]any)
			if seen[entry["id"]] {
				return apierr.Usagef("selección original de entries duplicada")
			}
			seen[entry["id"]] = true
		}
	}
	return nil
}

func validateTaskTimeResponse(op taskTimeOp, r *client.Response, ticket string, input map[string]any) error {
	invalid := func() error {
		if op.Mutating {
			return &apierr.TransportError{Err: errors.New(taskTimeUnknown())}
		}
		return &apierr.APIError{StatusCode: 502, Type: "api_error", Code: "invalid_native_response", Message: "La respuesta no cumple el contrato nativo TaskTimeBilling."}
	}
	value, err := nativeOriginalJSON(r.Body)
	if r.StatusCode != 200 || err != nil || nativeOwnerSchema(op.Response, value) != nil {
		return invalid()
	}
	data := value.(map[string]any)["data"].(map[string]any)
	if data["ticket_id"] != ticket {
		return invalid()
	}
	if op.Case == "link" || op.Case == "unlink" {
		result := data["result"].(map[string]any)
		binding := result["binding"].(map[string]any)
		expected, ok := knowledgeInt(input["expected_ticket_version"])
		actual, aok := knowledgeInt(data["ticket_version"])
		if !ok || !aok || actual != expected+1 || binding["task_id"] != input["task_id"] {
			return invalid()
		}
		if input["binding_id"] != nil && binding["id"] != input["binding_id"] {
			return invalid()
		}
		if op.Recovery && data["effect_id"] != input["effect_id"] {
			return invalid()
		}
		if taskVersion, present := result["task_version"]; present && taskVersion != input["expected_task_version"] {
			return invalid()
		}
		version, _ := knowledgeInt(binding["version"])
		if input["expected_binding_version"] == nil {
			if version != 1 {
				return invalid()
			}
		} else {
			expected, _ := knowledgeInt(input["expected_binding_version"])
			// Native transition is idempotent when its archive state already matches.
			if version != expected && version != expected+1 {
				return invalid()
			}
		}
		if archived, present := binding["archived_at"]; present && ((op.Case == "link" && archived != nil) || (op.Case == "unlink" && archived == nil)) {
			return invalid()
		}
	}
	if op.Case == "preview" {
		if data["ticket_version"] != input["expected_ticket_version"] {
			return invalid()
		}
		preview := data["preview"].(map[string]any)
		entries := input["entries"].([]any)
		ids := preview["time_entry_ids"].([]any)
		if len(ids) != len(entries) {
			return invalid()
		}
		selected := map[any]bool{}
		for _, raw := range entries {
			selected[raw.(map[string]any)["id"]] = true
		}
		for _, id := range ids {
			if !selected[id] {
				return invalid()
			}
			delete(selected, id)
		}
	}
	return nil
}

func taskTimeUnknown() string {
	return "resultado no confirmado; conserva la clave y JSON originales. Recupera explícitamente sólo si ya conoces effect_id y binding_id originales con sus CAS; sin esos IDs no se puede reconciliar. No inventes IDs ni reenvíes la escritura"
}
func reportTaskTimeError(cmd *cobra.Command, cc *cliContext, op taskTimeOp, err error) error {
	var transport *apierr.TransportError
	var native *apierr.APIError
	if errors.As(err, &transport) {
		message := "consulta TaskTimeBilling no confirmada"
		if op.Mutating {
			message = taskTimeUnknown()
		}
		err = &apierr.TransportError{Err: fmt.Errorf("%s", message)}
	} else if op.Mutating && errors.As(err, &native) && (native.StatusCode >= 500 || native.StatusCode == 408 || native.StatusCode == 307 || native.StatusCode == 308) {
		copy := *native
		copy.Message += ". " + taskTimeUnknown()
		err = &copy
	}
	output.PrintError(cmd.ErrOrStderr(), err, cc.errorFormat)
	return &AlreadyReported{Err: err}
}
