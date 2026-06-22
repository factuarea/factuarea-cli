package spec

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sort"
	"testing"
	"time"
)

// TestSpecOperationParity es un drift-guard OPT-IN: solo corre con
// FACTUAREA_CHECK_DRIFT=1. El objetivo es cazar "el binario distribuido NO
// conoce endpoints nuevos de la API" — un problema real para un CLI que se
// distribuye compilado.
//
// Compara el CONJUNTO de operationIds (no el hash byte-a-byte), porque el spec
// embebido es de develop y puede diferir byte-a-byte del de producción (orden
// de ejemplos, etc.) sin que falte ningún comando. Comparar el set tolera ese
// skew y solo señala lo que de verdad importa: operationIds que el spec vivo
// expone y el embebido NO tiene (= el CLI va por detrás de la API).
//
// Direccionalidad:
//   - vivo ⊄ embebido (faltan en el embebido)  → FALLA (regenerar el CLI).
//   - embebido ⊋ vivo  (develop adelantado a prod) → NO falla, solo informa.
//
// Sin red → t.Skip (no ensucia el pipeline offline).
func TestSpecOperationParity(t *testing.T) {
	if os.Getenv("FACTUAREA_CHECK_DRIFT") != "1" {
		t.Skip("drift-guard opt-in: exporta FACTUAREA_CHECK_DRIFT=1 para activarlo")
	}

	url := os.Getenv("FACTUAREA_SPEC_URL")
	if url == "" {
		url = "https://api.factuarea.com/v1/openapi.json"
	}

	live, err := fetchOperationIDs(url)
	if err != nil {
		t.Skipf("sin red para drift-guard (%s): %v", url, err)
	}
	if len(live) == 0 {
		t.Skipf("el spec vivo (%s) no expone operationIds; se omite el drift-guard", url)
	}

	embedded := embeddedOperationIDs(t)

	var missing []string // en el vivo pero NO en el embebido → CLI por detrás
	for id := range live {
		if _, ok := embedded[id]; !ok {
			missing = append(missing, id)
		}
	}
	var extra []string // en el embebido pero NO en el vivo → develop adelantado
	for id := range embedded {
		if _, ok := live[id]; !ok {
			extra = append(extra, id)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(extra) > 0 {
		t.Logf("el spec embebido tiene %d operationId(s) que el vivo (%s) no expone "+
			"(develop adelantado a prod, no es un fallo): %v", len(extra), url, extra)
	}

	if len(missing) > 0 {
		t.Fatalf("el spec vivo (%s) expone %d operationId(s) que el CLI embebido NO conoce; "+
			"corre `make generate` (o `make generate-dev`) y regenera. Faltan: %v",
			url, len(missing), missing)
	}
}

// fetchOperationIDs baja el spec vivo y extrae el conjunto de operationIds de
// paths.*.{get,post,put,patch,delete}.operationId.
func fetchOperationIDs(url string) (map[string]struct{}, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{code: resp.StatusCode}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseOperationIDs(body)
}

// httpStatusError reporta un código HTTP no-200 del spec vivo.
type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string {
	return "estado HTTP inesperado: " + http.StatusText(e.code)
}

// minimalSpec es la vista mínima del OpenAPI necesaria para extraer operationIds.
type minimalSpec struct {
	Paths map[string]map[string]struct {
		OperationID string `json:"operationId"`
	} `json:"paths"`
}

var httpMethods = map[string]struct{}{
	"get": {}, "post": {}, "put": {}, "patch": {}, "delete": {},
}

// parseOperationIDs extrae el conjunto de operationIds de un documento OpenAPI.
func parseOperationIDs(raw []byte) (map[string]struct{}, error) {
	var doc minimalSpec
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	ids := map[string]struct{}{}
	for _, item := range doc.Paths {
		for method, op := range item {
			if _, ok := httpMethods[method]; !ok {
				continue
			}
			if op.OperationID != "" {
				ids[op.OperationID] = struct{}{}
			}
		}
	}
	return ids, nil
}

// embeddedOperationIDs devuelve el conjunto COMPLETO de operationIds del spec
// embebido: Load() da las operaciones conformes y nonConforming las que no
// resuelven a un comando; la unión cubre todo lo que el spec embebido declara.
func embeddedOperationIDs(t *testing.T) map[string]struct{} {
	t.Helper()
	ops, nonConforming, err := Load()
	if err != nil {
		t.Fatalf("Load() del spec embebido falló: %v", err)
	}
	ids := map[string]struct{}{}
	for _, op := range ops {
		ids[op.OperationID] = struct{}{}
	}
	for _, id := range nonConforming {
		ids[id] = struct{}{}
	}
	return ids
}
