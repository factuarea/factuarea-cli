package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

var crmDocumentUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var crmDocumentNativeUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var crmDocumentMIME = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]*/[a-z0-9][a-z0-9!#$&^_.+-]*$`)
var crmDocumentCurrency = regexp.MustCompile(`^[A-Z]{3}$`)
var crmDocumentMoney = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})\.[0-9]{2}$`)
var crmDocumentForecast = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})\.[0-9]{4}$`)

// Decode tokens so duplicate fields, including nested fields, cannot silently
// replace an original identity/CAS. Numbers retain their exact JSON type.
func crmDocumentJSON(body []byte) (map[string]any, error) {
	if !utf8.Valid(body) {
		return nil, apierr.Usagef("JSON de documentos contiene UTF-8 inválido")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	v, err := crmDocumentValue(d, 0)
	if err != nil {
		return nil, apierr.Usagef("JSON de documentos inválido: %v", err)
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, apierr.Usagef("declara un único objeto JSON de documentos")
	}
	o, ok := v.(map[string]any)
	if !ok {
		return nil, apierr.Usagef("el cuerpo de documentos debe ser un objeto JSON")
	}
	return o, nil
}

func crmDocumentValue(d *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, fmt.Errorf("anidación excesiva")
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return t, nil
	}
	switch delim {
	case '{':
		o := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("campo no textual")
			}
			if _, exists := o[name]; exists {
				return nil, fmt.Errorf("campo repetido %q", name)
			}
			v, err := crmDocumentValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			o[name] = v
		}
		_, err = d.Token()
		return o, err
	case '[':
		items := []any{}
		for d.More() {
			v, err := crmDocumentValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
		_, err = d.Token()
		return items, err
	default:
		return nil, fmt.Errorf("delimitador inesperado")
	}
}

func crmDocumentRecord(v any, required, optional []string) (map[string]any, error) {
	o, ok := v.(map[string]any)
	if !ok {
		return nil, apierr.Usagef("se requiere un objeto JSON")
	}
	allowed := map[string]bool{}
	for _, key := range required {
		allowed[key] = true
		if _, exists := o[key]; !exists {
			return nil, apierr.Usagef("falta el campo %s", key)
		}
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key := range o {
		if !allowed[key] {
			return nil, apierr.Usagef("campo no admitido: %s", key)
		}
	}
	return o, nil
}

func crmDocumentText(v any, max int, empty bool) bool {
	s, ok := v.(string)
	return ok && len(s) <= max && (empty || strings.TrimSpace(s) != "")
}

func crmDocumentID(v any, native bool) bool {
	s, ok := v.(string)
	if !ok || strings.EqualFold(s, "00000000-0000-0000-0000-000000000000") || strings.EqualFold(s, "ffffffff-ffff-ffff-ffff-ffffffffffff") {
		return false
	}
	if native {
		return crmDocumentNativeUUID.MatchString(s)
	}
	return crmDocumentUUID.MatchString(s)
}

func crmDocumentInt(v any, zero bool) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := strconv.ParseInt(string(n), 10, 64)
	minimum := int64(1)
	if zero {
		minimum = 0
	}
	return i, err == nil && i >= minimum && i < math.MaxInt64
}

func crmDocumentEnum(v any, values ...string) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, value := range values {
		if s == value {
			return true
		}
	}
	return false
}

func crmDocumentType(v any) bool {
	return crmDocumentEnum(v, "quote", "invoice", "contract", "recurring_schedule")
}

func crmDocumentDate(v any) bool {
	s, ok := v.(string)
	if !ok || len(s) != 10 {
		return false
	}
	t, err := time.Parse("2006-01-02", s)
	return err == nil && t.Format("2006-01-02") == s
}

func crmDocumentTimestamp(v any) bool {
	s, ok := v.(string)
	if !ok || len(s) > 100 {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, s)
	return err == nil
}

func crmDocumentKey(key string) error {
	if len(key) < 1 || len(key) > 255 || key[0] == ' ' || key[len(key)-1] == ' ' {
		return apierr.Usagef("--idempotency-key requiere 1–255 caracteres ASCII, sin espacios exteriores que HTTP eliminaría")
	}
	for _, c := range []byte(key) {
		if c < 0x20 || c > 0x7e {
			return apierr.Usagef("--idempotency-key requiere ASCII sin caracteres de control")
		}
	}
	return nil
}

func validateCRMDocumentBody(op crmDocumentOperation, body []byte) (map[string]any, error) {
	if len(body) > 262144 {
		return nil, apierr.Usagef("el cuerpo de documentos supera 262144 bytes")
	}
	o, err := crmDocumentJSON(body)
	if err != nil {
		return nil, err
	}
	required, optional := crmDocumentBodyFields(op.action)
	if _, err := crmDocumentRecord(o, required, optional); err != nil {
		return nil, err
	}
	bad := func() (map[string]any, error) {
		return nil, apierr.Usagef("campos inválidos para %s; consulta --help o --skeleton", op.action)
	}
	if v, exists := o["expected_version"]; exists {
		if _, ok := crmDocumentInt(v, false); !ok {
			return bad()
		}
	}
	switch op.action {
	case "upload", "replace-version":
		mime, ok := o["claimed_mime"].(string)
		if !crmDocumentID(o["producer_reference_id"], false) || !ok || len(mime) > 255 || !crmDocumentMIME.MatchString(mime) || (op.action == "upload" && !crmDocumentText(o["title"], 255, false)) {
			return bad()
		}
	case "share":
		if !crmDocumentEnum(o["grant_type"], "user", "team") || !crmDocumentID(o["grantee_id"], o["grant_type"] == "user") {
			return bad()
		}
		if v, exists := o["expires_at"]; exists && v != nil && !crmDocumentTimestamp(v) {
			return bad()
		}
	case "revoke-share":
		if !crmDocumentID(o["share_id"], false) {
			return bad()
		}
	case "link":
		if !crmDocumentType(o["document_type"]) || !crmDocumentID(o["document_id"], true) {
			return bad()
		}
		if _, ok := o["is_primary"].(bool); !ok {
			return bad()
		}
	case "create-quote":
		if v, exists := o["contact_id"]; exists && v != nil && !crmDocumentID(v, false) {
			return bad()
		}
		if err := validateCRMQuoteDraft(o["draft"]); err != nil {
			return nil, err
		}
	case "archive", "unlink":
	default:
		return bad()
	}
	return o, nil
}

func crmDocumentBodyFields(action string) ([]string, []string) {
	switch action {
	case "upload":
		return []string{"title", "producer_reference_id", "claimed_mime"}, nil
	case "replace-version":
		return []string{"expected_version", "producer_reference_id", "claimed_mime"}, nil
	case "share":
		return []string{"expected_version", "grant_type", "grantee_id"}, []string{"expires_at"}
	case "revoke-share":
		return []string{"expected_version", "share_id"}, nil
	case "archive", "unlink":
		return []string{"expected_version"}, nil
	case "link":
		return []string{"document_type", "document_id", "is_primary"}, nil
	case "create-quote":
		return []string{"draft"}, []string{"contact_id"}
	}
	return nil, nil
}

func validateCRMQuoteDraft(v any) error {
	o, err := crmDocumentRecord(v, []string{"date", "recipient"}, []string{"expiry_date", "series_id", "client_id", "notes", "terms", "expected_root_version", "expected_line_version"})
	if err != nil {
		return err
	}
	r, err := crmDocumentRecord(o["recipient"], []string{"kind"}, []string{"id"})
	if err != nil {
		return err
	}
	bad := func() error {
		return apierr.Usagef("draft debe conservar el destinatario prefiscal y las revisiones nativas de Quote")
	}
	if !crmDocumentDate(o["date"]) || !crmDocumentEnum(r["kind"], "unassigned", "commercial_counterparty", "business_contact") {
		return bad()
	}
	if r["kind"] == "unassigned" {
		if r["id"] != nil {
			return bad()
		}
	} else if !crmDocumentID(r["id"], false) {
		return bad()
	}
	if expiry := o["expiry_date"]; expiry != nil && (!crmDocumentDate(expiry) || expiry.(string) < o["date"].(string)) {
		return bad()
	}
	for _, key := range []string{"series_id", "client_id"} {
		if v := o[key]; v != nil && !crmDocumentID(v, true) {
			return bad()
		}
	}
	if o["client_id"] != nil && r["kind"] != "business_contact" {
		return bad()
	}
	for _, key := range []string{"notes", "terms"} {
		if v := o[key]; v != nil && !crmDocumentText(v, 262144, true) {
			return bad()
		}
	}
	for _, key := range []string{"expected_root_version", "expected_line_version"} {
		if v, exists := o[key]; exists {
			if _, ok := crmDocumentInt(v, false); !ok {
				return bad()
			}
		}
	}
	return nil
}

func crmDocumentData(body []byte) (map[string]any, error) {
	o, err := crmDocumentJSON(body)
	if err != nil {
		return nil, err
	}
	if _, exists := o["error"]; exists {
		return nil, fmt.Errorf("respuesta contiene error")
	}
	data, ok := o["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("respuesta sin data nativo")
	}
	return data, nil
}

func validateCRMDocumentReceipt(op crmDocumentOperation, args []string, request, receipt map[string]any) error {
	required := []string{"id", "version"}
	if op.group == "opportunity-documents" {
		required = append(required, "opportunity_id", "document_type", "document_id")
		if op.action != "unlink" {
			required = append(required, "is_primary", "status", "document")
		}
	}
	if _, err := crmDocumentRecord(receipt, required, nil); err != nil {
		return err
	}
	version, ok := crmDocumentInt(receipt["version"], false)
	if !ok || !crmDocumentID(receipt["id"], false) {
		return fmt.Errorf("recibo sin identidad/revisión nativa")
	}
	if op.group == "documents" {
		if op.action == "upload" {
			if version != 1 {
				return fmt.Errorf("upload no devolvió versión original 1")
			}
		} else if !strings.EqualFold(receipt["id"].(string), args[0]) {
			return fmt.Errorf("recibo de otro documento")
		}
	} else {
		if !crmDocumentID(receipt["opportunity_id"], false) || !strings.EqualFold(receipt["opportunity_id"].(string), args[0]) || !crmDocumentType(receipt["document_type"]) || !crmDocumentID(receipt["document_id"], true) {
			return fmt.Errorf("recibo de otra oportunidad/propietario")
		}
		if op.action == "unlink" {
			if !strings.EqualFold(receipt["id"].(string), args[1]) {
				return fmt.Errorf("recibo de otra asociación")
			}
		} else {
			if err := validateCRMDocumentLink(receipt, args[0]); err != nil {
				return err
			}
			wantType := request["document_type"]
			if op.action == "create-quote" {
				wantType = "quote"
			}
			if receipt["status"] != "linked" || receipt["document"] == nil || receipt["document_type"] != wantType {
				return fmt.Errorf("vínculo sin documento canónico confirmado")
			}
			if op.action == "link" && (!strings.EqualFold(receipt["document_id"].(string), request["document_id"].(string)) || (request["is_primary"] == true && receipt["is_primary"] != true)) {
				return fmt.Errorf("vínculo distinto del intent original")
			}
		}
	}
	if expected, exists := request["expected_version"]; exists {
		n, _ := crmDocumentInt(expected, false)
		if version != n+1 {
			return fmt.Errorf("recibo no corresponde al CAS original + 1")
		}
	}
	return nil
}

func validateCRMDocumentLink(v any, parent string) error {
	o, err := crmDocumentRecord(v, []string{"id", "version", "opportunity_id", "document_type", "document_id", "is_primary", "status", "document"}, nil)
	if err != nil {
		return err
	}
	_, versionOK := crmDocumentInt(o["version"], false)
	_, boolOK := o["is_primary"].(bool)
	if !versionOK || !boolOK || !crmDocumentID(o["id"], false) || !crmDocumentID(o["opportunity_id"], false) || !strings.EqualFold(o["opportunity_id"].(string), parent) || !crmDocumentType(o["document_type"]) || !crmDocumentID(o["document_id"], true) || !crmDocumentText(o["status"], 100, false) {
		return fmt.Errorf("asociación inválida")
	}
	if o["document"] != nil {
		if err := validateCRMNativeDocument(o["document"], false); err != nil {
			return err
		}
		d := o["document"].(map[string]any)
		if d["type"] != o["document_type"] || !strings.EqualFold(d["id"].(string), o["document_id"].(string)) {
			return fmt.Errorf("asociación referencia otro documento canónico")
		}
	}
	return nil
}

func validateCRMNativeDocument(v any, candidate bool) error {
	required := []string{"id", "type", "version", "state", "number", "currency", "quoted_amount", "invoiced_amount", "paid_amount", "financial_applicability", "valid_from", "valid_until"}
	versionKey, labelKey := "version", "number"
	if candidate {
		required = []string{"id", "type", "revision", "state", "label", "currency", "valid_from", "valid_until", "financial_applicability"}
		versionKey, labelKey = "revision", "label"
	}
	o, err := crmDocumentRecord(v, required, nil)
	if err != nil {
		return err
	}
	_, versionOK := crmDocumentInt(o[versionKey], false)
	currency, _ := o["currency"].(string)
	if !versionOK || !crmDocumentID(o["id"], true) || !crmDocumentType(o["type"]) || !crmDocumentText(o["state"], 100, false) || !crmDocumentCurrency.MatchString(currency) || (o[labelKey] != nil && !crmDocumentText(o[labelKey], 255, false)) {
		return fmt.Errorf("documento canónico inválido")
	}
	for _, key := range []string{"valid_from", "valid_until"} {
		if o[key] != nil && !crmDocumentDate(o[key]) {
			return fmt.Errorf("fecha nativa inválida")
		}
	}
	fiscal := o["type"] == "quote" || o["type"] == "invoice"
	applicability := "not_applicable"
	if fiscal {
		applicability = "applicable"
	}
	if o["financial_applicability"] != applicability {
		return fmt.Errorf("aplicabilidad financiera distinta del propietario")
	}
	if !candidate {
		for _, key := range []string{"quoted_amount", "invoiced_amount", "paid_amount"} {
			s, stringOK := o[key].(string)
			if (fiscal && (!stringOK || !crmDocumentMoney.MatchString(s))) || (!fiscal && o[key] != nil) {
				return fmt.Errorf("importe debe conservar decimal-string/null nativo")
			}
		}
	}
	return nil
}

func validateCRMLibraryDocument(v any) error {
	o, err := crmDocumentRecord(v, []string{"id", "version"}, []string{"title", "visibility", "created_by_id", "archived_at", "archived_by_id", "versions", "links", "shares"})
	if err != nil {
		return err
	}
	_, versionOK := crmDocumentInt(o["version"], false)
	if !crmDocumentID(o["id"], false) || !versionOK {
		return fmt.Errorf("identidad/revisión de biblioteca inválida")
	}
	for _, key := range []string{"title", "visibility"} {
		if v, exists := o[key]; exists && !crmDocumentText(v, 255, false) {
			return fmt.Errorf("campo textual inválido")
		}
	}
	for _, key := range []string{"created_by_id", "archived_by_id"} {
		if v, exists := o[key]; exists && !(key == "archived_by_id" && v == nil) && !crmDocumentID(v, true) {
			return fmt.Errorf("identidad de actor inválida")
		}
	}
	if v := o["archived_at"]; v != nil && !crmDocumentTimestamp(v) {
		return fmt.Errorf("fecha de archivo inválida")
	}
	for _, key := range []string{"versions", "links", "shares"} {
		if v, exists := o[key]; exists {
			items, ok := v.([]any)
			if !ok {
				return fmt.Errorf("colección de biblioteca inválida")
			}
			for _, item := range items {
				if err := validateCRMLibraryChild(key, item); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateCRMLibraryChild(kind string, v any) error {
	fields := map[string][]string{
		"versions": {"number", "mime", "size_bytes", "created_by_id", "created_at"},
		"links":    {"entity_type", "entity_id", "document_version", "linked_by_id", "linked_at"},
		"shares":   {"id", "grant_type", "grantee_id", "expires_at", "revoked_at", "revoked_by_id", "created_by_id", "created_at"},
	}
	o, err := crmDocumentRecord(v, fields[kind], nil)
	if err != nil {
		return err
	}
	for key, value := range o {
		valid := true
		switch key {
		case "number", "document_version":
			_, valid = crmDocumentInt(value, false)
		case "size_bytes":
			_, valid = crmDocumentInt(value, true)
		case "id", "entity_id":
			valid = crmDocumentID(value, false)
		case "created_by_id", "linked_by_id", "grantee_id":
			valid = crmDocumentID(value, true)
		case "revoked_by_id":
			valid = value == nil || crmDocumentID(value, true)
		case "created_at", "linked_at":
			valid = crmDocumentTimestamp(value)
		case "expires_at", "revoked_at":
			valid = value == nil || crmDocumentTimestamp(value)
		case "mime":
			s, ok := value.(string)
			valid = ok && len(s) <= 255 && crmDocumentMIME.MatchString(s)
		case "grant_type", "entity_type":
			valid = crmDocumentText(value, 100, false)
		}
		if !valid {
			return fmt.Errorf("campo %s de %s inválido", key, kind)
		}
	}
	return nil
}

func validateCRMDocumentRead(op crmDocumentOperation, args []string, data map[string]any, query map[string][]string) error {
	if op.group == "documents" && op.action == "get" {
		if err := validateCRMLibraryDocument(data); err != nil {
			return err
		}
		if !strings.EqualFold(data["id"].(string), args[0]) {
			return fmt.Errorf("lectura de otra identidad")
		}
		return nil
	}
	if op.action == "financial-summary" {
		if _, err := crmDocumentRecord(data, []string{"opportunity_id", "version", "currencies"}, nil); err != nil {
			return err
		}
		_, validVersion := crmDocumentInt(data["version"], false)
		items, ok := data["currencies"].([]any)
		if !validVersion || !ok || !crmDocumentID(data["opportunity_id"], false) || !strings.EqualFold(data["opportunity_id"].(string), args[0]) {
			return fmt.Errorf("resumen de otra oportunidad/revisión")
		}
		seen := map[string]bool{}
		for _, v := range items {
			o, err := crmDocumentRecord(v, []string{"currency", "forecast", "quoted", "invoiced", "paid"}, nil)
			if err != nil {
				return err
			}
			currency, _ := o["currency"].(string)
			if !crmDocumentCurrency.MatchString(currency) || seen[currency] {
				return fmt.Errorf("divisa inválida/repetida; no se agrega FX")
			}
			seen[currency] = true
			for _, key := range []string{"forecast", "quoted", "invoiced", "paid"} {
				s, ok := o[key].(string)
				pattern := crmDocumentMoney
				if key == "forecast" {
					pattern = crmDocumentForecast
				}
				if !ok || !pattern.MatchString(s) {
					return fmt.Errorf("dinero debe conservar escala y decimal-string nativos")
				}
			}
		}
		return nil
	}
	required := []string{"items", "total", "page", "per_page"}
	if op.group == "opportunity-documents" {
		required = []string{"opportunity_id", "items", "next_cursor", "total", "pending_count"}
		if op.action == "candidates" {
			required = []string{"opportunity_id", "document_type", "items", "next_cursor", "available", "unavailable_reason"}
		}
	}
	if _, err := crmDocumentRecord(data, required, nil); err != nil {
		return err
	}
	items, ok := data["items"].([]any)
	if !ok {
		return fmt.Errorf("items debe conservar su colección nativa")
	}
	if op.group == "documents" {
		page, pageOK := crmDocumentInt(data["page"], false)
		perPage, perOK := crmDocumentInt(data["per_page"], false)
		_, totalOK := crmDocumentInt(data["total"], true)
		if !pageOK || !perOK || !totalOK || strconv.FormatInt(page, 10) != query["page"][0] || strconv.FormatInt(perPage, 10) != query["per_page"][0] || int64(len(items)) > perPage {
			return fmt.Errorf("página distinta de la solicitada")
		}
	} else {
		if !crmDocumentID(data["opportunity_id"], false) || !strings.EqualFold(data["opportunity_id"].(string), args[0]) {
			return fmt.Errorf("página de otra oportunidad")
		}
		cursorMax := 8192
		if op.action == "candidates" {
			cursorMax = 32768
			available, boolOK := data["available"].(bool)
			if !boolOK || data["document_type"] != query["document_type"][0] || !crmDocumentType(data["document_type"]) || (available && data["unavailable_reason"] != nil) || (!available && (!crmDocumentText(data["unavailable_reason"], 255, false) || len(items) != 0 || data["next_cursor"] != nil)) {
				return fmt.Errorf("candidates no conserva disponibilidad/propietario nativo")
			}
		} else {
			_, totalOK := crmDocumentInt(data["total"], true)
			_, pendingOK := crmDocumentInt(data["pending_count"], true)
			if !totalOK || !pendingOK {
				return fmt.Errorf("conteos no son enteros nativos")
			}
		}
		if data["next_cursor"] != nil && !crmDocumentText(data["next_cursor"], cursorMax, false) {
			return fmt.Errorf("cursor inválido")
		}
		limit, _ := strconv.Atoi(query["limit"][0])
		if len(items) > limit {
			return fmt.Errorf("página supera el límite solicitado")
		}
	}
	for _, item := range items {
		var err error
		switch {
		case op.group == "documents":
			err = validateCRMLibraryDocument(item)
		case op.action == "candidates":
			err = validateCRMNativeDocument(item, true)
			if err == nil && item.(map[string]any)["type"] != data["document_type"] {
				err = fmt.Errorf("candidate de otro propietario")
			}
		default:
			err = validateCRMDocumentLink(item, args[0])
		}
		if err != nil {
			return err
		}
	}
	return nil
}
