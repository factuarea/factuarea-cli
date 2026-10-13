package cmd

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

var crmServiceTicketActor = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var crmServiceTicketEnum = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func crmServiceTicketBodyFields(action string) ([]string, []string) {
	switch action {
	case "stage":
		return []string{"expected_version", "pipeline_id", "stage_id"}, []string{"waiting_reason", "customer_message_id"}
	case "attach":
		return []string{"expected_version", "document_id", "document_version"}, nil
	case "detach":
		return []string{"expected_version", "attachment_id"}, nil
	}
	return nil, nil
}

func validateCRMServiceTicketBody(action string, body []byte) (map[string]any, error) {
	if len(body) > 262144 {
		return nil, apierr.Usagef("el cuerpo del ticket supera 262144 bytes")
	}
	o, err := crmDocumentJSON(body)
	if err != nil {
		return nil, err
	}
	required, optional := crmServiceTicketBodyFields(action)
	if len(required) == 0 {
		return nil, apierr.Usagef("operación de ticket no admitida")
	}
	if _, err := crmDocumentRecord(o, required, optional); err != nil {
		return nil, err
	}
	for _, key := range required {
		if key == "expected_version" || key == "document_version" {
			if _, ok := crmDocumentInt(o[key], false); !ok {
				return nil, apierr.Usagef("%s debe ser un entero JSON positivo en rango nativo", key)
			}
		} else if !crmDocumentID(o[key], false) {
			return nil, apierr.Usagef("%s debe conservar su UUIDv7", key)
		}
	}
	if action == "stage" {
		reason, provided := o["waiting_reason"]
		if provided && reason != nil && reason != "waiting_customer" {
			return nil, apierr.Usagef("waiting_reason admite waiting_customer o null; omitir conserva la causa")
		}
		if message, exists := o["customer_message_id"]; exists && message != nil {
			if !crmDocumentID(message, false) || !provided || reason != nil {
				return nil, apierr.Usagef("customer_message_id requiere UUIDv7 original y waiting_reason:null para liberar explícitamente la espera")
			}
		}
	}
	return o, nil
}

func validateCRMServiceTicketReceipt(op crmServiceTicketOperation, ticket string, request, data map[string]any) error {
	if _, err := crmDocumentRecord(data, []string{"id", "version"}, nil); err != nil {
		return err
	}
	version, versionOK := crmDocumentInt(data["version"], false)
	expected, expectedOK := crmDocumentInt(request["expected_version"], false)
	if !versionOK || !expectedOK || !crmDocumentID(data["id"], false) || !strings.EqualFold(data["id"].(string), ticket) ||
		(version != expected+1 && !(op.action == "stage" && version == expected)) {
		return fmt.Errorf("recibo no corresponde al UUID/CAS original de la operación")
	}
	return nil
}

func crmServiceTicketEnumText(v any) bool {
	s, ok := v.(string)
	return ok && len(s) <= 255 && crmServiceTicketEnum.MatchString(s)
}

func validateCRMServiceTicket(v any) error {
	optional := []string{"title", "description", "status", "priority", "type", "creator_id", "pipeline_id", "stage_id", "counterparty_id", "person_id", "assignee_id",
		"created_at", "updated_at", "first_response_at", "resolved_at", "closed_at", "archived_at", "cycle", "comments", "attachments", "writable_fields", "authorization_version"}
	o, err := crmDocumentRecord(v, []string{"id", "version"}, optional)
	if err != nil {
		return err
	}
	_, versionOK := crmDocumentInt(o["version"], false)
	if !versionOK || !crmDocumentID(o["id"], false) {
		return fmt.Errorf("identidad/revisión de ticket inválida")
	}
	for key, value := range o {
		valid := true
		switch key {
		case "title":
			valid = crmDocumentText(value, 255, false)
		case "description":
			valid = crmDocumentText(value, 20000, true)
		case "status", "priority", "type":
			valid = crmServiceTicketEnumText(value)
		case "creator_id":
			s, ok := value.(string)
			valid = ok && crmServiceTicketActor.MatchString(s)
		case "assignee_id":
			s, ok := value.(string)
			valid = value == nil || (ok && crmServiceTicketActor.MatchString(s))
		case "pipeline_id", "stage_id", "counterparty_id", "person_id":
			valid = value == nil || crmDocumentID(value, false)
		case "created_at", "updated_at":
			valid = crmDocumentTimestamp(value)
		case "first_response_at", "resolved_at", "closed_at", "archived_at":
			valid = value == nil || crmDocumentTimestamp(value)
		case "cycle":
			_, valid = crmDocumentInt(value, false)
		case "authorization_version":
			valid = crmDocumentText(value, 4096, false)
		case "writable_fields":
			fields, ok := value.([]any)
			valid = ok
			seen := map[string]bool{}
			for _, field := range fields {
				s, ok := field.(string)
				if !ok || !crmServiceTicketEnumText(field) || seen[s] {
					valid = false
					break
				}
				seen[s] = true
			}
		case "comments", "attachments":
			items, ok := value.([]any)
			valid = ok
			for _, item := range items {
				if err := validateCRMServiceTicketChild(key, item); err != nil {
					return err
				}
			}
		}
		if !valid {
			return fmt.Errorf("campo de ticket %s incompatible", key)
		}
	}
	_, hasFields := o["writable_fields"]
	_, hasAuthorization := o["authorization_version"]
	if hasFields != hasAuthorization {
		return fmt.Errorf("writable_fields exige authorization_version nativa")
	}
	return nil
}

func validateCRMServiceTicketChild(kind string, v any) error {
	required, optional := []string{"id", "document_id", "document_version"}, []string{"mime", "size_bytes", "created_by", "created_at"}
	if kind == "comments" {
		required, optional = []string{"id", "visibility"}, []string{"author_type", "author_id", "body", "body_format", "delivery_state", "conversation_message_id", "created_at", "edited_at"}
	}
	o, err := crmDocumentRecord(v, required, optional)
	if err != nil {
		return err
	}
	for key, value := range o {
		valid := true
		switch key {
		case "id", "document_id":
			valid = crmDocumentID(value, false)
		case "document_version":
			_, valid = crmDocumentInt(value, false)
		case "size_bytes":
			_, valid = crmDocumentInt(value, true)
		case "mime":
			s, ok := value.(string)
			valid = ok && len(s) <= 255 && crmDocumentMIME.MatchString(s)
		case "created_by", "author_id":
			s, ok := value.(string)
			valid = ok && crmServiceTicketActor.MatchString(s)
		case "created_at":
			valid = crmDocumentTimestamp(value)
		case "edited_at":
			valid = value == nil || crmDocumentTimestamp(value)
		case "conversation_message_id":
			valid = value == nil || crmDocumentID(value, false)
		case "body":
			valid = crmDocumentText(value, 20000, false)
		case "visibility", "author_type", "body_format", "delivery_state":
			valid = crmServiceTicketEnumText(value)
		}
		if !valid {
			return fmt.Errorf("campo hijo de ticket %s incompatible", key)
		}
	}
	return nil
}

func validateCRMServiceTicketRead(op crmServiceTicketOperation, args []string, query url.Values, data map[string]any) error {
	if op.action == "get" {
		if err := validateCRMServiceTicket(data); err != nil {
			return err
		}
		if !strings.EqualFold(data["id"].(string), args[0]) {
			return fmt.Errorf("detalle de otro ticket")
		}
		return nil
	}
	required := []string{"items", "total", "page", "per_page"}
	if op.action == "timeline" {
		required = append(required, "id")
	}
	if _, err := crmDocumentRecord(data, required, nil); err != nil {
		return err
	}
	page, pageOK := crmDocumentInt(data["page"], false)
	perPage, perOK := crmDocumentInt(data["per_page"], false)
	total, totalOK := crmDocumentInt(data["total"], true)
	items, itemsOK := data["items"].([]any)
	if !pageOK || !perOK || !totalOK || !itemsOK || page > 1000000 || perPage > 100 || int64(len(items)) > perPage || int64(len(items)) > total ||
		strconv.FormatInt(page, 10) != query.Get("page") || strconv.FormatInt(perPage, 10) != query.Get("per_page") {
		return fmt.Errorf("página de ticket distinta de la consulta original")
	}
	if op.action == "timeline" && (!crmDocumentID(data["id"], false) || !strings.EqualFold(data["id"].(string), args[0])) {
		return fmt.Errorf("timeline de otro ticket")
	}
	for _, item := range items {
		if op.action == "list" {
			if err := validateCRMServiceTicket(item); err != nil {
				return err
			}
			continue
		}
		o, err := crmDocumentRecord(item, []string{"version"}, []string{"action", "actor_id", "occurred_at", "snapshot"})
		if err != nil {
			return err
		}
		version, ok := crmDocumentInt(o["version"], false)
		if !ok {
			return fmt.Errorf("versión histórica inválida")
		}
		for key, value := range o {
			valid := true
			switch key {
			case "action":
				valid = crmServiceTicketEnumText(value)
			case "actor_id":
				s, ok := value.(string)
				valid = ok && crmServiceTicketActor.MatchString(s)
			case "occurred_at":
				valid = crmDocumentTimestamp(value)
			case "snapshot":
				if err := validateCRMServiceTicket(value); err != nil {
					return err
				}
				snapshot := value.(map[string]any)
				snapshotVersion, _ := crmDocumentInt(snapshot["version"], false)
				valid = strings.EqualFold(snapshot["id"].(string), args[0]) && snapshotVersion == version
			}
			if !valid {
				return fmt.Errorf("snapshot/metadata histórica no corresponde al ticket y revisión originales")
			}
		}
	}
	return nil
}
