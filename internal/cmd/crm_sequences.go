package cmd

import (
	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"sort"
)

func nativeSequenceDefinition() nativeSchema {
	window := nObject(nativeSchema{"weekday": nRange(1, 7), "start_minute": nRange(0, 1439), "end_minute": nRange(1, 1440)}, "weekday", "start_minute", "end_minute")
	wait := nObject(nativeSchema{"type": nEnum("wait"), "wait_seconds": nInt(1), "recipe_id": nativeSchema{"type": "null"}}, "type", "wait_seconds", "recipe_id")
	action := nObject(nativeSchema{"type": nEnum("create_activity", "draft", "send"), "wait_seconds": nativeSchema{"type": "null"}, "recipe_id": nUUID()}, "type", "wait_seconds", "recipe_id")
	windows := nArray(window)
	windows["minItems"] = 1
	steps := nArray(nativeSchema{"oneOf": []any{wait, action}})
	steps["minItems"] = 1
	stops := nArray(nEnum("booking_confirmed", "hard_bounce", "complaint", "contactability_revoked"))
	stops["uniqueItems"] = true
	return nObject(nativeSchema{"timezone": nText(1, 100), "ambiguous_offset": nEnum("earlier", "later"), "allowed_windows": windows, "steps": steps, "stop_on_closure": nativeSchema{"type": "boolean"}, "purpose": nativeSchema{"type": "string", "pattern": `^[a-z][a-z0-9_]{0,63}$`}, "channel": nEnum("email", "whatsapp"), "stop_conditions": stops}, "timezone", "ambiguous_offset", "allowed_windows", "steps", "stop_on_closure", "purpose", "channel")
}

func nativeSequenceSchedule() nativeSchema {
	state := nEnum("draft", "active", "paused", "retired")
	fields := nObject(nativeSchema{"definition": nText(1, 65536), "state": state, "owner_id": nHost(), "team_id": nNullable(nUUID()), "next_due_at": nNullable(nText(1, 40)), "created_at": nText(1, 40), "updated_at": nText(1, 40)})
	return nObject(nativeSchema{"id": nUUID(), "version": nInt(1), "authorization_version": nText(1, 512), "sandbox": nativeSchema{"type": "boolean"}, "state": state, "owner_id": nHost(), "team_id": nNullable(nUUID()), "fields": fields}, "id", "version", "authorization_version", "sandbox", "state", "owner_id", "team_id", "fields")
}

func nativeSequenceEnrolment(metadata bool) nativeSchema {
	fields := nativeSchema{"id": nUUID(), "version": nInt(1), "authorization_version": nText(1, 512), "sandbox": nativeSchema{"type": "boolean"}, "sequence_id": nUUID(), "generation": nInt(1), "definition_version": nInt(1), "state": nEnum("active", "waiting", "paused", "completed", "cancelled", "blocked"), "current_step": nInt(0), "stop_reason": nativeSequenceStopReason()}
	if metadata {
		fields["owner_id"] = nHost()
		fields["team_id"] = nNullable(nUUID())
		fields["next_due_at"] = nNullable(nText(1, 40))
		fields["created_at"] = nText(1, 40)
		fields["updated_at"] = nText(1, 40)
	} else {
		fields["subject_type"] = nEnum("lead", "opportunity", "business_contact")
		fields["subject_id"] = nUUID()
		fields["due_at"] = nNullable(nText(1, 40))
		fields["fields"] = nObject(nativeSchema{"state": fields["state"], "next_due_at": nNullable(nText(1, 40)), "created_at": nText(1, 40)})
	}
	required := []string{}
	for name := range fields {
		required = append(required, name)
	}
	sort.Strings(required)
	return nObject(fields, required...)
}
func nativeSequenceStopReason() nativeSchema {
	return nNullable(nEnum("human_reply", "optout", "closure", "booking_confirmed", "owner_disabled", "revoked", "suppressed", "manual", "privacy", "unconfirmed"))
}
func nativeSequenceFilters() nativeSchema {
	return nObject(nativeSchema{"resource": nEnum("schedules", "enrolments"), "state": nEnum("draft", "active", "paused", "retired", "waiting", "completed", "cancelled", "blocked"), "owner_id": nHost(), "team_id": nNullable(nUUID()), "sequence_id": nUUID(), "definition_version": nInt(1), "stop_reason": nativeSequenceStopReason()}, "resource")
}
func validateSequenceFilters(value any) error {
	filters, _ := value.(map[string]any)
	if filters["resource"] == "schedules" {
		for _, name := range []string{"sequence_id", "definition_version", "stop_reason"} {
			if _, exists := filters[name]; exists {
				return apierr.Usagef("filtro de enrolments no válido para schedules")
			}
		}
		if state, exists := filters["state"]; exists {
			if knowledgeSchema(string(nRaw(nEnum("draft", "active", "paused", "retired"))), state) != nil {
				return apierr.Usagef("estado schedules inválido")
			}
		}
	} else if state, exists := filters["state"]; exists {
		if knowledgeSchema(string(nRaw(nEnum("active", "waiting", "paused", "completed", "cancelled", "blocked"))), state) != nil {
			return apierr.Usagef("estado enrolments inválido")
		}
	}
	return nil
}

func nativeSequenceOperations() []nativeOwnerOp {
	groups := []string{"crm", "sequences"}
	base := "/v1/crm/crmsequence"
	schedule := nativeSequenceSchedule()
	enrol := nativeSequenceEnrolment(false)
	page := nEnvelope(nObject(nativeSchema{"items": nArray(schedule), "enrolments": nArray(nativeSequenceEnrolment(true)), "resource": nEnum("schedules", "enrolments"), "next_cursor": nNullable(nText(1, 8192)), "visible_count": nInt(0)}, "items", "enrolments", "resource", "next_cursor", "visible_count"))
	preview := nEnvelope(nObject(nativeSchema{"sequence_id": nUUID(), "definition_version": nInt(1), "subject_id": nUUID(), "timezone": nText(1, 100), "steps": nArray(nObject(nativeSchema{"step_index": nInt(0), "type": nEnum("wait", "create_activity", "draft", "send"), "due_at": nText(1, 40), "recipe_id": nNullable(nUUID())}, "step_index", "type", "due_at", "recipe_id"))}, "sequence_id", "definition_version", "subject_id", "timezone", "steps"))
	newSave := nObject(nativeSchema{"expected_version": nativeSchema{"type": "integer", "const": 0}, "definition": nativeSequenceDefinition()}, "expected_version", "definition")
	update := nObject(nativeSchema{"expected_version": nInt(1), "definition": nativeSequenceDefinition()}, "expected_version", "definition")
	operations := []nativeOwnerOp{
		{Groups: groups, Action: "list", OperationID: "v1.crm.sequences.list", Operation: "automation.crm_sequence_list", Summary: "Lista schedules o enrolments con filtros y límite explícitos", Method: "GET", Path: base, Scope: "automation:read", Response: page, Kind: "sequence-list", Query: []nativeQuery{{"filters", nativeSequenceFilters(), true, true}, {"limit", nRange(1, 100), true, false}, {"cursor", nativeSchema{"type": "string", "minLength": 1, "maxLength": 8192, "pattern": `^[A-Za-z0-9+/=_-]+$`}, false, false}}},
		{Groups: groups, Action: "preview", OperationID: "v1.crm.sequences.preview", Operation: "automation.crm_sequence_preview", Summary: "Previsualiza el calendario sin reservar ni ejecutar efectos", Method: "GET", Path: base + "/{sequence}/preview", Args: []string{"sequence"}, Scope: "automation:read", Response: preview, Kind: "sequence-preview", Query: []nativeQuery{{"subject_type", nEnum("lead", "opportunity", "business_contact"), true, false}, {"subject_id", nUUID(), true, false}, {"start_at", nText(1, 40), true, false}}},
		{Groups: groups, Action: "create", OperationID: "v1.crm.sequences.save_new", Operation: "automation.crm_sequence_save", Summary: "Guarda una secuencia nueva con expected_version 0 y clave original", Method: "POST", Path: base, Scope: "automation:write", Mutating: true, Original: true, Confirm: true, Body: newSave, Response: nEnvelope(schedule), Kind: "sequence-save"},
		{Groups: groups, Action: "save", OperationID: "v1.crm.sequences.save_existing", Operation: "automation.crm_sequence_save", Summary: "Guarda la versión CAS de una secuencia existente", Method: "POST", Path: base + "/{sequence}", Args: []string{"sequence"}, Scope: "automation:write", Mutating: true, Original: true, Confirm: true, Body: update, Response: nEnvelope(schedule), Kind: "sequence-save"},
		{Groups: groups, Action: "enrol", OperationID: "v1.crm.sequences.enrol", Operation: "automation.crm_sequence_enrol", Summary: "Inscribe un sujeto con la versión explícita de la definición", Method: "POST", Path: base + "/{sequence}/enrolments", Args: []string{"sequence"}, Scope: "automation:write", Mutating: true, Original: true, Confirm: true, Body: nObject(nativeSchema{"subject_type": nEnum("lead", "opportunity", "business_contact"), "subject_id": nUUID(), "expected_definition_version": nInt(1)}, "subject_type", "subject_id", "expected_definition_version"), Response: nEnvelope(enrol), Kind: "sequence-enrol"},
		{Groups: groups, Action: "recover-save", OperationID: "v1.crm.sequences.original_save", Operation: "automation.crm_sequence_save_original", Summary: "Consulta el guardado ORIGINAL sólo por su clave, sin reenvío", Method: "GET", Path: base + "/original", Scope: "automation:write", Original: true, Response: nEnvelope(schedule), Kind: "sequence-original-save"},
		{Groups: groups, Action: "recover-enrol", OperationID: "v1.crm.sequences.original_enrol", Operation: "automation.crm_sequence_enrol_original", Summary: "Consulta la inscripción ORIGINAL sólo por su clave, sin reenvío", Method: "GET", Path: base + "/enrolments/original", Scope: "automation:write", Original: true, Response: nEnvelope(enrol), Kind: "sequence-original-enrol"},
	}
	for _, action := range []string{"pause", "resume", "cancel"} {
		properties := nativeSchema{"expected_version": nInt(1)}
		required := []string{"expected_version"}
		if action == "pause" {
			properties["reason"] = nEnum("manual")
			required = append(required, "reason")
		}
		operations = append(operations, nativeOwnerOp{Groups: groups, Action: action, OperationID: "v1.crm.sequences." + action, Operation: "automation.crm_sequence_" + action, Summary: "Control CAS nativo: HTTP 204 vacío; resultado ambiguo requiere releer, sin recuperación original", Method: "PATCH", Path: base + "/enrolments/{enrolment}/" + action, Args: []string{"enrolment"}, Scope: "automation:write", Mutating: true, Confirm: true, Body: nObject(properties, required...), Kind: "sequence-control"})
	}
	for i := range operations {
		operations[i].Realm = client.NativeStaff
	}
	return operations
}
