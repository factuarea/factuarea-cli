package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
)

type nativeSchema map[string]any
type nativeQuery struct {
	Name           string
	Schema         nativeSchema
	Required, JSON bool
}
type nativeOwnerOp struct {
	Groups                                                                         []string
	Action, OperationID, Operation, Summary, Method, Path, Scope                   string
	Args                                                                           []string
	OptionalArg                                                                    bool
	Realm                                                                          client.NativeAuthRealm
	Mutating, Original, Confirm, Recovery, PublicRecovery, Publication, Management bool
	Body, Response                                                                 nativeSchema
	Query                                                                          []nativeQuery
	Kind                                                                           string
	Status                                                                         int
}

func nObject(properties nativeSchema, required ...string) nativeSchema {
	return nativeSchema{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
}
func nText(min, max int) nativeSchema {
	return nativeSchema{"type": "string", "minLength": min, "maxLength": max}
}
func nInt(min int) nativeSchema {
	return nativeSchema{"type": "integer", "minimum": min, "maximum": int64(9223372036854775806)}
}
func nRange(min, max int) nativeSchema    { s := nInt(min); s["maximum"] = max; return s }
func nEnum(values ...string) nativeSchema { return nativeSchema{"type": "string", "enum": values} }
func nNullable(s nativeSchema) nativeSchema {
	return nativeSchema{"anyOf": []any{s, nativeSchema{"type": "null"}}}
}
func nArray(s nativeSchema) nativeSchema { return nativeSchema{"type": "array", "items": s} }
func nUUID() nativeSchema {
	return nativeSchema{"type": "string", "pattern": `^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`}
}
func nHost() nativeSchema {
	return nativeSchema{"type": "string", "pattern": `^[0-9a-f]{8}-[0-9a-f]{4}-[47][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`}
}
func nEnvelope(s nativeSchema) nativeSchema { return nObject(nativeSchema{"data": s}, "data") }
func nRaw(s nativeSchema) json.RawMessage {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return b
}

// Reuse the existing contract checker and cover the native Booking map and
// contains keywords absent from the previous KB/PCH contracts.
func nativeOwnerSchema(s nativeSchema, value any) error {
	var schema map[string]any
	if err := decodeKnowledgeJSON(nRaw(s), &schema); err != nil {
		return err
	}
	var check func(map[string]any, any) error
	check = func(schema map[string]any, value any) error {
		if err := checkKnowledgeSchema(schema, value, "$"); err != nil {
			return err
		}
		for _, name := range []string{"anyOf", "oneOf", "allOf"} {
			if choices, ok := schema[name].([]any); ok {
				for _, raw := range choices {
					child, _ := raw.(map[string]any)
					if checkKnowledgeSchema(child, value, "$") == nil {
						if err := check(child, value); err != nil {
							return err
						}
					}
				}
			}
		}
		if object, ok := value.(map[string]any); ok {
			properties, _ := schema["properties"].(map[string]any)
			extra, _ := schema["additionalProperties"].(map[string]any)
			for name, item := range object {
				child, _ := properties[name].(map[string]any)
				if child == nil {
					child = extra
				}
				if child != nil {
					if err := check(child, item); err != nil {
						return err
					}
				}
			}
		}
		if array, ok := value.([]any); ok {
			items, _ := schema["items"].(map[string]any)
			contains, _ := schema["contains"].(map[string]any)
			found := false
			for _, item := range array {
				if items != nil {
					if err := check(items, item); err != nil {
						return err
					}
				}
				if contains != nil && checkKnowledgeSchema(contains, item, "$") == nil {
					found = true
				}
			}
			if contains != nil && !found {
				return fmt.Errorf("lista sin valor nativo requerido")
			}
		}
		return nil
	}
	return check(schema, value)
}

func nativeOwnerOperations() []nativeOwnerOp {
	return append(nativeSequenceOperations(), nativeBookingOperations()...)
}
func registerNativeOwnerCommands(root *cobra.Command) {
	for _, op := range nativeOwnerOperations() {
		parent := root
		for _, name := range op.Groups {
			var next *cobra.Command
			for _, existing := range parent.Commands() {
				if existing.Name() == name {
					next = existing
					break
				}
			}
			if next == nil {
				next = &cobra.Command{Use: name, Short: "Operaciones del transporte nativo", Args: groupArgs}
				parent.AddCommand(next)
			}
			parent = next
		}
		parent.AddCommand(newNativeOwnerCommand(op))
	}
}

func nativeOwnerManifest() []manifestEntry {
	var result []manifestEntry
	for _, op := range nativeOwnerOperations() {
		entry := manifestEntry{Command: strings.Join(append(append([]string{"factuarea"}, op.Groups...), op.Action), " "), OperationID: op.OperationID, CrmOperation: op.Operation, Summary: op.Summary, Args: op.Args, Flags: []flagInfo{}, Mutating: op.Mutating, Irreversible: op.Action == "cancel", RequiresConfirmation: op.Confirm && op.Realm == client.NativeStaff, LocalConfirmation: op.Confirm, RequiredScope: op.Scope, IdempotencyRequired: op.Original, Method: op.Method, NativePath: op.Path, AuthRealm: string(op.Realm), OriginalRecovery: op.Recovery || op.Kind == "sequence-original-save" || op.Kind == "sequence-original-enrol", QuerySchemas: map[string]json.RawMessage{}, ResponseSchemas: map[string]json.RawMessage{}, MaxBodyBytes: 65536}
		if entry.Args == nil {
			entry.Args = []string{}
		}
		entry.OperationIDSource = "native_v1_surface"
		if op.Realm == client.NativeBookingVisitor {
			entry.OperationIDSource = "native_public_route"
		}
		if op.Scope != "" {
			entry.RequiredScopes = []string{op.Scope}
		}
		if op.Body != nil {
			entry.RequestSchema = nRaw(op.Body)
			entry.Flags = append(entry.Flags, flagInfo{"data", "string"}, flagInfo{"data-file", "string"})
		} else {
			entry.MaxBodyBytes = 0
		}
		if op.Recovery {
			entry.OriginalBodySchema = nRaw(op.Body)
			entry.RequestSchema = nRaw(nObject(nativeSchema{"original_wire_body": nText(1, 65536)}, "original_wire_body"))
			entry.MaxBodyBytes = 393216
		}
		if op.PublicRecovery {
			entry.RequestSchema = nRaw(nObject(nativeSchema{"operation": nEnum("booking.create_public", "booking.reschedule_public", "booking.cancel_public"), "original_wire_body": nText(1, 65536)}, "operation", "original_wire_body"))
			entry.Flags = append(entry.Flags, flagInfo{"operation", "string"})
		}
		if op.Kind == "sequence-control" {
			entry.ResponseSchemas["204"] = json.RawMessage(`{"x-empty-body":true,"description":"HTTP 204 with no response content; no original receipt"}`)
		} else {
			status := op.Status
			if status == 0 {
				status = 200
			}
			entry.ResponseSchemas[strconv.Itoa(status)] = nRaw(op.Response)
		}
		for _, q := range op.Query {
			entry.Flags = append(entry.Flags, flagInfo{q.Name, "string"})
			entry.QuerySchemas[q.Name] = nRaw(q.Schema)
		}
		if op.Original {
			entry.Flags = append(entry.Flags, flagInfo{"idempotency-key", "string"}, flagInfo{"idempotency-key-file", "string"})
		}
		if op.Confirm {
			entry.Flags = append(entry.Flags, flagInfo{"confirm", "string"})
		}
		if op.Publication {
			entry.Flags = append(entry.Flags, flagInfo{"publication-version", "string"})
		}
		if op.Management {
			entry.Flags = append(entry.Flags, flagInfo{"management-token-file", "string"})
		}
		if op.Realm == client.NativeBookingVisitor && op.Original {
			entry.Flags = append(entry.Flags, flagInfo{"secret-output-file", "string"})
		}
		result = append(result, entry)
	}
	return result
}

func newNativeOwnerCommand(op nativeOwnerOp) *cobra.Command {
	use := op.Action
	for _, arg := range op.Args {
		if op.OptionalArg {
			use += " [" + arg + "]"
		} else {
			use += " <" + arg + ">"
		}
	}
	var data, file, key, keyFile, confirm, publication, tokenFile, secretFile, originalOperation string
	cmd := &cobra.Command{Use: use, Short: op.Summary, Long: op.Summary + "\nUsa el endpoint nativo actual. La disponibilidad, C7, campos y política los decide el servidor. No reenvía escrituras automáticamente."}
	if op.OptionalArg {
		cmd.Args = UsageArgs(cobra.RangeArgs(0, 1))
	} else {
		cmd.Args = UsageArgs(cobra.ExactArgs(len(op.Args)))
	}
	f := cmd.Flags()
	if op.Body != nil {
		f.StringVarP(&data, "data", "d", "", "JSON original exacto, @fichero o - para stdin")
		f.StringVar(&file, "data-file", "", "fichero con los bytes JSON originales")
	}
	if op.Original {
		f.StringVar(&key, "idempotency-key", "", "clave ORIGINAL opaca; obligatoria, no se genera")
		f.StringVar(&keyFile, "idempotency-key-file", "", "fichero privado con la clave original exacta (sin recortar)")
	}
	if op.Confirm {
		f.StringVar(&confirm, "confirm", "", "confirmación local del ID destino (create para alta; cancel para cancelación pública)")
	}
	if op.Publication {
		f.StringVar(&publication, "publication-version", "", "versión observada de la publicación; cabecera X-Booking-Publication-Version")
	}
	if op.Management {
		f.StringVar(&tokenFile, "management-token-file", "", "token privado exacto; alternativa FACTUAREA_BOOKING_MANAGEMENT_TOKEN")
	}
	if op.Realm == client.NativeBookingVisitor && op.Original {
		f.StringVar(&secretFile, "secret-output-file", "", "exporta el manage_token nativo a un fichero NUEVO 0600; stdout siempre redactado")
	}
	if op.PublicRecovery {
		f.StringVar(&originalOperation, "operation", "", "operación ORIGINAL: booking.create_public|booking.reschedule_public|booking.cancel_public")
	}
	for _, q := range op.Query {
		f.String(q.Name, "", "consulta nativa explícita (sin valor inventado)")
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		g := globalsFrom(cmd)
		path := op.Path
		for i, name := range op.Args {
			if i >= len(args) {
				path = strings.TrimSuffix(path, "/{"+name+"}")
				continue
			}
			s := nUUID()
			if name == "publicId" {
				s = nativeSchema{"type": "string", "pattern": `^[A-Za-z0-9_-]{32,128}$`}
			}
			if err := knowledgeSchema(string(nRaw(s)), args[i]); err != nil {
				return apierr.Usagef("identificador nativo inválido: %s", name)
			}
			path = strings.ReplaceAll(path, "{"+name+"}", url.PathEscape(args[i]))
		}
		queries := url.Values{}
		queryInput := map[string]any{}
		for _, q := range op.Query {
			if !f.Changed(q.Name) {
				if q.Required {
					return apierr.Usagef("falta --%s", q.Name)
				}
				continue
			}
			v, _ := f.GetString(q.Name)
			var typed any = v
			if q.JSON {
				parsed, err := nativeOriginalJSON([]byte(v))
				if err != nil || len(v) > 8192 {
					return apierr.Usagef("consulta JSON inválida: %s", q.Name)
				}
				typed = parsed
			} else if q.Schema["type"] == "integer" {
				typed = json.Number(v)
			}
			if err := nativeOwnerSchema(q.Schema, typed); err != nil {
				return apierr.Usagef("consulta nativa inválida: %s", q.Name)
			}
			if q.Name == "filters" {
				if err := validateSequenceFilters(typed); err != nil {
					return err
				}
			}
			if q.Name == "start_at" || q.Name == "from" || q.Name == "through" || q.Name == "to" {
				if err := validateNativeInstant(v, op.Kind != "sequence-preview"); err != nil {
					return apierr.Usagef("instante nativo inválido: %s", q.Name)
				}
			}
			if q.Name == "visitor_timezone" {
				if err := validateNativeTimezone(v); err != nil {
					return err
				}
			}
			queries.Set(q.Name, v)
			queryInput[q.Name] = typed
		}
		if len(queries) > 0 {
			path += "?" + queries.Encode()
		}
		if keyFile != "" {
			if key != "" {
				return apierr.Usagef("usa una sola fuente para la clave original")
			}
			b, err := nativeReadBoundedFile(keyFile, 512)
			if err != nil {
				return apierr.ReadFileUsagef(keyFile, err)
			}
			key = string(b)
		}
		if op.Original {
			if err := validateNativeOriginalKey(op, key); err != nil {
				return err
			}
		}
		var body []byte
		var input any = queryInput
		if op.Body != nil {
			if file != "" && data != "" {
				return apierr.Usagef("usa --data o --data-file")
			}
			var err error
			if file != "" {
				body, err = nativeReadBoundedFile(file, 65536)
			} else if data != "" {
				if data == "-" {
					body, err = io.ReadAll(io.LimitReader(cmd.InOrStdin(), 65537))
				} else if strings.HasPrefix(data, "@") {
					body, err = nativeReadBoundedFile(strings.TrimPrefix(data, "@"), 65536)
				} else {
					body = []byte(data)
				}
			} else {
				return apierr.Usagef("falta el JSON original --data o --data-file")
			}
			if err != nil {
				return apierr.Usagef("no se pudo leer el JSON original")
			}
			input, err = nativeOriginalJSON(body)
			if err != nil || len(body) > 65536 || nativeOwnerSchema(op.Body, input) != nil {
				return apierr.Usagef("el JSON original no cumple el contrato nativo cerrado")
			}
			if err := validateNativeOwnerBody(op, input, len(args) > 0, originalOperation); err != nil {
				return err
			}
			if op.Recovery {
				payload := map[string]any{"original_wire_body": string(body)}
				if op.PublicRecovery {
					payload["operation"] = originalOperation
				}
				body, _ = json.Marshal(payload)
				if len(body) > 393216 {
					return apierr.Usagef("el sobre de recuperación supera el límite nativo")
				}
			}
		}
		headers := map[string]string{}
		if op.Publication {
			if knowledgeSchema(string(nRaw(nInt(1))), json.Number(publication)) != nil {
				return apierr.Usagef("falta una --publication-version entera observada")
			}
			headers["X-Booking-Publication-Version"] = publication
		}
		if op.Management {
			token := os.Getenv("FACTUAREA_BOOKING_MANAGEMENT_TOKEN")
			if tokenFile != "" {
				b, err := nativeReadBoundedFile(tokenFile, 128)
				if err != nil {
					return apierr.Usagef("no se pudo leer el token privado")
				}
				token = string(b)
			}
			if token == "" || len(token) > 128 || strings.ContainsAny(token, "\r\n") {
				return apierr.Usagef("falta el token de gestión nativo válido")
			}
			headers["Booking-Management-Token"] = token
		}
		if op.Confirm {
			target := "create"
			if len(args) > 0 {
				target = args[0]
			}
			if op.Kind == "public-cancel" {
				target = "cancel"
			}
			if err := safety.Confirm(target, confirm, false, g.NoInput, nil); err != nil {
				return err
			}
		}
		var secret *os.File
		if secretFile != "" {
			var err error
			secret, err = os.OpenFile(secretFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return apierr.WriteFileUsagef(secretFile, err)
			}
			defer secret.Close()
		}
		var cc *cliContext
		if op.Realm == client.NativeStaff {
			var err error
			cc, err = newCLIContext(g, "")
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
		} else {
			opts, err := baseURLClientOptions(g.AllowInsecureTransport)
			if err != nil {
				return err
			}
			cc = &cliContext{client: client.New("", opts...), format: output.ResolveFormat(g.JSON, output.IsTTY(os.Stdout)), errorFormat: output.ResolveErrorFormat(g.JSON, os.Stderr), g: g}
		}
		if op.Mutating {
			environment := cc.res.Environment
			if op.Realm == client.NativeBookingVisitor {
				environment = "visitor"
			}
			if err := safety.RequireLive(environment, g.Live); err != nil {
				return err
			}
		}
		response, err := cc.client.DoNative(cmd.Context(), client.NativeRequest{Realm: op.Realm, Method: op.Method, Path: path, Body: body, OriginalKey: key, Headers: headers})
		if err != nil {
			return reportNativeOwnerError(cmd, cc, op, err)
		}
		if err := validateNativeOwnerResponse(op, response, input, args); err != nil {
			return reportNativeOwnerError(cmd, cc, op, err)
		}
		if op.Kind == "sequence-control" {
			return nil
		}
		result := response.Body
		if op.Realm == client.NativeBookingVisitor {
			value, _ := nativeOriginalJSON(result)
			if secret != nil {
				if token := nativeManageToken(value); token != "" {
					if _, err := secret.WriteString(token); err != nil {
						return apierr.Usagef("el resultado se confirmó pero no se pudo guardar el token; no reenvíes la escritura")
					}
				}
			}
			result, _ = json.Marshal(redactNativeBooking(value))
		}
		return output.PrintBody(cmd.OutOrStdout(), result, cc.format)
	}
	return cmd
}

func validateNativeOriginalKey(op nativeOwnerOp, key string) error {
	max := 200
	minimumByte := byte(0)
	if strings.HasPrefix(op.Operation, "automation.") {
		max = 512
		minimumByte = 32
	}
	if key == "" || len(key) > max {
		return apierr.Usagef("falta la clave ORIGINAL válida; no se genera ni se reenvía una intención")
	}
	if op.Realm == client.NativeBookingVisitor {
		if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(key) {
			return apierr.Usagef("la clave ORIGINAL pública debe tener 64 caracteres hexadecimales")
		}
		return nil
	}
	if strings.Trim(key, " ") != key {
		return apierr.Usagef("la clave ORIGINAL no puede representarse sin cambios como cabecera HTTP")
	}
	for i := range len(key) {
		if key[i] < 32 || key[i] == 127 || (minimumByte == 32 && key[i] == 32) {
			return apierr.Usagef("la clave ORIGINAL contiene caracteres no permitidos")
		}
	}
	return nil
}

// Token parsing rejects duplicate member names (including escaped aliases)
// while the separate wire byte slice remains completely unchanged.
func nativeOriginalJSON(body []byte) (any, error) {
	if !utf8.Valid(body) {
		return nil, fmt.Errorf("UTF-8 inválido")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var parse func(int) (any, error)
	parse = func(depth int) (any, error) {
		if depth > 32 {
			return nil, fmt.Errorf("JSON demasiado profundo")
		}
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		delimiter, is := t.(json.Delim)
		if !is {
			return t, nil
		}
		switch delimiter {
		case '{':
			obj := map[string]any{}
			for d.More() {
				member, e := d.Token()
				if e != nil {
					return nil, e
				}
				name, ok := member.(string)
				if !ok {
					return nil, fmt.Errorf("miembro inválido")
				}
				if _, seen := obj[name]; seen {
					return nil, fmt.Errorf("miembro duplicado")
				}
				v, e := parse(depth + 1)
				if e != nil {
					return nil, e
				}
				obj[name] = v
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return nil, fmt.Errorf("objeto inválido")
			}
			return obj, nil
		case '[':
			array := []any{}
			for d.More() {
				v, e := parse(depth + 1)
				if e != nil {
					return nil, e
				}
				array = append(array, v)
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return nil, fmt.Errorf("lista inválida")
			}
			return array, nil
		default:
			return nil, fmt.Errorf("JSON inválido")
		}
	}
	value, err := parse(0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("se requiere un único JSON")
	}
	return value, nil
}

func validateNativeOwnerResponse(op nativeOwnerOp, r *client.Response, input any, args []string) error {
	invalid := func() error {
		if op.Mutating {
			return &apierr.TransportError{Err: fmt.Errorf("resultado desconocido: el acuse no cumple el contrato nativo; no reenvíes la escritura")}
		}
		return &apierr.APIError{StatusCode: 502, Type: "api_error", Code: "invalid_native_response", Message: "La respuesta no cumple el contrato nativo."}
	}
	if op.Kind == "sequence-control" {
		if r.StatusCode != 204 || len(r.Body) != 0 {
			return invalid()
		}
		return nil
	}
	status := op.Status
	if status == 0 {
		status = 200
	}
	value, err := nativeOriginalJSON(r.Body)
	if r.StatusCode != status || err != nil || nativeOwnerSchema(op.Response, value) != nil {
		return invalid()
	}
	envelope, _ := value.(map[string]any)
	data, _ := envelope["data"].(map[string]any)
	if data == nil {
		return nil
	}
	request, _ := input.(map[string]any)
	if op.Kind == "sequence-save" || op.Kind == "booking-save" {
		if len(args) > 0 && data["id"] != args[0] {
			return invalid()
		}
		if op.Mutating || op.Recovery {
			version, ok := knowledgeInt(data["version"])
			expected, eok := knowledgeInt(request["expected_version"])
			if op.Kind == "booking-save" && request["expected_version"] == nil {
				expected, eok = 0, true
			}
			if !ok || !eok || version != expected+1 {
				return invalid()
			}
		}
	}
	if op.Kind == "sequence-enrol" {
		if len(args) != 1 || data["sequence_id"] != args[0] || data["definition_version"] != request["expected_definition_version"] {
			return invalid()
		}
	}
	if op.Kind == "booking-get" && (len(args) != 1 || data["id"] != args[0]) {
		return invalid()
	}
	if op.Kind == "sequence-preview" && (len(args) != 1 || data["sequence_id"] != args[0] || data["subject_id"] != request["subject_id"]) {
		return invalid()
	}
	if op.Kind == "sequence-list" {
		filters, _ := request["filters"].(map[string]any)
		if data["resource"] != filters["resource"] {
			return invalid()
		}
		items, _ := data["items"].([]any)
		enrolments, _ := data["enrolments"].([]any)
		selected := items
		if data["resource"] == "enrolments" {
			selected = enrolments
		} else if len(enrolments) != 0 {
			return invalid()
		}
		if int64(len(selected)) > nativeInteger(data["visible_count"]) || int64(len(selected)) > nativeInteger(request["limit"]) {
			return invalid()
		}
		parents := map[any]bool{}
		for _, value := range items {
			item := value.(map[string]any)
			if parents[item["id"]] {
				return invalid()
			}
			parents[item["id"]] = true
		}
		seen := map[any]bool{}
		for _, value := range enrolments {
			item := value.(map[string]any)
			if seen[item["id"]] || !parents[item["sequence_id"]] {
				return invalid()
			}
			seen[item["id"]] = true
		}
	}
	if strings.HasPrefix(op.Kind, "public-") && op.Original {
		_, creating := request["slot"]
		_, rescheduling := request["new_slot"]
		if creating || rescheduling {
			if token, _ := data["manage_token"].(string); token == "" {
				return invalid()
			}
		} else {
			if _, exists := data["manage_token"]; exists {
				return invalid()
			}
		}
		version, ok := knowledgeInt(data["version"])
		if creating {
			// The native creation writes held V1 then confirms V2 in the same
			// original effect; accepting a guessed initial V1 is not an ACK.
			if !ok || version != 2 || data["status"] != "confirmed" {
				return invalid()
			}
		} else {
			expected, eok := knowledgeInt(request["expected_version"])
			if !ok || !eok || version != expected+1 {
				return invalid()
			}
			if rescheduling && data["status"] != "confirmed" {
				return invalid()
			}
			if !rescheduling && data["status"] != "cancelled" {
				return invalid()
			}
		}
		name := "slot"
		if rescheduling {
			name = "new_slot"
		}
		if creating || rescheduling {
			slot, _ := request[name].(map[string]any)
			if data["start_at"] != slot["start_at"] || data["end_at"] != slot["end_at"] {
				return invalid()
			}
		}
	}
	return nil
}

func reportNativeOwnerError(cmd *cobra.Command, cc *cliContext, op nativeOwnerOp, err error) error {
	var transport *apierr.TransportError
	var nativeError *apierr.APIError
	uncertain := errors.As(err, &transport)
	if op.Mutating && errors.As(err, &nativeError) && (nativeError.StatusCode >= 500 || nativeError.StatusCode == 408 || nativeError.StatusCode == 307 || nativeError.StatusCode == 308) {
		uncertain = true
		copy := *nativeError
		copy.Message += ". " + nativeUnknownInstruction(op)
		err = &copy
	}
	if errors.As(err, &transport) {
		message := "consulta nativa no confirmada"
		if op.Mutating {
			message = nativeUnknownInstruction(op)
		}
		err = &apierr.TransportError{Err: errors.New(message)}
	}
	if op.Realm == client.NativeBookingVisitor {
		var native *apierr.APIError
		if errors.As(err, &native) {
			safe := *native
			safe.Message = "La operación pública no pudo completarse."
			if op.Mutating && uncertain {
				safe.Message += " " + nativeUnknownInstruction(op)
			}
			safe.Data = nil
			safe.Details = nil
			safe.Errors = nil
			safe.DocURL = ""
			safe.Param = ""
			err = &safe
		}
	}
	output.PrintError(cmd.ErrOrStderr(), err, cc.errorFormat)
	return &AlreadyReported{Err: err}
}

func nativeUnknownInstruction(op nativeOwnerOp) string {
	if !op.Original {
		return "resultado desconocido; no reenvíes la escritura. Consulta el estado actual"
	}
	command := "factuarea crm bookings recover-service"
	if op.Realm == client.NativeBookingVisitor {
		command = "factuarea crm bookings public recover"
	} else if op.Kind == "sequence-save" {
		command = "factuarea crm sequences recover-save"
	} else if op.Kind == "sequence-enrol" {
		command = "factuarea crm sequences recover-enrol"
	}
	return "resultado desconocido; conserva clave y JSON originales y usa " + command + " explícitamente; no reenvíes la escritura"
}

func nativeManageToken(value any) string {
	object, _ := value.(map[string]any)
	data, _ := object["data"].(map[string]any)
	token, _ := data["manage_token"].(string)
	return token
}
func redactNativeBooking(value any) any {
	switch v := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for k, child := range v {
			switch k {
			case "manage_token", "captcha_token", "visitor_fields", "email", "phone", "original_wire_body":
				result[k] = "[redacted]"
			default:
				result[k] = redactNativeBooking(child)
			}
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, child := range v {
			result[i] = redactNativeBooking(child)
		}
		return result
	default:
		return value
	}
}

func nativeInteger(value any) int64 { n, _ := knowledgeInt(value); return n }
func nativeReadBoundedFile(path string, maximum int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, int64(maximum)+1))
}
