package spec

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestSpecOperationParity compara el conjunto de `operationId` del documento.
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

	var missing []string
	for id := range live {
		if _, ok := embedded[id]; !ok {
			missing = append(missing, id)
		}
	}
	var extra []string
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

type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string {
	return "estado HTTP inesperado: " + http.StatusText(e.code)
}

type minimalSpec struct {
	Paths map[string]map[string]struct {
		OperationID string `json:"operationId"`
	} `json:"paths"`
}

var httpMethods = map[string]struct{}{
	"get": {}, "post": {}, "put": {}, "patch": {}, "delete": {},
}

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

// embeddedOperationFloor es un SUELO.
const embeddedOperationFloor = 400

// TestEmbeddedSpecHasNoNonConformingOperationIDs mide el censo de operaciones.
func TestEmbeddedSpecHasNoNonConformingOperationIDs(t *testing.T) {
	ops, nonConforming, err := Load()
	if err != nil {
		t.Fatalf("Load() del spec embebido falló: %v", err)
	}
	if len(nonConforming) != 0 {
		t.Fatalf("el spec embebido declara %d operationId(s) fuera del espacio de nombres %q: %v. "+
			"El generador aborta con código distinto de cero ante cualquiera de ellos y no hay lista de "+
			"excepciones: el arreglo va en el nombre de la ruta del BACKEND, no en este repo",
			len(nonConforming), OperationIDPrefix, nonConforming)
	}
	if len(ops) < embeddedOperationFloor {
		t.Fatalf("el spec embebido solo resolvió %d operaciones (suelo %d): el censo de no conformes "+
			"saldría en cero por no haber medido nada. ¿`internal/spec/openapi.json` vacío o truncado?",
			len(ops), embeddedOperationFloor)
	}
	t.Logf("censo del spec embebido: %d operaciones conformes, 0 no conformes (sha256 del documento %s…)",
		len(ops), Hash()[:12])
}

// TestLocalSpecFixtureHasNoNonConformingOperationIDs corre el MISMO censo.
func TestLocalSpecFixtureHasNoNonConformingOperationIDs(t *testing.T) {
	path := os.Getenv("FACTUAREA_SPEC_FIXTURE")
	if path == "" {
		t.Skip("censo sobre documento local opt-in: exporta FACTUAREA_SPEC_FIXTURE=<ruta a un openapi.json>")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no se pudo leer el documento %s: %v", path, err)
	}
	ids, err := operationIDsOf(raw)
	if err != nil {
		t.Fatalf("no se pudo parsear el documento %s: %v", path, err)
	}
	if len(ids) == 0 {
		t.Fatalf("el documento %s no declara ningún operationId: el censo no mediría nada", path)
	}

	var nonConforming []string
	for id := range ids {
		if _, _, ok := Resolve(id); !ok {
			nonConforming = append(nonConforming, id)
		}
	}
	sort.Strings(nonConforming)
	if len(nonConforming) != 0 {
		t.Fatalf("%s declara %d operationId(s) fuera del espacio de nombres %q: %v",
			path, len(nonConforming), OperationIDPrefix, nonConforming)
	}
	t.Logf("censo de %s: %d operationId(s), 0 no conformes", path, len(ids))
}

// operationIDsOf extrae los `operationId` de un documento OpenAPI arbitrario.
func operationIDsOf(raw []byte) (map[string]struct{}, error) {
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	ids := map[string]struct{}{}
	for _, item := range doc.Paths {
		for method, rawOp := range item {
			if _, ok := httpMethods[strings.ToLower(method)]; !ok {
				continue
			}
			var op struct {
				OperationID string `json:"operationId"`
			}
			if err := json.Unmarshal(rawOp, &op); err != nil {
				continue
			}
			if op.OperationID != "" {
				ids[op.OperationID] = struct{}{}
			}
		}
	}
	return ids, nil
}
