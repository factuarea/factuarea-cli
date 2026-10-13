package cmd

import (
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
)

const conversationReadPath = "/v1/crm/conversation"

var conversationReadScopes = []string{"conversations:read", "crm:read"}
var conversationReadFields = []string{"uuid", "version", "mailbox_uuid", "status", "subject", "owner_uuid", "team_uuid", "last_message_at", "links", "messages", "sender", "recipients", "body", "delivery_status", "attachments"}

type conversationReadOp struct {
	Action, ID, Operation string
	Detail                bool
}

func conversationReadOperations() []conversationReadOp {
	return []conversationReadOp{{"list", "listCrmConversations", "conversations.list", false}, {"get", "getCrmConversation", "conversations.get", true}}
}
func conversationReadID(user bool) nativeSchema { return mailboxOriginalUUID(user) }
func conversationReadInteger(min int) nativeSchema {
	return nativeSchema{"type": "integer", "minimum": min, "maximum": int64(9223372036854775807)}
}
func conversationReadInstant() nativeSchema {
	return nNullable(nativeSchema{"type": "string", "pattern": `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`})
}
func conversationReadHead() nativeSchema {
	link := nObject(nativeSchema{"type": nEnum("contact", "person", "lead", "opportunity", "quote", "invoice", "proposal", "activity"), "id": conversationReadID(false), "state": nEnum("automatic_suggestion", "confirmed")}, "type", "id", "state")
	return nObject(nativeSchema{"id": conversationReadID(false), "version": conversationReadInteger(1), "mailbox_id": conversationReadID(false), "subject": nNullable(nText(0, 998)), "status": nEnum("active", "archived"), "owner_id": nNullable(conversationReadID(true)), "team_id": nNullable(conversationReadID(false)), "last_message_at": conversationReadInstant(), "links": nArray(link)}, "id", "version", "mailbox_id", "subject", "status", "owner_id", "team_id", "last_message_at", "links")
}
func conversationReadMessage() nativeSchema {
	participant := nObject(nativeSchema{"email": nText(1, 254), "name": nNullable(nText(0, 255))}, "email", "name")
	recipient := nObject(nativeSchema{"email": nText(1, 254), "name": nNullable(nText(0, 255)), "kind": nEnum("to", "cc", "bcc")}, "email", "name", "kind")
	recipients := nArray(recipient)
	recipients["minItems"] = 1
	recipients["maxItems"] = 100
	return nObject(nativeSchema{"id": conversationReadID(false), "conversation_id": conversationReadID(false), "version": conversationReadInteger(1), "direction": nEnum("inbound", "outbound"), "sender": nNullable(participant), "recipients": nNullable(recipients), "subject": nNullable(nText(0, 998)), "body": nNullable(nText(0, 1048576)), "body_format": nEnum("plain_text"), "composition_state": nEnum("draft", "send_intent", "received"), "delivery_status": nNullable(nEnum("pending", "confirmed", "unconfirmed", "failed")), "submitted_at": conversationReadInstant()}, "id", "conversation_id", "version", "direction", "sender", "recipients", "subject", "body", "body_format", "composition_state", "delivery_status", "submitted_at")
}
func conversationReadMeta() nativeSchema {
	fields := func(allowed ...string) nativeSchema {
		s := nArray(nEnum(allowed...))
		s["uniqueItems"] = true
		return s
	}
	return nObject(nativeSchema{"authorization_version": nativeSchema{"type": "string", "minLength": 1}, "readable_fields": fields(conversationReadFields...), "filterable_fields": fields("mailbox_uuid", "status", "subject", "links"), "sortable_fields": fields("uuid", "subject", "last_message_at")}, "authorization_version", "readable_fields", "filterable_fields", "sortable_fields")
}
func conversationReadSchema(detail bool) nativeSchema {
	data := nObject(nativeSchema{"items": nArray(conversationReadHead()), "next_cursor": nNullable(nText(1, 8192)), "total": conversationReadInteger(0), "active_count": conversationReadInteger(0), "archived_count": conversationReadInteger(0), "limit": nRange(1, 100)}, "items", "next_cursor", "total", "active_count", "archived_count", "limit")
	if detail {
		attachment := nObject(nativeSchema{"id": conversationReadID(false), "conversation_id": conversationReadID(false), "message_id": conversationReadID(false), "version": conversationReadInteger(1), "filename": nText(1, 255), "mime": nText(1, 255), "size": conversationReadInteger(1), "status": nEnum("quarantine", "ready", "rejected")}, "id", "conversation_id", "message_id", "version", "filename", "mime", "size", "status")
		data = nObject(nativeSchema{"conversation": conversationReadHead(), "messages": nArray(conversationReadMessage()), "attachments": nArray(attachment), "next_cursor": nNullable(nText(1, 8192)), "limit": nRange(1, 100)}, "conversation", "messages", "attachments", "next_cursor", "limit")
	}
	return nObject(nativeSchema{"data": data, "meta": conversationReadMeta()}, "data", "meta")
}
func conversationReadManifest() []manifestEntry {
	var result []manifestEntry
	for _, op := range conversationReadOperations() {
		path := conversationReadPath
		args := []string{}
		items := "data.items"
		flags := []flagInfo{{"cursor", "string"}, {"limit", "string"}, {"all", "bool"}}
		query := map[string]json.RawMessage{"cursor": nRaw(nNullable(nText(1, 8192))), "limit": nRaw(nRange(1, 100))}
		if op.Detail {
			path += "/{conversation}"
			args = append(args, "conversation")
			items = "data.messages"
		} else {
			flags = append([]flagInfo{{"filters", "string"}}, flags...)
			query["filters"] = nRaw(nativeSchema{"type": "string", "maxLength": 1048576, "default": "{}"})
		}
		result = append(result, manifestEntry{Command: "factuarea crm conversations " + op.Action, OperationID: op.ID, CrmOperation: op.Operation, Summary: conversationReadSummary(op), Args: args, Flags: flags, Method: "GET", NativePath: path, AuthRealm: string(client.NativeStaff), OperationIDSource: "native_v1_surface", RequiredScope: conversationReadScopes[0], RequiredScopes: append([]string{}, conversationReadScopes...), Paginated: true, Pagination: &genPagination{Kind: "cursor", QueryParameter: "cursor", ItemsPath: items, CursorPath: "data.next_cursor", IdentityPath: "id"}, QuerySchemas: query, ResponseSchemas: map[string]json.RawMessage{"200": nRaw(conversationReadSchema(op.Detail))}})
	}
	return result
}
func conversationReadSummary(op conversationReadOp) string {
	if op.Detail {
		return "Consulta el hilo CRM paginado con máscaras y entrega originales"
	}
	return "Lista conversaciones CRM con filtros, conteos y cursor autorizados del dueño"
}
func registerConversationReadCommands(root *cobra.Command) {
	for _, parent := range root.Commands() {
		if parent.Name() == "crm" {
			group := &cobra.Command{Use: "conversations", Short: "Lecturas V1 nativas de conversaciones", Args: groupArgs}
			for _, op := range conversationReadOperations() {
				group.AddCommand(newConversationReadCommand(op))
			}
			parent.AddCommand(group)
			return
		}
	}
	panic("native CRM registration required")
}
func newConversationReadCommand(op conversationReadOp) *cobra.Command {
	use := op.Action
	argc := 0
	if op.Detail {
		use += " <conversation>"
		argc = 1
	}
	cmd := &cobra.Command{Use: use, Short: conversationReadSummary(op), Long: conversationReadSummary(op) + ".\nUna página por defecto; --all conserva los envelopes de cada página con meta/C7 actuales en un array JSON. Sólo sigue next_cursor nativo y retira stdout si cambia la autoridad o falla una página. La credencial vigente fija empresa/entorno; conversations:read y el guard crm:read/* son independientes. Unconfirmed no confirma entrega.", Args: UsageArgs(cobra.ExactArgs(argc))}
	f := cmd.Flags()
	if !op.Detail {
		f.String("filters", "", "objeto JSON literal nativo: mailbox_uuid,state,search,target_type+target_uuid,sort")
	}
	f.String("cursor", "", "cursor ORIGINAL del dueño para la misma selección, límite y canal")
	f.String("limit", "", "límite nativo decimal canónico 1..100; default servidor20")
	f.Bool("all", false, "lee todas las páginas y conserva su envelope/meta individual; no agrega counts")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		query := url.Values{}
		limit := int64(20)
		path := conversationReadPath
		target := ""
		if op.Detail {
			target = args[0]
			if nativeOwnerSchema(conversationReadID(false), target) != nil {
				return apierr.Usagef("conversation debe ser UUIDv7 original")
			}
			path += "/" + url.PathEscape(target)
		} else if f.Changed("filters") {
			filters, _ := f.GetString("filters")
			if !validConversationReadFilters(filters) {
				return apierr.Usagef("--filters no cumple el objeto JSON cerrado nativo")
			}
			query.Set("filters", filters)
		}
		if f.Changed("cursor") {
			cursor, _ := f.GetString("cursor")
			if !validConversationReadText(cursor, 8192, false) {
				return apierr.Usagef("--cursor original inválido")
			}
			query.Set("cursor", cursor)
		}
		if f.Changed("limit") {
			raw, _ := f.GetString("limit")
			n, ok := knowledgeInt(json.Number(raw))
			if !ok || n < 1 || n > 100 || raw == "" || raw[0] == '0' || strings.ContainsAny(raw, ".+-eE") {
				return apierr.Usagef("--limit debe ser decimal canónico 1..100")
			}
			limit = n
			query.Set("limit", raw)
		}
		all, _ := f.GetBool("all")
		cc, err := newCLIContext(globalsFrom(cmd), "")
		if err != nil {
			return err
		}
		scopes, err := cc.scopes(cmd.Context())
		if err != nil {
			return err
		}
		for _, scope := range conversationReadScopes {
			if !safety.HasScope(scopes, scope) {
				return apierr.Permf("falta scope nativo %s", scope)
			}
		}
		pages := []json.RawMessage{}
		seen := map[string]bool{}
		var previous map[string]any
		if cursor := query.Get("cursor"); cursor != "" {
			seen[cursor] = true
		}
		for {
			endpoint := path
			if len(query) > 0 {
				endpoint += "?" + query.Encode()
			}
			response, err := cc.client.DoNative(cmd.Context(), client.NativeRequest{Realm: client.NativeStaff, Method: "GET", Path: endpoint})
			if err != nil {
				return reportNativeOwnerError(cmd, cc, nativeOwnerOp{Realm: client.NativeStaff}, err)
			}
			envelope, err := decodeConversationReadResponse(op, response, target, limit)
			if err != nil {
				return reportNativeOwnerError(cmd, cc, nativeOwnerOp{Realm: client.NativeStaff}, err)
			}
			meta := envelope["meta"].(map[string]any)
			if previous != nil && !reflect.DeepEqual(previous, meta) {
				return reportNativeOwnerError(cmd, cc, nativeOwnerOp{Realm: client.NativeStaff}, invalidConversationReadResponse())
			}
			pages = append(pages, json.RawMessage(response.Body))
			if !all {
				return output.PrintBody(cmd.OutOrStdout(), response.Body, cc.format)
			}
			previous = meta
			next := envelope["data"].(map[string]any)["next_cursor"]
			if next == nil {
				break
			}
			cursor := next.(string)
			if seen[cursor] {
				return reportNativeOwnerError(cmd, cc, nativeOwnerOp{Realm: client.NativeStaff}, invalidConversationReadResponse())
			}
			seen[cursor] = true
			query.Set("cursor", cursor)
		}
		wire, err := json.Marshal(pages)
		if err != nil {
			return err
		}
		return output.PrintBody(cmd.OutOrStdout(), wire, cc.format)
	}
	return cmd
}
func invalidConversationReadResponse() error {
	return &apierr.APIError{StatusCode: 502, Type: "api_error", Code: "invalid_native_response", Message: "La respuesta de conversación no coincide con la página, autoridad y máscaras nativas."}
}
func validConversationReadText(value string, max int, empty bool) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > max || (!empty && strings.Trim(value, " \t\n\r\x00\x0b") == "") {
		return false
	}
	for _, c := range value {
		if c <= 8 || c == 11 || c == 12 || (c >= 14 && c <= 31) || c == 127 {
			return false
		}
	}
	return true
}
func validConversationReadFilters(raw string) bool {
	if len(raw) > 1048576 {
		return false
	}
	value, err := nativeOriginalJSON([]byte(raw))
	if err != nil {
		return false
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for key, raw := range obj {
		value, ok := raw.(string)
		if !ok {
			return false
		}
		switch key {
		case "mailbox_uuid", "target_uuid":
			if nativeOwnerSchema(conversationReadID(false), value) != nil {
				return false
			}
		case "state":
			if value != "active" && value != "archived" {
				return false
			}
		case "search":
			if !validConversationReadText(value, 255, false) {
				return false
			}
		case "target_type":
			if nativeOwnerSchema(nEnum("contact", "person", "lead", "opportunity", "quote", "invoice", "proposal", "activity"), value) != nil {
				return false
			}
		case "sort":
			if nativeOwnerSchema(nEnum("last_message_at:asc", "last_message_at:desc", "subject:asc", "subject:desc"), value) != nil {
				return false
			}
		default:
			return false
		}
	}
	_, kind := obj["target_type"]
	_, id := obj["target_uuid"]
	return kind == id
}
func conversationReadTime(value any) bool {
	if value == nil {
		return true
	}
	raw := value.(string)
	at, err := time.Parse("2006-01-02T15:04:05Z", raw)
	return err == nil && at.Format("2006-01-02T15:04:05Z") == raw
}
func decodeConversationReadResponse(op conversationReadOp, response *client.Response, target string, limit int64) (map[string]any, error) {
	if response == nil || response.StatusCode != 200 {
		return nil, invalidConversationReadResponse()
	}
	value, err := nativeOriginalJSON(response.Body)
	if err != nil || nativeOwnerSchema(conversationReadSchema(op.Detail), value) != nil {
		return nil, invalidConversationReadResponse()
	}
	envelope := value.(map[string]any)
	data := envelope["data"].(map[string]any)
	meta := envelope["meta"].(map[string]any)
	if strings.Trim(meta["authorization_version"].(string), " \t\n\r\x00\x0b") == "" || nativeInteger(data["limit"]) != limit {
		return nil, invalidConversationReadResponse()
	}
	readable := map[string]bool{}
	for _, raw := range meta["readable_fields"].([]any) {
		readable[raw.(string)] = true
	}
	for _, field := range []string{"uuid", "version", "mailbox_uuid", "status"} {
		if !readable[field] {
			return nil, invalidConversationReadResponse()
		}
	}
	head := func(item map[string]any) bool {
		for public, field := range map[string]string{"subject": "subject", "owner_id": "owner_uuid", "team_id": "team_uuid", "last_message_at": "last_message_at"} {
			if !readable[field] && item[public] != nil {
				return false
			}
		}
		links := item["links"].([]any)
		if !readable["links"] && len(links) != 0 {
			return false
		}
		return conversationReadTime(item["last_message_at"])
	}
	if !op.Detail {
		items := data["items"].([]any)
		total := nativeInteger(data["total"])
		active := nativeInteger(data["active_count"])
		if int64(len(items)) > limit || total < int64(len(items)) || active > total || nativeInteger(data["archived_count"]) != total-active || (data["next_cursor"] != nil && (len(items) == 0 || total <= int64(len(items)))) {
			return nil, invalidConversationReadResponse()
		}
		ids := map[string]bool{}
		for _, raw := range items {
			item := raw.(map[string]any)
			id := strings.ToLower(item["id"].(string))
			if ids[id] || !head(item) {
				return nil, invalidConversationReadResponse()
			}
			ids[id] = true
		}
	} else {
		item := data["conversation"].(map[string]any)
		if item["id"] != target || !head(item) {
			return nil, invalidConversationReadResponse()
		}
		messages := data["messages"].([]any)
		attachments := data["attachments"].([]any)
		if int64(len(messages)) > limit || (!readable["messages"] && (len(messages) != 0 || len(attachments) != 0 || data["next_cursor"] != nil)) || (!readable["attachments"] && len(attachments) != 0) || (data["next_cursor"] != nil && len(messages) == 0) {
			return nil, invalidConversationReadResponse()
		}
		ids := map[string]bool{}
		for _, raw := range messages {
			message := raw.(map[string]any)
			id := strings.ToLower(message["id"].(string))
			if ids[id] || message["conversation_id"] != target {
				return nil, invalidConversationReadResponse()
			}
			ids[id] = true
			for _, field := range []string{"sender", "recipients", "subject", "body", "delivery_status"} {
				if !readable[field] && message[field] != nil {
					return nil, invalidConversationReadResponse()
				}
			}
			if !readable["delivery_status"] && message["submitted_at"] != nil {
				return nil, invalidConversationReadResponse()
			}
			state := message["composition_state"]
			status := message["delivery_status"]
			submitted := message["submitted_at"]
			if (message["direction"] == "inbound") != (state == "received") || (state != "send_intent" && (status != nil || submitted != nil)) || (status == "confirmed" && submitted == nil) || ((status == nil || status == "pending" || status == "failed") && submitted != nil) || !conversationReadTime(submitted) {
				return nil, invalidConversationReadResponse()
			}
		}
		attachmentIDs := map[string]bool{}
		for _, raw := range attachments {
			attachment := raw.(map[string]any)
			id := strings.ToLower(attachment["id"].(string))
			if attachmentIDs[id] || attachment["conversation_id"] != target || !ids[strings.ToLower(attachment["message_id"].(string))] || strings.ContainsAny(attachment["filename"].(string), "/\\") {
				return nil, invalidConversationReadResponse()
			}
			attachmentIDs[id] = true
		}
	}
	if data["next_cursor"] != nil && !validConversationReadText(data["next_cursor"].(string), 8192, false) {
		return nil, invalidConversationReadResponse()
	}
	return envelope, nil
}
