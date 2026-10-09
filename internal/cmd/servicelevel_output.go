package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/output"
)

type serviceLevelPage struct {
	Items   []json.RawMessage `json:"items"`
	Total   int64             `json:"total"`
	Page    int               `json:"page"`
	PerPage int               `json:"per_page"`
}

func decodeServiceLevelPage(body []byte) (serviceLevelPage, error) {
	var envelope struct {
		Data *serviceLevelPage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data == nil || envelope.Data.Items == nil ||
		envelope.Data.Total < 0 || envelope.Data.Page < 1 || envelope.Data.PerPage < 1 || envelope.Data.PerPage > 100 || len(envelope.Data.Items) > envelope.Data.PerPage {
		return serviceLevelPage{}, &apierr.TransportError{Err: errors.New("la API no devolvió el contrato items/total/page/per_page de SLA")}
	}
	return *envelope.Data, nil
}

func printServiceLevelBody(w io.Writer, body []byte, format output.Format, op serviceLevelOperation) error {
	if format == output.JSON {
		return output.PrintBody(w, body, format)
	}
	value, err := decodeServiceLevelJSON(body)
	if err != nil {
		return &apierr.TransportError{Err: errors.New("la API no devolvió un objeto SLA válido")}
	}
	data, ok := value["data"].(map[string]any)
	if !ok {
		return &apierr.TransportError{Err: errors.New("la API no devolvió data SLA")}
	}
	table := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	writeRow := func(values ...any) error {
		cells := make([]string, len(values))
		for index, value := range values {
			if value == nil {
				cells[index] = "—"
			} else {
				// Remote names/reasons must not inject control sequences or rows
				// into a terminal; JSON output still preserves the original wire.
				cells[index] = strings.Map(func(char rune) rune {
					if char < 32 || char == 127 || (char >= 128 && char <= 159) {
						return ' '
					}
					return char
				}, fmt.Sprint(value))
			}
		}
		_, err := fmt.Fprintln(table, strings.Join(cells, "\t"))
		return err
	}
	if op.mutating() {
		if err := writeRow("ID", "VERSION"); err != nil {
			return err
		}
		if err := writeRow(data["id"], data["version"]); err != nil {
			return err
		}
	} else if op.paginated {
		page, err := decodeServiceLevelPage(body)
		if err != nil {
			return err
		}
		if op.action == "calendars" {
			err = writeRow("ID", "VERSION", "NOMBRE", "ZONA", "MODO")
		} else {
			err = writeRow("ID", "VERSION", "CICLO", "ABIERTO", "TERMINAL")
		}
		if err != nil {
			return err
		}
		for _, raw := range page.Items {
			item, err := decodeServiceLevelJSON(raw)
			if err != nil {
				return &apierr.TransportError{Err: errors.New("la API devolvió un elemento SLA incompatible")}
			}
			if op.action == "calendars" {
				err = writeRow(item["id"], item["version"], item["name"], item["timezone"], item["mode"])
			} else {
				err = writeRow(item["id"], item["version"], item["cycle_number"], item["opened_at"], item["terminal_at"])
			}
			if err != nil {
				return err
			}
		}
		if err := table.Flush(); err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "Página %d · %d por página · %d elementos autorizados\n", page.Page, page.PerPage, page.Total)
		return err
	} else {
		if err := writeRow("ID", "VERSION", "CICLO", "ESTADO", "AS_OF"); err != nil {
			return err
		}
		if err := writeRow(data["id"], data["version"], data["cycle_number"], data["state"], data["as_of"]); err != nil {
			return err
		}
		if err := writeRow("RELOJ", "ESTADO", "MINUTOS", "DEADLINE", "PAUSADO", "SATISFECHO"); err != nil {
			return err
		}
		clocks, ok := data["clocks"].(map[string]any)
		if !ok {
			return &apierr.TransportError{Err: errors.New("la API no devolvió los tres relojes SLA")}
		}
		for _, name := range []string{"first_response", "next_response", "resolution"} {
			clock, ok := clocks[name].(map[string]any)
			if !ok {
				return &apierr.TransportError{Err: errors.New("la API omitió un reloj SLA")}
			}
			if err := writeRow(name, clock["state"], clock["target_minutes"], clock["deadline"], clock["paused"], clock["satisfied"]); err != nil {
				return err
			}
		}
	}
	return table.Flush()
}
