package spec

import (
	"testing"
)

// miniSpec imita las operaciones del contrato de plantillas PDF: PATCH
// condicionales con If-Match, descarga binaria con 202/304, subida multipart y el
// envío a FACe.
const miniSpec = `{
  "openapi": "3.1.0",
  "info": {"title": "mini", "version": "1"},
  "components": {"parameters": {
    "IdempotencyKey": {"name": "Idempotency-Key", "in": "header", "required": true, "schema": {"type": "string"}},
    "FactuareaVersion": {"name": "Factuarea-Version", "in": "header", "schema": {"type": "string"}},
    "XActiveProfile": {"name": "X-Active-Profile", "in": "header", "schema": {"type": "string"}}
  }},
  "paths": {
    "/companies/{company}/series/{series}/pdf-template": {"patch": {
      "operationId": "public-api.v1.series.assign_pdf_template",
      "parameters": [
        {"name": "company", "in": "path", "required": true, "schema": {"type": "string"}},
        {"name": "series", "in": "path", "required": true, "schema": {"type": "string"}},
        {"name": "If-Match", "in": "header", "description": "Strong ETag", "schema": {"type": "string"}},
        {"$ref": "#/components/parameters/IdempotencyKey"},
        {"$ref": "#/components/parameters/FactuareaVersion"},
        {"$ref": "#/components/parameters/XActiveProfile"}
      ],
      "requestBody": {"content": {"application/json": {"schema": {"type": "object", "properties": {
        "pdf_template_id": {"anyOf": [{"type": "string"}, {"type": "null"}]},
        "expected_version": {"type": "integer"}
      }}}}},
      "responses": {
        "200": {"description": "ok", "headers": {"ETag": {"schema": {"type": "string"}}}, "content": {"application/json": {"schema": {"type": "object"}}}},
        "409": {"description": "conflict"}, "412": {"description": "precondition"}, "428": {"description": "required"}
      }
    }},
    "/companies/{company}/invoices/{invoice}/pdf": {"get": {
      "operationId": "public-api.v1.invoices.pdf",
      "parameters": [{"name": "company", "in": "path", "required": true, "schema": {"type": "string"}}, {"name": "invoice", "in": "path", "required": true, "schema": {"type": "string"}}],
      "responses": {
        "200": {"description": "pdf", "headers": {"ETag": {"schema": {"type": "string"}}}, "content": {"application/pdf": {"schema": {"type": "string", "format": "binary"}}}},
        "202": {"description": "pending", "content": {"application/json": {"schema": {"type": "object"}}}},
        "304": {"description": "not modified"}
      }
    }},
    "/companies/{company}/pdf-artifacts/{artifact}/download": {"get": {
      "operationId": "public-api.v1.pdf_artifacts.download",
      "parameters": [
        {"name": "company", "in": "path", "required": true, "schema": {"type": "string"}},
        {"name": "artifact", "in": "path", "required": true, "schema": {"type": "string"}},
        {"name": "If-None-Match", "in": "header", "required": true, "schema": {"type": "string"}}
      ],
      "responses": {
        "200": {"description": "pdf", "content": {"application/pdf": {"schema": {"type": "string", "format": "binary"}}}},
        "304": {"description": "not modified", "headers": {"ETag": {"schema": {"type": "string"}}}},
        "503": {"description": "pending", "headers": {"Retry-After": {"schema": {"type": "integer"}}}}
      }
    }},
    "/companies/{company}/pdf-templates/assets": {"post": {
      "operationId": "public-api.v1.pdf_templates.asset_upload",
      "parameters": [{"name": "company", "in": "path", "required": true, "schema": {"type": "string"}}, {"$ref": "#/components/parameters/IdempotencyKey"}],
      "requestBody": {"required": true, "content": {"multipart/form-data": {"schema": {"type": "object", "properties": {
        "kind": {"type": "string", "enum": ["logo", "imago"]},
        "file": {"type": "string", "format": "binary"}
      }, "required": ["kind", "file"]}}}},
      "responses": {"201": {"description": "created", "content": {"application/json": {"schema": {"type": "object"}}}}}
    }},
    "/companies/{company}/invoices/{invoice}/face-submissions": {"post": {
      "operationId": "public-api.v1.invoices.face_submissions.submit",
      "parameters": [{"name": "company", "in": "path", "required": true, "schema": {"type": "string"}}, {"name": "invoice", "in": "path", "required": true, "schema": {"type": "string"}}],
      "responses": {"201": {"description": "created"}}
    }},
    "/widgets": {"post": {
      "operationId": "public-api.v1.widgets.create",
      "x-speakeasy-retries": {"strategy": "none"},
      "responses": {"201": {"description": "created"}}
    }},
    "/gadgets": {"post": {
      "operationId": "public-api.v1.gadgets.create",
      "x-speakeasy-retries": {"strategy": "backoff"},
      "responses": {"201": {"description": "created"}}
    }}
  }
}`

func loadMini(t *testing.T) map[string]Operation {
	t.Helper()
	ops, nonConforming, err := LoadFrom([]byte(miniSpec))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if len(nonConforming) > 0 {
		t.Fatalf("operationId no conformes: %v", nonConforming)
	}
	by := map[string]Operation{}
	for _, o := range ops {
		by[o.OperationID] = o
	}
	return by
}

func TestHeaderParamsComeFromTheSpecAndSkipManagedOnes(t *testing.T) {
	by := loadMini(t)

	patch := by["public-api.v1.series.assign_pdf_template"]
	if len(patch.HeaderParams) != 1 || patch.HeaderParams[0].Name != "If-Match" || patch.HeaderParams[0].Required {
		t.Fatalf("solo If-Match (Idempotency-Key, Factuarea-Version y X-Active-Profile los gestiona la CLI): %+v", patch.HeaderParams)
	}
	if !patch.IdempotencyRequired {
		t.Error("IdempotencyRequired debe seguir detectándose")
	}
	if !patch.ReturnsETag {
		t.Error("el 200 declara ETag")
	}

	download := by["public-api.v1.pdf_artifacts.download"]
	if len(download.HeaderParams) != 1 || download.HeaderParams[0].Name != "If-None-Match" || !download.HeaderParams[0].Required {
		t.Fatalf("If-None-Match declarado y requerido se conserva tal cual (sin duplicar): %+v", download.HeaderParams)
	}
	if !download.ReturnsETag {
		t.Error("el 304 declara ETag")
	}

	upload := by["public-api.v1.pdf_templates.asset_upload"]
	if len(upload.HeaderParams) != 0 || upload.ReturnsETag {
		t.Errorf("la subida solo declara Idempotency-Key: %+v", upload.HeaderParams)
	}
}

func TestA304ResponseImpliesIfNoneMatch(t *testing.T) {
	pdf := loadMini(t)["public-api.v1.invoices.pdf"]
	if len(pdf.HeaderParams) != 1 || pdf.HeaderParams[0].Name != "If-None-Match" {
		t.Fatalf("una respuesta 304 sin If-None-Match declarado lo añade: %+v", pdf.HeaderParams)
	}
	if pdf.BinaryResponse == nil || pdf.BinaryResponse.ContentType != "application/pdf" {
		t.Errorf("el 200 binario sigue siendo la respuesta binaria (el 202 JSON no la anula): %+v", pdf.BinaryResponse)
	}
	if !pdf.ReturnsETag {
		t.Error("el 200 declara ETag")
	}
}

func TestMultipartAssetUploadDeclaresFileAndKind(t *testing.T) {
	up := loadMini(t)["public-api.v1.pdf_templates.asset_upload"]
	if up.Body == nil || up.Body.Kind != "multipart" || len(up.Body.FileFields) != 1 || up.Body.FileFields[0] != "file" {
		t.Fatalf("multipart con el campo binario `file`: %+v", up.Body)
	}
}

func TestNoRetrySignals(t *testing.T) {
	by := loadMini(t)
	for id, want := range map[string]bool{
		"public-api.v1.invoices.face_submissions.submit": true,  // lista explícita
		"public-api.v1.widgets.create":                   true,  // x-speakeasy-retries.strategy: none
		"public-api.v1.gadgets.create":                   false, // backoff
		"public-api.v1.series.assign_pdf_template":       false,
	} {
		if got := by[id].NoRetry; got != want {
			t.Errorf("%s NoRetry = %v, quiero %v", id, got, want)
		}
	}
}

func TestRealSpecMarksFaceOperationsNoRetry(t *testing.T) {
	ops, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range ops {
		want := o.OperationID == "public-api.v1.invoices.face_submissions.submit" || o.OperationID == "public-api.v1.face_submissions.cancel"
		if o.NoRetry != want {
			t.Errorf("%s NoRetry = %v, quiero %v", o.OperationID, o.NoRetry, want)
		}
	}
}
