package cmd

import (
	"bytes"
	"encoding/json"
)

// specProperty es la vista mínima de una propiedad de schema que leen los
// tests que congelan el contrato de respuesta. Una propiedad puede ser un
// esquema booleano (`false` reserva un campo que la API no acepta todavía): se
// decodifica como propiedad vacía en vez de romper la lectura del spec entero.
type specProperty struct {
	Type json.RawMessage `json:"type"`
	// `[]any`, no `[]string`: el enum de un campo nullable trae un `null` y otros
	// schemas del spec declaran enums numéricos/booleanos.
	Enum       []any                   `json:"enum"`
	Properties map[string]specProperty `json:"properties"`
}

func (p *specProperty) UnmarshalJSON(data []byte) error {
	if t := bytes.TrimSpace(data); bytes.Equal(t, []byte("true")) || bytes.Equal(t, []byte("false")) {
		return nil
	}
	type plain specProperty
	return json.Unmarshal(data, (*plain)(p))
}
