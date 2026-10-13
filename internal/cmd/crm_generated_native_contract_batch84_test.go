package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/spec"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestCRMNativeDevelopmentBatch84EveryActualExportedOperationHasOneGeneratedCommand(t *testing.T) {
	loaded, rejected, err := spec.Load()
	if err != nil || len(rejected) != 0 || len(loaded) != 883 {
		t.Fatalf("actual export incomplete: loaded=%d rejected=%v err=%v", len(loaded), rejected, err)
	}
	generated := generatedOps()
	if len(generated) != len(loaded) {
		t.Fatalf("generated=%d actual=%d", len(generated), len(loaded))
	}
	byID := map[string]genOp{}
	for _, op := range generated {
		if _, exists := byID[op.OperationID]; exists {
			t.Fatalf("duplicate published ID: %s", op.OperationID)
		}
		byID[op.OperationID] = op
	}
	root := NewRootCmd()
	var assertSiblings func(*cobra.Command)
	assertSiblings = func(parent *cobra.Command) {
		seen := map[string]bool{}
		for _, child := range parent.Commands() {
			if seen[child.Name()] {
				t.Fatalf("duplicate command: %s %s", parent.CommandPath(), child.Name())
			}
			seen[child.Name()] = true
			assertSiblings(child)
		}
	}
	assertSiblings(root)
	crm := 0
	for _, source := range loaded {
		op, exists := byID[source.OperationID]
		if !exists || op.Method != source.Method || op.Path != source.Path || op.RequiredScope != source.RequiredScope || op.IdempotencyRequired != source.IdempotencyRequired {
			t.Fatalf("published contract changed: %s", source.OperationID)
		}
		leaf, remainder, err := root.Find(append(append([]string{}, op.Groups...), op.Action))
		if err != nil || len(remainder) != 0 || leaf.Name() != op.Action {
			t.Fatalf("published command missing: %s remainder=%v err=%v", commandPath(op), remainder, err)
		}
		for _, param := range op.QueryParams {
			flagName := param.Name
			if _, native := crmOAuthGeneratedCommand(op); native && param.Name == "original_body_sha256" {
				flagName = "original-body-sha256"
			}
			if leaf.Flags().Lookup(flagName) == nil {
				t.Fatalf("query flag missing for %s: %s", op.OperationID, param.Name)
			}
		}
		if strings.HasPrefix(source.Path, "/crm/") || source.Path == "/crm" {
			crm++
			if op.Method == http.MethodGet && op.isMutating() {
				t.Fatalf("native GET became mutation: %s", op.OperationID)
			}
		}
	}
	if crm != 288 || len(crmCoreMissingOps(generated)) != 0 {
		t.Fatalf("CRM coverage incomplete: actual=%d missing core=%d", crm, len(crmCoreMissingOps(generated)))
	}
	if len(crmOAuthOriginalManifestFor(generated)) != 0 {
		t.Fatal("native generated Original readers duplicated supplementary discovery")
	}
}

func TestCRMNativeDevelopmentBatch84DiscoveryPreservesPublishedIDsAndWriterAuthorityOnReads(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"commands", "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var entries []manifestEntry
	if json.Unmarshal(out.Bytes(), &entries) != nil {
		t.Fatal("discovery is not JSON")
	}
	byCommand := map[string]manifestEntry{}
	for _, entry := range entries {
		if _, exists := byCommand[entry.Command]; exists {
			t.Fatalf("duplicate discovery command: %s", entry.Command)
		}
		byCommand[entry.Command] = entry
	}
	for _, op := range generatedOps() {
		entry, ok := byCommand[commandPath(op)]
		if !ok || entry.OperationID != op.OperationID || entry.RequiredScope != op.RequiredScope || entry.Mutating != op.isMutating() {
			t.Fatalf("discovery changed native contract: %s", op.OperationID)
		}
	}
	for _, name := range []string{"factuarea crm originals lead-create", "factuarea crm originals opportunity-create", "factuarea crm originals opportunity-stage", "factuarea crm originals activity-create"} {
		entry := byCommand[name]
		if entry.Mutating || entry.RequiredScope == "" || !strings.HasSuffix(entry.RequiredScope, ":write") {
			t.Fatalf("original read lost current writer authority: %s", name)
		}
	}
}

func TestCRMNativeDevelopmentBatch84GeneratedCreatePreservesActualFirstWireAndSingleSend(t *testing.T) {
	wires := map[string][]byte{
		"crmCreateLead":        []byte(" \n{\"name\":\"Àngela 山\",\"notes\":null,\"source\":\"manual\"}\n"),
		"crmCreateOpportunity": []byte(" \n{\"title\":\"Àngela 山\",\"amount\":\"9000000000000000.0001\",\"currency\":\"EUR\",\"pipeline_id\":\"01900000-0000-7000-8000-000000000002\",\"stage_id\":\"01900000-0000-7000-8000-000000000003\",\"lead_id\":null}\n"),
		"crmCreateCrmActivity": []byte(" \n{\"type\":\"note\",\"title\":\"Àngela 山\",\"body\":null,\"target\":{\"type\":\"lead\",\"id\":\"01900000-0000-7000-8000-000000000001\"},\"timezone\":\"Europe/Madrid\"}\n"),
	}
	for _, id := range []string{"crmCreateLead", "crmCreateOpportunity", "crmCreateCrmActivity"} {
		t.Run(id, func(t *testing.T) {
			wire := wires[id]
			var op genOp
			for _, current := range generatedOps() {
				if current.OperationID == id {
					op = current
				}
			}
			if op.OperationID != id {
				t.Fatal("native create unavailable")
			}
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet && r.URL.Path == "/v1/account" {
					fmt.Fprintf(w, `{"data":{"api_key":{"scopes":[%q]}}}`, op.RequiredScope)
					return
				}
				writes++
				actual, _ := io.ReadAll(r.Body)
				if r.Method != http.MethodPost || r.URL.Path != "/v1"+op.Path || r.Header.Get("Idempotency-Key") != "exact first key" || !bytes.Equal(actual, wire) {
					t.Error("generated create changed original wire, native route or key")
				}
				w.WriteHeader(http.StatusCreated)
				fmt.Fprint(w, `{"data":{"id":"01900000-0000-7000-8000-000000000001","version":1,"status":"open"}}`)
			}))
			t.Cleanup(server.Close)
			crmDeliveryEnv(t, server.URL)
			root := NewRootCmd()
			args := append([]string{"--json"}, op.Groups...)
			args = append(args, op.Action, "--idempotency-key", "exact first key", "-d", string(wire))
			root.SetArgs(args)
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			if err := root.Execute(); err != nil || writes != 1 {
				t.Fatalf("native generated create failed: writes=%d err=%v", writes, err)
			}
		})
	}
}

func TestCRMNativeDevelopmentBatch84NativeCommandHelpNeedsNoCredential(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "")
	t.Setenv("FACTUAREA_PROFILE", "")
	for _, path := range [][]string{{"crm-leads", "create"}, {"crm-opportunities", "move-stage"}, {"crm-activities", "transition"}, {"crm", "originals", "activity-create"}, {"crm-whatsapp-cloud", "templates"}, {"crm", "sequences", "list"}} {
		t.Run(strings.Join(path, "/"), func(t *testing.T) {
			root := NewRootCmd()
			root.SetArgs(append(append([]string{}, path...), "--help"))
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			if err := root.Execute(); err != nil || out.Len() == 0 {
				t.Fatalf("native command help unavailable: %v", err)
			}
		})
	}
}

func TestCRMNativeDevelopmentBatch84BodyIdempotencyKeyIsDistinctFromTransportHeader(t *testing.T) {
	root := NewRootCmd()
	var op genOp
	for _, current := range generatedOps() {
		if current.OperationID == "crmSubmitAuthenticatedCapture" {
			op = current
		}
	}
	if op.OperationID == "" {
		t.Fatal("actual capture operation absent")
	}
	command, _, err := root.Find(append(append([]string{}, op.Groups...), op.Action))
	if err != nil || command.Flags().Lookup("body-idempotency-key") == nil || command.Flags().Lookup("idempotency-key") == nil {
		t.Fatal("actual body/header keys unavailable separately")
	}
	if err := command.Flags().Set("body-idempotency-key", "native payload original key"); err != nil {
		t.Fatal(err)
	}
	if err := command.Flags().Set("idempotency-key", "HTTP transport original key"); err != nil {
		t.Fatal(err)
	}
	for _, flag := range collectFieldFlags(op.Body.Fields, nil) {
		if len(flag.jsonPath) == 1 && flag.jsonPath[0] == "idempotency_key" {
			if flag.flagName != "body-idempotency-key" {
				t.Fatal("payload key reuses the header flag")
			}
			value, _ := command.Flags().GetString(flag.flagName)
			if value != "native payload original key" {
				t.Fatal("payload key was replaced by the header")
			}
		}
	}
}

func TestCRMNativeDevelopmentBatch84NativeReaderDiscoveryDescribesExistingConstructorFlags(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"commands", "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var entries []manifestEntry
	if err := json.Unmarshal(out.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	byID := map[string]manifestEntry{}
	for _, entry := range entries {
		byID[entry.OperationID] = entry
	}
	nativeCount := 0
	for _, op := range generatedOps() {
		constructor, native := crmOAuthGeneratedCommand(op)
		if !native {
			continue
		}
		nativeCount++
		entry, ok := byID[op.OperationID]
		if !ok || entry.RequiredScope != op.RequiredScope || entry.Mutating {
			t.Fatalf("published native reader changed: %s", op.OperationID)
		}
		actual := map[string]string{}
		for _, flag := range entry.Flags {
			if _, exists := actual[flag.Name]; exists {
				t.Fatalf("duplicate native reader flag: %s", flag.Name)
			}
			actual[flag.Name] = flag.Type
		}
		expectedCount := 0
		constructor.Flags().VisitAll(func(flag *pflag.Flag) {
			expectedCount++
			if actual[flag.Name] != flag.Value.Type() {
				t.Errorf("discovery changed constructor flag: %s --%s", op.OperationID, flag.Name)
			}
		})
		if len(actual) != expectedCount || actual["oauth-token-env"] != "string" {
			t.Fatalf("native reader options incomplete: %s", op.OperationID)
		}
	}
	if nativeCount < 4 || nativeCount > 5 {
		t.Fatalf("native original constructor coverage=%d", nativeCount)
	}
}
