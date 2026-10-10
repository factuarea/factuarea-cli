package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

const knowledgeCAS = "9007199254740993"

func knowledgeOps() []genOp {
	var ops []genOp
	for _, op := range generatedOps() {
		if op.NativeContract != nil {
			ops = append(ops, op)
		}
	}
	return ops
}

func knowledgeIntent(native string) string {
	switch native {
	case "knowledge_articles.create":
		return ` {"expected_version":0,"title":"Guía <CRM>","body":"Texto revisado","editorial_locale":"es","category_ids":[],"slug":"crm"} `
	case "knowledge_articles.save":
		return ` {"expected_version":` + knowledgeCAS + `,"title":"Guía <CRM>","body":"Texto revisado","editorial_locale":"es","category_ids":[]} `
	case "knowledge_articles.category_save":
		return ` {"taxonomy_id":"` + crmTestOtherID + `","expected_taxonomy_version":` + knowledgeCAS + `,"expected_version":null,"name":"General","slug":"general","parent_id":null,"visibility":"internal","status":"active"} `
	case "knowledge_articles.publish":
		return ` {"expected_version":` + knowledgeCAS + `,"published_version":4,"audience":"public"} `
	case "knowledge_articles.link":
		return ` {"expected_version":` + knowledgeCAS + `,"target_type":"ticket","target_id":"` + crmTestOtherID + `","target_version":` + knowledgeCAS + `} `
	case "public_help_centers.publish":
		return ` {"confirmed":true,"expected_version":0,"slug":"crm-help","display_name":"Ayuda <CRM>","locale":"es","article_slugs":[]} `
	case "public_help_centers.unpublish":
		return ` {"confirmed":true,"expected_version":` + knowledgeCAS + `} `
	default:
		return ` {"expected_version":` + knowledgeCAS + `} `
	}
}

func knowledgeResponse(op genOp) (int, string) {
	var schema map[string]any
	code := 200
	if op.NativeContract.ResponseSchemas["200"] == "" {
		code = 201
	}
	_ = decodeKnowledgeJSON([]byte(op.NativeContract.ResponseSchemas[strconv.Itoa(code)]), &schema)
	properties := schema["properties"].(map[string]any)
	dataSchema := properties["data"].(map[string]any)
	dataProps := dataSchema["properties"].(map[string]any)
	if operation, receipt := dataProps["operation"].(map[string]any); receipt {
		native := operation["const"].(string)
		original, _ := knowledgeJSON([]byte(knowledgeIntent(native)))
		intent := original.(map[string]any)
		expected, _ := knowledgeInt(intent["expected_version"])
		snapshot := map[string]any{"id": crmTestID, "version": expected + 1}
		data := map[string]any{"operation": native, "effect_id": crmTestOtherID, "expected_version": intent["expected_version"], "confirmed": true}
		if strings.HasPrefix(native, "public_help_centers.") {
			snapshot["slug"] = "crm-help"
			snapshot["display_name"] = "Ayuda"
			snapshot["locale"] = "es"
			snapshot["article_slugs"] = []any{}
			snapshot["enabled"] = native == "public_help_centers.publish"
			data["center"] = snapshot
		} else if native == "knowledge_articles.category_save" {
			taxCAS, _ := knowledgeInt(intent["expected_taxonomy_version"])
			data["expected_taxonomy_version"] = intent["expected_taxonomy_version"]
			snapshot["taxonomy_id"] = crmTestOtherID
			snapshot["taxonomy_version"] = taxCAS + 1
			snapshot["parent_id"] = nil
			data["category"] = snapshot
		} else {
			data["article"] = snapshot
		}
		body, _ := json.Marshal(map[string]any{"data": data})
		return code, string(body)
	}
	var data any
	switch op.CrmOperation {
	case "public_help_centers.administration_get":
		data = map[string]any{"center": nil}
	case "knowledge_articles.show":
		data = map[string]any{"id": crmTestID, "version": json.Number(knowledgeCAS)}
	case "knowledge_articles.search":
		data = map[string]any{"items": []any{map[string]any{"id": crmTestID, "version": 1}}, "total": 1, "page": 1, "per_page": 25}
	case "knowledge_articles.suggest":
		data = map[string]any{"items": []any{}, "total": 0, "ticket_id": crmTestID, "ticket_version": json.Number(knowledgeCAS), "audience": "internal"}
	case "knowledge_articles.categories":
		data = map[string]any{"items": []any{}, "total": 0}
	case "knowledge_articles.versions":
		data = map[string]any{"items": []any{map[string]any{"id": crmTestID, "version": 1, "revision_number": 1}}}
	default:
		panic(op.CrmOperation)
	}
	body, _ := json.Marshal(map[string]any{"data": data})
	return code, string(body)
}

func knowledgeArgs(op genOp, intent bool) []string {
	args := append(append([]string{}, op.Groups...), op.Action)
	for range op.PathParams {
		args = append(args, crmTestID)
	}
	args = append(args, "--json", "--skip-scope-check")
	if op.CrmOperation == "knowledge_articles.suggest" {
		args = append(args, "--ticket_version", knowledgeCAS)
	}
	if op.IdempotencyRequired {
		args = append(args, "--idempotency-key", crmTestKey)
	}
	if intent && op.isMutating() {
		args = append(args, "-d", knowledgeIntent(op.CrmOperation), "--confirm", op.confirmResourceID([]string{crmTestID}))
	}
	return args
}

func TestCRMKnowledge28ActualHTTPBindings(t *testing.T) {
	ops := knowledgeOps()
	if len(ops) != 28 {
		t.Fatalf("new native bindings=%d", len(ops))
	}
	for _, op := range ops {
		t.Run(op.CrmOperation, func(t *testing.T) {
			calls := 0
			status, response := knowledgeResponse(op)
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != op.Method || r.URL.Path != op.buildPath([]string{crmTestID}) {
					t.Errorf("method/path changed: %s %s", r.Method, r.URL.Path)
				}
				body, _ := io.ReadAll(r.Body)
				if op.isMutating() && string(body) != knowledgeIntent(op.CrmOperation) {
					t.Errorf("original bytes changed: %s", body)
				}
				if !op.isMutating() && len(body) != 0 {
					t.Errorf("GET dispatched intent body")
				}
				if op.IdempotencyRequired && r.Header.Get("Idempotency-Key") != crmTestKey {
					t.Errorf("original key changed")
				}
				if r.URL.Query().Has("Idempotency-Key") {
					t.Errorf("key escaped into query")
				}
				if op.CrmOperation == "knowledge_articles.suggest" && r.URL.Query().Get("ticket_version") != knowledgeCAS {
					t.Errorf("CAS query changed")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, response)
			}))
			defer s.Close()
			out, stderr, err := crmRun(t, s, knowledgeArgs(op, true)...)
			if err != nil || calls != 1 || strings.TrimSpace(out) != response || stderr != "" {
				t.Fatalf("calls=%d err=%v out=%s stderr=%s", calls, err, out, stderr)
			}
		})
	}
}

func TestCRMKnowledgeDiscoverySchemasScopesAndOriginalReceiptFlags(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"commands", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var entries []manifestEntry
	if err := json.Unmarshal(out.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	count := 0
	commands := map[string]bool{}
	for _, entry := range entries {
		if len(entry.ResponseSchemas) == 0 {
			continue
		}
		count++
		if commands[entry.Command] {
			t.Fatalf("duplicate command %s", entry.Command)
		}
		commands[entry.Command] = true
		if len(entry.RequiredScopes) != 2 || !strings.HasPrefix(entry.RequiredScopes[0], "customer_service:") {
			t.Fatalf("native all-of scopes missing %+v", entry)
		}
		if entry.Paginated {
			t.Fatalf("undeclared continuation invented: %s", entry.Command)
		}
		if entry.IdempotencyRequired {
			found := false
			for _, flag := range entry.Flags {
				found = found || flag.Name == "idempotency-key"
			}
			if !found {
				t.Fatalf("original receipt key flag absent %s", entry.Command)
			}
		}
		if entry.Mutating && (len(entry.RequestSchema) == 0 || !entry.Irreversible || entry.MaxBodyBytes < 1) {
			t.Fatalf("intent metadata absent %+v", entry)
		}
	}
	if count != 28 {
		t.Fatalf("new discovery entries=%d", count)
	}
}

func TestCRMKnowledgeOriginalReceiptRequiresCallerKeyBeforeNetwork(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer s.Close()
	for _, op := range knowledgeOps() {
		if op.isMutating() || !op.IdempotencyRequired {
			continue
		}
		t.Run(op.CrmOperation, func(t *testing.T) {
			args := append(append([]string{}, op.Groups...), op.Action)
			_, _, err := crmRun(t, s, args...)
			if exit.ForError(err) != exit.Usage {
				t.Fatalf("receipt without original key: %v", err)
			}
		})
	}
	if calls != 0 {
		t.Fatalf("missing receipt key made %d calls", calls)
	}
}

func TestCRMKnowledgeHumanConfirmationDoesNotFollowBodyConfirmation(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer s.Close()
	for _, op := range knowledgeOps() {
		if !op.isMutating() {
			continue
		}
		t.Run(op.CrmOperation, func(t *testing.T) {
			args := knowledgeArgs(op, false)
			args = append(args, "--no-input", "-d", knowledgeIntent(op.CrmOperation))
			_, _, err := crmRun(t, s, args...)
			if exit.ForError(err) != exit.Usage {
				t.Fatalf("human confirmation missing: %v", err)
			}
		})
	}
	if calls != 0 {
		t.Fatalf("unconfirmed intent made %d calls", calls)
	}
}

func TestCRMKnowledgeCompositeScopesAreAllRequired(t *testing.T) {
	for _, native := range []string{"knowledge_articles.show", "public_help_centers.administration_get"} {
		for _, op := range knowledgeOps() {
			if op.CrmOperation != native {
				continue
			}
			for _, scopes := range [][]string{{op.RequiredScopes[0]}, {op.RequiredScopes[1]}} {
				t.Run(native+scopes[0], func(t *testing.T) {
					calls := 0
					s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path == "/v1/account" {
							crmAccount(w, scopes...)
							return
						}
						calls++
					}))
					defer s.Close()
					args := append(append([]string{}, op.Groups...), op.Action)
					for range op.PathParams {
						args = append(args, crmTestID)
					}
					args = append(args, "--json")
					_, stderr, err := crmRun(t, s, args...)
					if exit.ForError(err) != exit.Perm || calls != 0 || !strings.Contains(stderr, "scope") {
						t.Fatalf("all-of scope weakened calls=%d err=%v %s", calls, err, stderr)
					}
				})
			}
		}
	}
}

func TestCRMKnowledgeClosedIntentQueryAndCASRejectedLocally(t *testing.T) {
	cases := []struct{ group, action, body string }{
		{"knowledge-articles", "create", strings.Replace(knowledgeIntent("knowledge_articles.create"), `"expected_version":0`, `"expected_version":1`, 1)},
		{"knowledge-articles", "create", strings.Replace(knowledgeIntent("knowledge_articles.create"), `"slug":"crm"`, `"slug":"crm","company_id":"other"`, 1)},
		{"knowledge-articles", "create", strings.Replace(knowledgeIntent("knowledge_articles.create"), `"category_ids":[]`, `"category_ids":["123"]`, 1)},
		{"knowledge-categories", "save", strings.Replace(knowledgeIntent("knowledge_articles.category_save"), `"expected_version":null`, `"expected_version":2`, 1)},
		{"knowledge-categories", "save", strings.Replace(knowledgeIntent("knowledge_articles.category_save"), `"expected_version":null`, `"expected_version":null,"id":"`+crmTestID+`"`, 1)},
		{"public-help-center", "publish", strings.Replace(knowledgeIntent("public_help_centers.publish"), `"confirmed":true`, `"confirmed":false`, 1)},
		{"public-help-center", "publish", strings.Replace(knowledgeIntent("public_help_centers.publish"), `"expected_version":0`, `"expected_version":0,"id":"`+crmTestID+`"`, 1)},
		{"public-help-center", "publish", strings.Replace(knowledgeIntent("public_help_centers.publish"), `"article_slugs":[]`, `"article_slugs":["crm","crm"]`, 1)},
	}
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer s.Close()
	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			_, _, err := crmRun(t, s, "crm", c.group, c.action, "-d", c.body, "--dry-run")
			if exit.ForError(err) != exit.Usage {
				t.Fatalf("invalid native body accepted %v", err)
			}
		})
	}
	for _, query := range []string{"0", "9223372036854775808", "1.1"} {
		_, _, err := crmRun(t, s, "crm", "knowledge-articles", "suggest", crmTestID, "--ticket_version", query, "--json")
		if exit.ForError(err) != exit.Usage {
			t.Fatalf("invalid query CAS accepted %s %v", query, err)
		}
	}
	_, _, err := crmRun(t, s, "crm", "knowledge-articles", "search", "--locale", "fr", "--json")
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("invalid query enum accepted %v", err)
	}
	if calls != 0 {
		t.Fatalf("invalid input made %d calls", calls)
	}
}

func TestCRMKnowledgeTypedCASNullableCategoryAndExactFileBytes(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("dry run called network") }))
	defer s.Close()
	out, _, err := crmRun(t, s, "crm", "knowledge-articles", "create", "--expected-version", "0", "--title", "Guía", "--body", "Texto", "--editorial-locale", "es", "--category-ids", "", "--slug", "crm", "--dry-run")
	if err != nil || !strings.Contains(out, `"category_ids":[]`) {
		t.Fatalf("empty native enrollment cannot be represented %v %s", err, out)
	}
	for _, native := range []string{"knowledge_articles.save", "knowledge_articles.category_save", "public_help_centers.publish"} {
		for _, op := range knowledgeOps() {
			if op.CrmOperation != native {
				continue
			}
			t.Run(native, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "intent.json")
				intent := knowledgeIntent(native) + "\n"
				if err := os.WriteFile(path, []byte(intent), 0600); err != nil {
					t.Fatal(err)
				}
				args := append(append([]string{}, op.Groups...), op.Action)
				for range op.PathParams {
					args = append(args, crmTestID)
				}
				args = append(args, "--data-file", path, "--dry-run")
				out, _, err := crmRun(t, s, args...)
				if err != nil || out != intent+"\n" {
					t.Fatalf("file bytes/CAS/null changed %v %q", err, out)
				}
			})
		}
	}
}

func TestCRMKnowledgeUnconfirmedReceiptNeverPromotesLatestHead(t *testing.T) {
	var op genOp
	for _, candidate := range knowledgeOps() {
		if candidate.CrmOperation == "knowledge_articles.save" {
			op = candidate
		}
	}
	_, valid := knowledgeResponse(op)
	for _, bad := range []string{strings.Replace(valid, `"version":9007199254740994`, `"version":9007199254740995`, 1), strings.Replace(valid, `"confirmed":true`, `"confirmed":false`, 1), strings.Replace(valid, `"operation":"knowledge_articles.save"`, `"operation":"knowledge_articles.create"`, 1), strings.Replace(valid, crmTestID, crmTestOtherID, 1), `{"data":{"id":"` + crmTestID + `"}}`} {
		t.Run(bad, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, bad)
			}))
			defer s.Close()
			out, stderr, err := crmRun(t, s, knowledgeArgs(op, true)...)
			if calls != 1 || out != "" || exit.ForError(err) != exit.Network || !strings.Contains(stderr, crmTestKey) || !strings.Contains(stderr, "cli_mutation_unconfirmed") {
				t.Fatalf("false receipt accepted calls=%d err=%v out=%s stderr=%s", calls, err, out, stderr)
			}
		})
	}
}

func TestCRMKnowledgeNativeErrorsKeepDetailsKeyAndSingleAttempt(t *testing.T) {
	var op genOp
	for _, candidate := range knowledgeOps() {
		if candidate.CrmOperation == "public_help_centers.publish" {
			op = candidate
		}
	}
	for _, c := range []struct {
		status int
		typ    string
		code   int
	}{{403, "authorization_error", exit.Perm}, {404, "not_found_error", exit.NotFound}, {409, "conflict_error", exit.Conflict}, {422, "invalid_request_error", exit.Validation}, {429, "rate_limit_error", exit.RateLimit}, {503, "service_unavailable_error", exit.Server}} {
		t.Run(strconv.Itoa(c.status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Idempotency-Key") != crmTestKey {
					t.Error("original key changed")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, `{"error":{"type":"`+c.typ+`","code":"native_guard","message":"Denied","details":{"expected_version":`+knowledgeCAS+`,"reason":"original"}}}`)
			}))
			defer s.Close()
			out, stderr, err := crmRun(t, s, knowledgeArgs(op, true)...)
			if calls != 1 || out != "" || exit.ForError(err) != c.code || !strings.Contains(stderr, knowledgeCAS) || !strings.Contains(stderr, "native_guard") {
				t.Fatalf("native error changed calls=%d err=%v out=%s stderr=%s", calls, err, out, stderr)
			}
		})
	}
}
