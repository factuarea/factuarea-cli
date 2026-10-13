package spec

import (
	"reflect"
	"testing"
)

func TestNativeCRMOriginalOperationIDsUseExistingCommandPaths(t *testing.T) {
	cases := []struct {
		id     string
		groups []string
		action string
	}{
		{"crmGetOAuthCredentialContext", []string{"crm"}, "credential-context"},
		{"crmGetLeadCreateOriginal", []string{"crm", "originals"}, "lead-create"},
		{"crmGetOpportunityCreateOriginal", []string{"crm", "originals"}, "opportunity-create"},
		{"crmGetOpportunityStageOriginal", []string{"crm", "originals"}, "opportunity-stage"},
		{"crmGetCrmActivityCreateOriginal", []string{"crm", "originals"}, "activity-create"},
	}
	for _, test := range cases {
		t.Run(test.id, func(t *testing.T) {
			groups, action, ok := Resolve(test.id)
			if !ok || action != test.action || !reflect.DeepEqual(groups, test.groups) {
				t.Fatalf("Resolve(%q) = (%v,%q,%v)", test.id, groups, action, ok)
			}
		})
	}
}

func TestNativeCRMOriginalOperationIDsDoNotAdmitGuessedAliases(t *testing.T) {
	for _, id := range []string{"crmGetLeadOriginal", "crmGetOpportunityOriginal", "crmGetCrmActivityOriginal", "crmGetOAuthContext", "crmgetLeadCreateOriginal", "futureNativeCRMRead"} {
		t.Run(id, func(t *testing.T) {
			groups, action, ok := Resolve(id)
			if ok || groups != nil || action != "" {
				t.Fatalf("unqualified native ID accepted: %q", id)
			}
		})
	}
}
