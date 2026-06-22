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
