package cmd

import (
	"net/mail"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
)

func nativeBookingSchedule() nativeSchema {
	window := nObject(nativeSchema{"start_minute": nRange(0, 1439), "end_minute": nRange(1, 1440)}, "start_minute", "end_minute")
	weekly := nObject(nativeSchema{"weekday": nRange(1, 7), "start_minute": nRange(0, 1439), "end_minute": nRange(1, 1440)}, "weekday", "start_minute", "end_minute")
	fields := nativeSchema{"timezone": nText(1, 100), "dst_policy": nEnum("reject_ambiguous", "both_offsets"), "windows": nArray(weekly), "exceptions": nArray(nObject(nativeSchema{"date": nativeSchema{"type": "string", "pattern": `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`}, "windows": nArray(window)}, "date", "windows")), "duration_minutes": nRange(1, 1440), "buffer_before": nRange(0, 1440), "buffer_after": nRange(0, 1440), "capacity": nRange(1, 10000), "minimum_notice_minutes": nRange(0, 525600), "booking_horizon_days": nRange(1, 366), "slot_step_minutes": nRange(1, 1440)}
	return nObject(fields, "timezone", "dst_policy", "windows", "exceptions", "duration_minutes", "buffer_before", "buffer_after", "capacity", "minimum_notice_minutes", "booking_horizon_days", "slot_step_minutes")
}
func nativeBookingDefinition() nativeSchema {
	sources := nArray(nText(1, 200))
	sources["minItems"] = 1
	sources["uniqueItems"] = true
	sources["contains"] = nativeSchema{"const": "booking"}
	return nObject(nativeSchema{"public_id": nText(1, 200), "state": nEnum("draft", "published", "paused"), "host_id": nHost(), "resource_id": nUUID(), "schedule": nativeBookingSchedule(), "required_sources": sources, "notice_version": nText(1, 200), "hold_minutes": nRange(1, 1440), "location": nNullable(nText(1, 1000))})
}
func nativeBookingService(sources bool) nativeSchema {
	definition := nativeBookingDefinition()["properties"].(nativeSchema)
	fields := nativeSchema{"id": nUUID(), "version": nInt(1), "publication_version": nInt(1)}
	for key, value := range definition {
		fields[key] = value
	}
	if sources {
		fields["source_statuses"] = nativeSchema{"type": "object", "additionalProperties": nObject(nativeSchema{"verified": nativeSchema{"type": "boolean"}, "revision": nativeSchema{"type": "string", "pattern": `^[a-f0-9]{64}$`}, "reason": nNullable(nText(0, 200))}, "verified", "revision", "reason")}
	}
	return nObject(fields, "id", "version")
}
func nativeBookingList(bookings bool) nativeSchema {
	item := nativeBookingService(true)
	if bookings {
		item = nObject(nativeSchema{"id": nUUID(), "version": nInt(1), "service_id": nUUID(), "host_id": nHost(), "resource_id": nUUID(), "status": nEnum("held", "confirmed", "cancelled", "expired"), "start_at": nText(1, 40), "end_at": nText(1, 40), "projection_status": nText(1, 100)}, "id", "version")
	}
	return nEnvelope(nObject(nativeSchema{"items": nArray(item), "total": nInt(0), "next_cursor": nNullable(nText(1, 4096))}, "items", "total", "next_cursor"))
}
func nativeBookingSlot() nativeSchema {
	utc := nativeSchema{"type": "string", "pattern": `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`}
	return nObject(nativeSchema{"start_at": utc, "end_at": utc}, "start_at", "end_at")
}
func nativeBookingPublicBody(operation string) nativeSchema {
	switch operation {
	case "booking.create_public":
		return nObject(nativeSchema{"slot": nativeBookingSlot(), "visitor_fields": nObject(nativeSchema{"name": nText(1, 600), "email": nText(1, 254), "phone": nNullable(nText(1, 100))}, "name", "email"), "notice_version": nText(1, 200), "captcha_token": nText(1, 4096)}, "slot", "visitor_fields", "notice_version", "captcha_token")
	case "booking.reschedule_public":
		return nObject(nativeSchema{"manage_token": nText(1, 128), "expected_version": nInt(1), "new_slot": nativeBookingSlot()}, "manage_token", "expected_version", "new_slot")
	case "booking.cancel_public":
		return nObject(nativeSchema{"manage_token": nText(1, 128), "expected_version": nInt(1)}, "manage_token", "expected_version")
	default:
		return nil
	}
}
func nativeBookingPublicReceipt() nativeSchema {
	return nEnvelope(nObject(nativeSchema{"id": nUUID(), "version": nInt(1), "status": nEnum("held", "confirmed", "cancelled", "expired"), "start_at": nText(1, 40), "end_at": nText(1, 40), "effect_id": nUUID(), "manage_token": nText(1, 128)}, "id", "version", "status", "start_at", "end_at", "effect_id"))
}
func nativeBookingOperations() []nativeOwnerOp {
	groups := []string{"crm", "bookings"}
	base := "/v1/crm/booking"
	pageQuery := []nativeQuery{{"cursor", nText(1, 4096), false, false}, {"limit", nRange(1, 100), false, false}}
	createBody := nObject(nativeSchema{"definition": nativeBookingDefinition(), "expected_version": nativeSchema{"type": "null"}, "publication_confirmation_id": nUUID()}, "definition")
	saveBody := nObject(nativeSchema{"definition": nativeBookingDefinition(), "expected_version": nInt(1), "publication_confirmation_id": nUUID()}, "definition", "expected_version")
	recoverBody := nativeSchema{"oneOf": []any{createBody, saveBody}}
	operations := []nativeOwnerOp{
		{Groups: groups, Action: "list-services", OperationID: "listBookingServices", Operation: "booking.list_services", Summary: "Lista servicios Booking con campos actuales admitidos", Method: "GET", Path: base + "/services", Scope: "booking:read", Response: nativeBookingList(false), Query: pageQuery, Kind: "booking-list"},
		{Groups: groups, Action: "get-service", OperationID: "getBookingService", Operation: "booking.get_service", Summary: "Consulta un servicio Booking con su máscara actual", Method: "GET", Path: base + "/services/{service}", Args: []string{"service"}, Scope: "booking:read", Response: nEnvelope(nativeBookingService(false)), Kind: "booking-get"},
		{Groups: groups, Action: "create-service", OperationID: "createBookingService", Operation: "booking.save_service", Summary: "Guarda la preparación ORIGINAL emitida por Company; no crea IDs ni aprobación", Method: "POST", Path: base + "/services", Scope: "booking:write", Mutating: true, Original: true, Confirm: true, Body: createBody, Response: nEnvelope(nativeBookingService(false)), Kind: "booking-save", Status: 201},
		{Groups: groups, Action: "save-service", OperationID: "saveBookingService", Operation: "booking.save_service", Summary: "Guarda el servicio con CAS y JSON/clave originales", Method: "PUT", Path: base + "/services/{service}", Args: []string{"service"}, Scope: "booking:write", Mutating: true, Original: true, Confirm: true, Body: saveBody, Response: nEnvelope(nativeBookingService(false)), Kind: "booking-save"},
		{Groups: groups, Action: "recover-service", OperationID: "recoverBookingServiceReceipt", Operation: "booking.service_receipt", Summary: "POST de lectura del recibo original; --data contiene el JSON ORIGINAL, no un sobre", Method: "POST", Path: base + "/services/receipts/{service}", Args: []string{"service"}, OptionalArg: true, Scope: "booking:write", Original: true, Recovery: true, Body: recoverBody, Response: nEnvelope(nNullable(nativeBookingService(false))), Kind: "booking-save"},
		{Groups: groups, Action: "list", OperationID: "listBookings", Operation: "booking.list_bookings", Summary: "Lista reservas administrativas, sin datos privados de visitantes", Method: "GET", Path: base + "/bookings", Scope: "booking:read", Response: nativeBookingList(true), Query: append(append([]nativeQuery{}, pageQuery...), nativeQuery{"service_id", nUUID(), false, false}, nativeQuery{"host_id", nHost(), false, false}, nativeQuery{"status", nEnum("held", "confirmed", "cancelled", "expired"), false, false}, nativeQuery{"from", nText(1, 40), false, false}, nativeQuery{"through", nText(1, 40), false, false}), Kind: "booking-list"},
	}
	for i := range operations {
		operations[i].Realm = client.NativeStaff
	}
	publicGroups := []string{"crm", "bookings", "public"}
	publicBase := "/public/crm/booking"
	configuration := nEnvelope(nObject(nativeSchema{"public_id": nText(32, 128), "publication_version": nInt(1), "business_timezone": nText(1, 100), "is_sandbox": nativeSchema{"type": "boolean"}, "service_display": nText(1, 500), "duration_minutes": nRange(1, 1440), "buffer_before": nRange(0, 1440), "buffer_after": nRange(0, 1440), "notice_version": nText(1, 200), "notice_copy": nText(1, 20000), "captcha": nativeSchema{"oneOf": []any{nObject(nativeSchema{"provider": nEnum("none")}, "provider"), nObject(nativeSchema{"provider": nEnum("turnstile"), "site_key": nativeSchema{"type": "string", "pattern": `^[A-Za-z0-9_-]{1,256}$`}}, "provider", "site_key")}}}, "public_id", "publication_version", "business_timezone", "is_sandbox", "service_display", "duration_minutes", "buffer_before", "buffer_after", "notice_version", "notice_copy", "captcha"))
	availability := nEnvelope(nObject(nativeSchema{"public_id": nText(32, 128), "publication_version": nInt(1), "business_timezone": nText(1, 100), "visitor_timezone": nText(1, 100), "slots": nArray(nativeBookingSlot()), "as_of": nText(1, 40), "notice_version": nText(1, 200), "is_sandbox": nativeSchema{"type": "boolean"}}, "public_id", "publication_version", "business_timezone", "visitor_timezone", "slots", "as_of", "notice_version", "is_sandbox"))
	management := nEnvelope(nObject(nativeSchema{"id": nUUID(), "version": nInt(1), "status": nEnum("held", "confirmed", "cancelled", "expired"), "start_at": nText(1, 40), "end_at": nText(1, 40)}, "id", "version", "status", "start_at", "end_at"))
	publicOps := []nativeOwnerOp{
		{Groups: publicGroups, Action: "configuration", OperationID: "crm.booking.public.configuration", Operation: "booking.public_configuration", Summary: "Configuración pública vigente sin credenciales corporativas", Method: "GET", Path: publicBase + "/{publicId}/configuration", Args: []string{"publicId"}, Response: configuration, Kind: "public-configuration"},
		{Groups: publicGroups, Action: "availability", OperationID: "crm.booking.public.availability", Operation: "booking.get_public_availability", Summary: "Disponibilidad pública con publicación e intervalo explícitos", Method: "GET", Path: publicBase + "/{publicId}/availability", Args: []string{"publicId"}, Response: availability, Kind: "public-availability", Query: []nativeQuery{{"publication_version", nInt(1), true, false}, {"from", nText(20, 20), true, false}, {"to", nText(20, 20), true, false}, {"visitor_timezone", nText(1, 100), true, false}}},
		{Groups: publicGroups, Action: "create", OperationID: "crm.booking.public.create", Operation: "booking.create_public", Summary: "Reserva pública ORIGINAL; exige --live explícito sin inferir el entorno del visitante", Method: "POST", Path: publicBase + "/{publicId}/bookings", Args: []string{"publicId"}, Mutating: true, Original: true, Publication: true, Body: nativeBookingPublicBody("booking.create_public"), Response: nativeBookingPublicReceipt(), Kind: "public-create", Status: 201},
		{Groups: publicGroups, Action: "recover", OperationID: "crm.booking.public.receipt", Operation: "booking.public_receipt", Summary: "Recupera por POST de lectura con operación, clave y bytes ORIGINALES", Method: "POST", Path: publicBase + "/{publicId}/receipts", Args: []string{"publicId"}, Original: true, Recovery: true, PublicRecovery: true, Publication: true, Body: nativeSchema{"oneOf": []any{nativeBookingPublicBody("booking.create_public"), nativeBookingPublicBody("booking.reschedule_public"), nativeBookingPublicBody("booking.cancel_public")}}, Response: nativeBookingPublicReceipt(), Kind: "public-recover"},
		{Groups: publicGroups, Action: "management", OperationID: "crm.booking.public.management", Operation: "booking.get_public_management", Summary: "Consulta con Booking-Management-Token privado, sin idempotencia ni sesión staff", Method: "GET", Path: publicBase + "/{publicId}/management", Args: []string{"publicId"}, Management: true, Response: management, Kind: "public-management"},
		{Groups: publicGroups, Action: "reschedule", OperationID: "crm.booking.public.reschedule", Operation: "booking.reschedule_public", Summary: "Reprograma con token de posesión en el JSON original y CAS actual", Method: "POST", Path: publicBase + "/manage/reschedule", Mutating: true, Original: true, Body: nativeBookingPublicBody("booking.reschedule_public"), Response: nativeBookingPublicReceipt(), Kind: "public-reschedule"},
		{Groups: publicGroups, Action: "cancel", OperationID: "crm.booking.public.cancel", Operation: "booking.cancel_public", Summary: "Cancela con token privado, CAS y confirmación local cancel", Method: "POST", Path: publicBase + "/manage/cancel", Mutating: true, Original: true, Confirm: true, Body: nativeBookingPublicBody("booking.cancel_public"), Response: nativeBookingPublicReceipt(), Kind: "public-cancel"},
	}
	for i := range publicOps {
		publicOps[i].Realm = client.NativeBookingVisitor
	}
	return append(operations, publicOps...)
}

func validateNativeOwnerBody(op nativeOwnerOp, value any, hasTarget bool, originalOperation string) error {
	object, _ := value.(map[string]any)
	if op.Kind == "booking-save" {
		if (hasTarget && object["expected_version"] == nil) || (!hasTarget && object["expected_version"] != nil) {
			return apierr.Usagef("expected_version no coincide con el destino original")
		}
		definition, _ := object["definition"].(map[string]any)
		if sources, exists := definition["required_sources"]; exists {
			found := false
			for _, source := range sources.([]any) {
				found = found || source == "booking"
			}
			if !found {
				return apierr.Usagef("required_sources debe incluir booking")
			}
		}
	}
	if op.PublicRecovery {
		schema := nativeBookingPublicBody(originalOperation)
		if schema == nil || knowledgeSchema(string(nRaw(schema)), value) != nil {
			return apierr.Usagef("--operation no coincide con los bytes originales")
		}
	}
	for _, name := range []string{"slot", "new_slot"} {
		if raw, exists := object[name]; exists {
			slot, _ := raw.(map[string]any)
			start, e := time.Parse(time.RFC3339, slot["start_at"].(string))
			end, e2 := time.Parse(time.RFC3339, slot["end_at"].(string))
			if e != nil || e2 != nil || !start.Before(end) {
				return apierr.Usagef("intervalo nativo inválido")
			}
		}
	}
	if visitor, ok := object["visitor_fields"].(map[string]any); ok {
		email := visitor["email"].(string)
		address, err := mail.ParseAddress(email)
		if err != nil || address.Address != email {
			return apierr.Usagef("email del visitante inválido")
		}
	}
	var definition map[string]any
	if d, ok := object["definition"].(map[string]any); ok {
		definition = d
	}
	if d, ok := definition["schedule"].(map[string]any); ok {
		definition = d
	}
	if timezone, ok := definition["timezone"].(string); ok {
		if err := validateNativeTimezone(timezone); err != nil {
			return err
		}
	}
	for _, name := range []string{"allowed_windows", "windows"} {
		if windows, ok := definition[name].([]any); ok {
			for _, raw := range windows {
				window := raw.(map[string]any)
				if nativeInteger(window["start_minute"]) >= nativeInteger(window["end_minute"]) {
					return apierr.Usagef("ventana local inválida")
				}
			}
		}
	}
	return nil
}

func validateNativeTimezone(value string) error {
	if _, err := time.LoadLocation(value); err != nil || value == "Local" {
		return apierr.Usagef("timezone debe ser IANA explícita")
	}
	return nil
}
func validateNativeInstant(value string, utc bool) error {
	instant, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return err
	}
	if utc && (len(value) != 20 || value[len(value)-1] != 'Z' || instant.Format("2006-01-02T15:04:05Z") != value) {
		return apierr.Usagef("instante UTC nativo inválido")
	}
	return nil
}
