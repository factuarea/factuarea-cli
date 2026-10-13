package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/spec"
	"github.com/spf13/cobra"
)

// These five descriptors reproduce the current owner contributor/route
// contracts. They test the existing command builder without claiming an API
// export, native OAuth issuer proof or an application effect.
func nativeCRMGeneratedReadDescriptors(t *testing.T) []genOp {
	t.Helper()
	groups, action, ok := spec.Resolve("crmGetOAuthCredentialContext")
	if !ok {
		t.Fatal("native credential context ID unavailable")
	}
	ops := []genOp{{OperationID: "crmGetOAuthCredentialContext", Method: http.MethodGet, Path: "/crm/credential-context", RequiredScope: "crm_access_context:read", Groups: groups, Action: action}}
	for _, native := range crmOAuthOriginalOperations {
		groups, action, ok = spec.Resolve(native.nativeID)
		if !ok {
			t.Fatal("native original ID unavailable")
		}
		op := genOp{OperationID: native.nativeID, Method: http.MethodGet, Path: strings.TrimPrefix(native.path, "/v1"), RequiredScope: native.scope, IdempotencyRequired: true, Groups: groups, Action: action,
			QueryParams: []genParam{{Name: "original_body_sha256", In: "query", Type: "string", Required: true}}}
		if native.stage {
			op.PathParams = []genParam{{Name: "opportunity", In: "path", Type: "string", Required: true}}
		}
		ops = append(ops, op)
	}
	return ops
}

func nativeCRMGeneratedReadRoot(t *testing.T, generated []genOp) *cobra.Command {
	t.Helper()
	root := NewRootCmd()
	crm, _, err := root.Find([]string{"crm"})
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range crm.Commands() {
		if child.Name() == "credential-context" || child.Name() == "originals" {
			crm.RemoveCommand(child)
		}
	}
	var originals *cobra.Command
	for _, op := range generated {
		if len(op.Groups) == 1 {
			crm.AddCommand(buildGeneratedCommand(op))
			continue
		}
		if originals == nil {
			originals = serviceLevelGroup("originals", "Original native reads")
			crm.AddCommand(originals)
		}
		originals.AddCommand(buildGeneratedCommand(op))
	}
	registerCRMOAuthOriginalCommandsFor(root, generated)
	return root
}

func TestNativeCRMGeneratedReadsNeverBecomeMutationsUnderWriterScopes(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		for _, scope := range []string{"crm_leads:write", "crm_opportunities:write", "crm_activities:write", "conversations:write", "crm_activities:delete", ""} {
			t.Run(method+"/"+scope, func(t *testing.T) {
				if (genOp{Method: method, RequiredScope: scope}).isMutating() {
					t.Fatal("current writer authority turned an HTTP read into a mutation")
				}
			})
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if !(genOp{Method: method, RequiredScope: "crm_leads:write"}).isMutating() {
			t.Fatalf("write lost its native guard: %s", method)
		}
	}
}

func TestNativeCRMGeneratedOriginalsKeepSingleRegistrationAndDiscovery(t *testing.T) {
	all := nativeCRMGeneratedReadDescriptors(t)
	for _, count := range []int{0, 1, 2, len(all)} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			root := nativeCRMGeneratedReadRoot(t, all[:count])
			crm, _, _ := root.Find([]string{"crm"})
			contextCount, originalsCount := 0, 0
			for _, child := range crm.Commands() {
				if child.Name() == "credential-context" {
					contextCount++
				}
				if child.Name() == "originals" {
					originalsCount++
				}
			}
			if contextCount != 1 || originalsCount != 1 {
				t.Fatalf("duplicate native groups: context=%d originals=%d", contextCount, originalsCount)
			}
			originals, _, _ := root.Find([]string{"crm", "originals"})
			if len(originals.Commands()) != 4 {
				t.Fatalf("original commands=%d", len(originals.Commands()))
			}
			for _, op := range all {
				leaf, _, err := root.Find(append(append([]string{}, op.Groups...), op.Action))
				if err != nil || leaf.Name() != op.Action || leaf.Flags().Lookup("oauth-token-env") == nil || leaf.Flags().Lookup("confirm") != nil || leaf.Flags().Lookup("data") != nil {
					t.Fatalf("native read constructor lost: %s", op.OperationID)
				}
			}
			missing := crmOAuthOriginalManifestFor(all[:count])
			if len(missing)+count != len(all) {
				t.Fatal("generated and supplementary discovery overlap")
			}
			for _, entry := range missing {
				if entry.Mutating || entry.Irreversible {
					t.Fatal("supplementary original discovery claims a write")
				}
			}
		})
	}
}

func TestNativeCRMGeneratedOriginalsUseExactRetainedKeyHashAndCurrentBearer(t *testing.T) {
	all := nativeCRMGeneratedReadDescriptors(t)
	for _, op := range all[1:] {
		t.Run(op.OperationID, func(t *testing.T) {
			wire := []byte(" \n{\"notes\":null,\"amount\":\"90000000000000.0001\",\"name\":\"Àngela 山\"}\n")
			sum := sha256.Sum256(wire)
			digest := hex.EncodeToString(sum[:])
			calls := 0
			target := "01900000-0000-7000-8000-000000000001"
			path := "/v1" + strings.ReplaceAll(op.Path, "{opportunity}", target)
			// Activity's factual confirmed receipt can preserve a null private
			// representation. The client retains the exact first response string.
			original := " { \"data\": null, \"meta\": {\"representation_available\":false} } \n"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != path || r.URL.RawQuery != "original_body_sha256="+digest || r.Header.Get("Idempotency-Key") != "retained original key" || r.Header.Get("Authorization") != "Bearer actual-current-native-bearer" || r.ContentLength != 0 {
					t.Error("native read changed original intention, issuer bearer or verb")
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"state": "confirmed", "original_body_sha256": digest, "original_response": map[string]any{"status": 201, "body": original}}, "meta": map[string]any{"authorization_version": "current-native-c7"}})
			}))
			t.Cleanup(server.Close)
			t.Setenv(envBaseURL, server.URL)
			t.Setenv("FACTUAREA_API_KEY", "")
			t.Setenv("FACTUAREA_PROFILE", "")
			t.Setenv("CRM_BATCH84_NATIVE_BEARER", "actual-current-native-bearer")
			root := nativeCRMGeneratedReadRoot(t, all)
			args := append([]string{"--json"}, op.Groups...)
			args = append(args, op.Action)
			if len(op.PathParams) != 0 {
				args = append(args, target)
			}
			args = append(args, "--original-body-sha256", digest, "--idempotency-key", "retained original key", "--oauth-token-env", "CRM_BATCH84_NATIVE_BEARER")
			root.SetArgs(args)
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			if err := root.Execute(); err != nil || calls != 1 {
				t.Fatalf("native generated reader failed or redispatched: calls=%d err=%v", calls, err)
			}
			var response struct {
				Data struct {
					Original struct {
						Body string `json:"body"`
					} `json:"original_response"`
				} `json:"data"`
			}
			if json.Unmarshal(out.Bytes(), &response) != nil || response.Data.Original.Body != original {
				t.Fatal("immutable private original response was reconstructed")
			}
		})
	}
}

func TestNativeCRMGeneratedOriginalContractDriftFailsBeforeHTTP(t *testing.T) {
	all := nativeCRMGeneratedReadDescriptors(t)
	cases := map[string]func(*genOp){
		"verb":            func(op *genOp) { op.Method = http.MethodPost },
		"path":            func(op *genOp) { op.Path += "/borrowed" },
		"scope":           func(op *genOp) { op.RequiredScope = "crm_leads:read" },
		"body":            func(op *genOp) { op.Body = &genBody{Kind: "json"} },
		"new_key":         func(op *genOp) { op.IdempotencyRequired = !op.IdempotencyRequired },
		"digest_optional": func(op *genOp) { op.QueryParams = []genParam{{Name: "original_body_sha256", Type: "string"}} },
		"extra_query": func(op *genOp) {
			op.QueryParams = append(append([]genParam{}, op.QueryParams...), genParam{Name: "family_id", Type: "string"})
		},
		"confirm":   func(op *genOp) { op.Irreversible = true },
		"namespace": func(op *genOp) { op.Groups = []string{"borrowed"} },
		"action":    func(op *genOp) { op.Action = "retry-write" },
		"target":    func(op *genOp) { op.PathParams = []genParam{{Name: "other", Required: true, Type: "string"}} },
	}
	for _, original := range all {
		for name, change := range cases {
			t.Run(original.OperationID+"/"+name, func(t *testing.T) {
				op := original
				change(&op)
				defer func() {
					if recover() == nil {
						t.Fatal("native descriptor drift fell through to generic dispatch")
					}
				}()
				buildGeneratedCommand(op)
			})
		}
	}
}
