package cmd

import (
	"encoding/json"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
)

const mailboxWriteOriginalPath = "/v1/crm/conversation/mailboxes"
const mailboxWriteOriginalOperation = "conversations.configure_mailbox"
const mailboxWriteOriginalScope = "crm:write"
const mailboxOriginalMaxInteger = int64(9223372036854775807)

type mailboxWriteOriginalOp struct {
	Action, ID, Path string
	Update           bool
}

func mailboxWriteOriginalOperations() []mailboxWriteOriginalOp {
	return []mailboxWriteOriginalOp{
		{"recover-create", "getCreatedCrmMailboxWriteOriginal", mailboxWriteOriginalPath + "/write-original", false},
		{"recover-update", "getUpdatedCrmMailboxWriteOriginal", mailboxWriteOriginalPath + "/{mailbox}/write-original", true},
	}
}

func mailboxOriginalUUID(user bool) nativeSchema {
	version := "7[0-9a-f]{3}-[89ab][0-9a-f]{3}"
	if user {
		// The native historical UserUuid accepts the owner's UUID contract,
		// independently of the UUIDv7 mailbox/connection identity.
		version = "[0-9a-f]{4}-[0-9a-f]{4}"
	}
	return nativeSchema{"type": "string", "format": "uuid", "pattern": `(?i)^[0-9a-f]{8}-[0-9a-f]{4}-` + version + `-[0-9a-f]{12}$`}
}

func mailboxOriginalWireSchema(update bool) nativeSchema {
	expected := nativeSchema{"type": "integer", "minimum": 0, "maximum": mailboxOriginalMaxInteger}
	connection := nNullable(mailboxOriginalUUID(false))
	if update {
		expected["minimum"] = 1
		connection = mailboxOriginalUUID(false)
	} else {
		expected["const"] = 0
	}
	users := nArray(mailboxOriginalUUID(true))
	users["maxItems"] = 1000
	users["uniqueItems"] = true
	schema := nObject(nativeSchema{
		"expected_version": expected, "provider": nEnum("gmail", "microsoft_graph"),
		"connection_id": connection, "kind": nEnum("personal", "shared"),
		"team_id": nNullable(mailboxOriginalUUID(false)), "allowed_user_ids": users,
	}, "expected_version", "provider", "connection_id", "kind", "team_id", "allowed_user_ids")
	schema["allOf"] = []any{nativeSchema{
		"if":   nativeSchema{"properties": nativeSchema{"kind": nativeSchema{"const": "personal"}}, "required": []string{"kind"}},
		"then": nativeSchema{"properties": nativeSchema{"team_id": nativeSchema{"type": "null"}}},
	}}
	return schema
}

func mailboxOriginalReceiptSchema() nativeSchema {
	return nEnvelope(nObject(nativeSchema{
		"id": mailboxOriginalUUID(false), "effect_id": mailboxOriginalUUID(false), "connection_id": mailboxOriginalUUID(false),
		"version":      nativeSchema{"type": "integer", "minimum": 1, "maximum": mailboxOriginalMaxInteger},
		"availability": nEnum("active", "disconnected"), "provider": nEnum("gmail", "microsoft_graph"),
		"confirmed_at": nativeSchema{"type": "string", "format": "date-time", "pattern": `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`},
	}, "id", "effect_id", "connection_id", "version", "availability", "confirmed_at", "provider"))
}

func mailboxWriteOriginalManifest() []manifestEntry {
	result := make([]manifestEntry, 0, 2)
	for _, op := range mailboxWriteOriginalOperations() {
		args := []string{}
		if op.Update {
			args = append(args, "mailbox")
		}
		result = append(result, manifestEntry{
			Command: "factuarea crm mailboxes " + op.Action, OperationID: op.ID, CrmOperation: mailboxWriteOriginalOperation,
			Summary: mailboxWriteOriginalSummary(op), Args: args, Method: "GET", NativePath: op.Path,
			AuthRealm: string(client.NativeStaff), OperationIDSource: "native_v1_surface",
			RequiredScope: mailboxWriteOriginalScope, RequiredScopes: []string{mailboxWriteOriginalScope},
			IdempotencyRequired: true, OriginalRecovery: true, OriginalBodySchema: nRaw(mailboxOriginalWireSchema(op.Update)),
			Flags:           []flagInfo{{"idempotency-key", "string"}, {"idempotency-key-file", "string"}, {"original-wire", "string"}, {"original-wire-file", "string"}},
			QuerySchemas:    map[string]json.RawMessage{"original_wire": nRaw(nText(1, 65536))},
			ResponseSchemas: map[string]json.RawMessage{"200": nRaw(mailboxOriginalReceiptSchema())},
		})
	}
	return result
}

func mailboxWriteOriginalSummary(op mailboxWriteOriginalOp) string {
	if op.Update {
		return "Recupera el recibo original de actualización del buzón con crm:write"
	}
	return "Recupera el recibo original de alta del buzón con crm:write"
}

func registerMailboxWriteOriginalCommands(root *cobra.Command) {
	for _, child := range root.Commands() {
		if child.Name() == "crm" {
			group := &cobra.Command{Use: "mailboxes", Short: "Recuperación de escrituras originales de buzones CRM", Args: groupArgs}
			for _, op := range mailboxWriteOriginalOperations() {
				group.AddCommand(newMailboxWriteOriginalCommand(op))
			}
			child.AddCommand(group)
			return
		}
	}
	panic("native CRM registration required")
}

func newMailboxWriteOriginalCommand(op mailboxWriteOriginalOp) *cobra.Command {
	use := op.Action
	arguments := 0
	if op.Update {
		use += " <mailbox>"
		arguments = 1
	}
	cmd := &cobra.Command{Use: use, Short: mailboxWriteOriginalSummary(op), Args: UsageArgs(cobra.ExactArgs(arguments)),
		Long: mailboxWriteOriginalSummary(op) + ".\nGET nativo con la misma clave y los bytes JSON originales exactos, incluidos orden, espacios y CAS. --original-wire - lee stdin. No genera claves ni repite escrituras. El servidor conserva autoridad, máscaras y primer recibo; un error o ausencia no confirma el efecto. La credencial existente fija empresa y entorno; crm:write es obligatorio incluso para esta lectura."}
	var key, keyFile, wire, wireFile string
	f := cmd.Flags()
	f.StringVar(&key, "idempotency-key", "", "clave ORIGINAL exacta; obligatoria, no se recorta ni genera")
	f.StringVar(&keyFile, "idempotency-key-file", "", "fichero con la clave original exacta, sin salto añadido")
	f.StringVar(&wire, "original-wire", "", "JSON ORIGINAL exacto o - para stdin; se envía sólo como query original_wire")
	f.StringVar(&wireFile, "original-wire-file", "", "fichero que conserva los bytes JSON originales")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		path := op.Path
		if op.Update {
			if nativeOwnerSchema(mailboxOriginalUUID(false), args[0]) != nil {
				return apierr.Usagef("el destino original del buzón debe ser un UUIDv7")
			}
			path = strings.Replace(path, "{mailbox}", url.PathEscape(args[0]), 1)
		}
		if f.Changed("idempotency-key") == f.Changed("idempotency-key-file") {
			return apierr.Usagef("usa una sola fuente explícita para la clave original")
		}
		if f.Changed("idempotency-key-file") {
			body, err := nativeReadBoundedFile(keyFile, 512)
			if err != nil {
				return apierr.Usagef("no se pudo leer la clave original")
			}
			key = string(body)
		}
		if key == "" || len(key) > 512 {
			return apierr.Usagef("la clave original debe contener de 1 a 512 bytes")
		}
		for i := range len(key) {
			if key[i] <= 32 || key[i] == 127 {
				return apierr.Usagef("la clave original contiene espacios o controles no admitidos")
			}
		}
		if f.Changed("original-wire") == f.Changed("original-wire-file") {
			return apierr.Usagef("usa una sola fuente explícita para el JSON original")
		}
		body := []byte(wire)
		var err error
		if f.Changed("original-wire-file") {
			body, err = nativeReadBoundedFile(wireFile, 65536)
		} else if wire == "-" {
			body, err = io.ReadAll(io.LimitReader(cmd.InOrStdin(), 65537))
		}
		if err != nil {
			return apierr.Usagef("no se pudo leer el JSON original")
		}
		input, err := nativeOriginalJSON(body)
		if err != nil || len(body) == 0 || len(body) > 65536 || nativeOwnerSchema(mailboxOriginalWireSchema(op.Update), input) != nil {
			return apierr.Usagef("el JSON original no cumple el contrato cerrado del buzón")
		}
		intent := input.(map[string]any)
		if intent["kind"] == "personal" && intent["team_id"] != nil {
			return apierr.Usagef("el JSON original personal exige team_id null")
		}
		seen := map[string]bool{}
		for _, user := range intent["allowed_user_ids"].([]any) {
			id := strings.ToLower(user.(string))
			if seen[id] {
				return apierr.Usagef("el JSON original contiene usuarios duplicados")
			}
			seen[id] = true
		}
		cc, err := newCLIContext(globalsFrom(cmd), "")
		if err != nil {
			return err
		}
		scopes, err := cc.scopes(cmd.Context())
		if err != nil {
			return reportNativeOwnerError(cmd, cc, nativeOwnerOp{Realm: client.NativeStaff}, err)
		}
		if !safety.HasScope(scopes, mailboxWriteOriginalScope) {
			return apierr.Permf("falta scope nativo %s", mailboxWriteOriginalScope)
		}
		// Encode the retained bytes exactly once. There is no request body,
		// regenerated intent, mailbox head lookup, or automatic replay.
		path += "?" + url.Values{"original_wire": []string{string(body)}}.Encode()
		response, err := cc.client.DoNative(cmd.Context(), client.NativeRequest{Realm: client.NativeStaff,
			Method: "GET", Path: path, OriginalKey: key})
		if err == nil {
			err = validateMailboxWriteOriginalResponse(op, response, intent, args)
		}
		if err != nil {
			return reportNativeOwnerError(cmd, cc, nativeOwnerOp{Realm: client.NativeStaff}, err)
		}
		return output.PrintBody(cmd.OutOrStdout(), response.Body, cc.format)
	}
	return cmd
}

func validateMailboxWriteOriginalResponse(op mailboxWriteOriginalOp, response *client.Response, intent map[string]any, args []string) error {
	invalid := func() error {
		return &apierr.APIError{StatusCode: 502, Type: "api_error", Code: "invalid_native_response",
			Message: "El recibo original no coincide con la intención del buzón; no confirma la escritura."}
	}
	if response == nil || response.StatusCode != 200 {
		return invalid()
	}
	value, err := nativeOriginalJSON(response.Body)
	if err != nil || nativeOwnerSchema(mailboxOriginalReceiptSchema(), value) != nil {
		return invalid()
	}
	data := value.(map[string]any)["data"].(map[string]any)
	version, valid := knowledgeInt(data["version"])
	expected, expectedValid := knowledgeInt(intent["expected_version"])
	if !valid || !expectedValid || expected == mailboxOriginalMaxInteger || version != expected+1 || data["provider"] != intent["provider"] {
		return invalid()
	}
	if (op.Update && (len(args) != 1 || data["id"] != args[0])) || (intent["connection_id"] != nil && data["connection_id"] != intent["connection_id"]) {
		return invalid()
	}
	if _, err := time.Parse("2006-01-02T15:04:05Z", data["confirmed_at"].(string)); err != nil {
		return invalid()
	}
	return nil
}
