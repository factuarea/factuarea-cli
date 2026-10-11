package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/spec"
)

type crmEntityExportSelection struct{ id, entity string }
type crmEntityExportJob struct {
	ID       string
	Entity   string
	Status   string
	Filename string
	Size     int64
}

// The typed envelope/UUID conventions are shared with native SLA. This boundary
// also rejects duplicate members while retaining the caller's exact wire bytes.
func crmEntityExportJSON(body []byte) (map[string]any, error) {
	if !utf8.Valid(body) {
		return nil, apierr.Usagef("el JSON de exportación requiere UTF-8 válido")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	value, err := crmEntityExportValue(d, 0)
	if err != nil {
		return nil, apierr.Usagef("el JSON de exportación no conserva un objeto único sin campos repetidos")
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, apierr.Usagef("declara un único objeto JSON de exportación")
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, apierr.Usagef("el cuerpo de exportación debe ser un objeto JSON")
	}
	return object, nil
}

func crmEntityExportValue(d *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, fmt.Errorf("JSON demasiado anidado")
	}
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("campo no textual")
			}
			if _, duplicate := object[name]; duplicate {
				return nil, fmt.Errorf("campo repetido")
			}
			value, err := crmEntityExportValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			object[name] = value
		}
		_, err = d.Token()
		return object, err
	case '[':
		items := []any{}
		for d.More() {
			value, err := crmEntityExportValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		_, err = d.Token()
		return items, err
	}
	return nil, fmt.Errorf("delimitador no válido")
}

func crmEntityExportKeys(object map[string]any, expected []string) bool {
	if len(object) != len(expected) {
		return false
	}
	for _, field := range expected {
		if _, exists := object[field]; !exists {
			return false
		}
	}
	return true
}

func validateCrmEntityExportRequest(body []byte) (crmEntityExportSelection, error) {
	var selection crmEntityExportSelection
	contract := spec.NativeCrmEntityExportContract()
	if len(body) > contract.RequestMaxBytes {
		return selection, apierr.Usagef("el cuerpo excede el máximo nativo de exportación")
	}
	object, err := crmEntityExportJSON(body)
	if err != nil {
		return selection, err
	}
	if !crmEntityExportKeys(object, contract.RequestKeys) || object["confirmed"] != true {
		return selection, apierr.Usagef("exportación requiere exactamente id, entity_type, filters, columns y confirmed:true")
	}
	id, ok := object["id"].(string)
	if !ok || !serviceLevelUUID.MatchString(id) || id != strings.ToLower(id) {
		return selection, apierr.Usagef("id debe conservar el UUIDv7 original en minúsculas")
	}
	entity, ok := object["entity_type"].(string)
	dataset, found := contract.Datasets[entity]
	if !ok || !found {
		return selection, apierr.Usagef("entity_type debe ser organization, person, lead, opportunity, activity o service_ticket")
	}
	filters, ok := object["filters"].(map[string]any)
	if !ok {
		return selection, apierr.Usagef("filters debe ser el objeto nativo del dueño")
	}
	for field := range filters {
		if _, exists := dataset.Filters[field]; !exists {
			return selection, apierr.Usagef("filters contiene un campo fuera del contrato del dueño; no se admite page, cursor ni limit")
		}
	}
	columns, ok := object["columns"].([]any)
	if !ok || len(columns) < contract.ColumnsMin || len(columns) > contract.ColumnsMax {
		return selection, apierr.Usagef("columns debe conservar la lista nativa ordenada de 1 a 128 columnas")
	}
	allowed := map[string]bool{}
	for _, column := range dataset.Columns {
		allowed[column] = true
	}
	selected := map[string]bool{}
	for _, value := range columns {
		column, ok := value.(string)
		if !ok || !allowed[column] || selected[column] {
			return selection, apierr.Usagef("columns debe contener columnas nativas distintas del dueño seleccionado")
		}
		selected[column] = true
	}
	// No normalization of filters/period or columns: the native publisher validates
	// values and current definition, scope and masks. This static schema grants none.
	return crmEntityExportSelection{id: id, entity: entity}, nil
}

func decodeCrmEntityExportJob(body []byte, id, entity string) (crmEntityExportJob, error) {
	var job crmEntityExportJob
	envelope, err := crmEntityExportJSON(body)
	if err != nil || !crmEntityExportKeys(envelope, []string{"data"}) {
		return job, &apierr.TransportError{Err: fmt.Errorf("respuesta de exportación sin Original tipado")}
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		return job, &apierr.TransportError{Err: fmt.Errorf("Original de exportación incompatible")}
	}
	job.ID, _ = data["id"].(string)
	job.Entity, _ = data["entity_type"].(string)
	job.Status, _ = data["status"].(string)
	contract := spec.NativeCrmEntityExportContract()
	_, owner := contract.Datasets[job.Entity]
	if job.ID != id || !owner || (entity != "" && job.Entity != entity) {
		return job, &apierr.TransportError{Err: fmt.Errorf("Original no corresponde al identificador y dueño solicitados")}
	}
	keys := []string{"id", "entity_type", "status"}
	switch job.Status {
	case "processing", "failed", "expired":
	case "ready":
		keys = append(keys, "filename", "size_bytes")
		job.Filename, _ = data["filename"].(string)
		size, ok := data["size_bytes"].(json.Number)
		if !ok {
			return job, &apierr.TransportError{Err: fmt.Errorf("Original ready carece del tamaño nativo")}
		}
		job.Size, err = size.Int64()
		if err != nil || job.Size < 0 || job.Filename != "crm-"+job.Entity+".csv" {
			return job, &apierr.TransportError{Err: fmt.Errorf("Original ready carece de metadata válida")}
		}
	default:
		return job, &apierr.TransportError{Err: fmt.Errorf("estado Original no publicado por el dueño")}
	}
	if !crmEntityExportKeys(data, keys) {
		return job, &apierr.TransportError{Err: fmt.Errorf("Original contiene campos fuera del contrato público")}
	}
	return job, nil
}
