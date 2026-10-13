package config

import (
	"errors"
	"strings"
	"testing"
)

type crmDeliveryProfileStore struct {
	*fakeStore
	key, failure string
	reads        []string
}

func (store *crmDeliveryProfileStore) GetKey(profile string) (string, error) {
	store.reads = append(store.reads, profile)
	if store.failure != "" {
		return store.key, errors.New(store.failure)
	}
	if store.fakeStore == nil {
		return store.key, nil
	}
	return store.fakeStore.GetKey(profile)
}

func TestCRMDeliverySelectedProfilesIsolateGenericEnvironmentKey(t *testing.T) {
	store := &fakeStore{keys: map[string]string{"a": "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa", "b": "fact_live_bbbbbbbbbbbbbbbbbbbbbbbb", "default": "fact_test_dddddddddddddddddddddddd"}}
	for _, profile := range []string{"a", "b", "default"} {
		t.Run(profile, func(t *testing.T) {
			result, err := ResolveAPIKey("", profile, env(map[string]string{EnvProfile: "b", EnvAPIKey: "fact_live_eeeeeeeeeeeeeeeeeeeeeeee"}), store)
			if err != nil || result.Source != "profile" || result.APIKey != store.keys[profile] || result.Profile != profile {
				t.Fatalf("explicit profile crossed to environment-selected or generic credential: %+v %v", result, err)
			}
		})
	}
}
func TestCRMDeliveryMissingSelectedProfileNeverFallsBackToAnotherTenant(t *testing.T) {
	store := &fakeStore{keys: map[string]string{"default": "fact_test_dddddddddddddddddddddddd"}}
	if _, err := ResolveAPIKey("", "unknown", env(map[string]string{EnvAPIKey: "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa"}), store); err == nil {
		t.Fatal("missing profile used global key")
	}
	result, err := ResolveAPIKey("", "", env(map[string]string{EnvProfile: "default", EnvAPIKey: "fact_live_eeeeeeeeeeeeeeeeeeeeeeee"}), store)
	if err != nil || result.Source != "profile" {
		t.Fatal("environment-selected profile escaped boundary")
	}
	if _, err := ResolveAPIKey("", "", env(map[string]string{EnvProfile: "missing", EnvAPIKey: "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa"}), store); err == nil {
		t.Fatal("missing environment-selected profile fell back to default or generic tenant credential")
	}
}

func TestCRMDeliverySandboxLoginKeepsProvidedCredentialBoundToExplicitProfile(t *testing.T) {
	const keyA = "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa"
	const keyB = "fact_test_bbbbbbbbbbbbbbbbbbbbbbbb"
	store := &fakeStore{keys: map[string]string{"sandbox-a": keyA, "sandbox-b": keyB}}

	result, err := ResolveAPIKey(keyB, "sandbox-b", env(map[string]string{
		EnvProfile: "sandbox-a",
		EnvAPIKey:  keyA,
	}), store)
	if err != nil {
		t.Fatal("explicit sandbox login credential was rejected")
	}
	if result.Source != "stdin" || result.Profile != "sandbox-b" || result.APIKey != keyB || result.Environment != "test" {
		t.Fatalf("sandbox login crossed to the selected or generic key: %+v", result)
	}
}

func TestCRMDeliveryUnavailableSelectedStoreNeverFallsBackOrDisclosesItsFailure(t *testing.T) {
	const genericKey = "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa"
	const foreignKey = "fact_test_bbbbbbbbbbbbbbbbbbbbbbbb"
	for _, selectedBy := range []string{"flag", "environment"} {
		for _, failure := range []struct {
			name, key, message string
		}{
			{name: "empty_profile"},
			{name: "unavailable_store", message: "locked store " + foreignKey},
			{name: "key_with_error", key: foreignKey, message: "cannot unlock " + foreignKey},
		} {
			t.Run(selectedBy+"/"+failure.name, func(t *testing.T) {
				store := &crmDeliveryProfileStore{key: failure.key, failure: failure.message}
				profile := "sandbox-b"
				values := map[string]string{EnvAPIKey: genericKey, EnvProfile: "sandbox-a"}
				if selectedBy == "environment" {
					profile = ""
					values[EnvProfile] = "sandbox-b"
				}
				result, err := ResolveAPIKey("", profile, env(values), store)
				if err == nil || result != (Resolution{}) || len(store.reads) != 1 || store.reads[0] != "sandbox-b" {
					t.Fatal("failed selected profile escaped its boundary or returned a partial credential")
				}
				if strings.Contains(err.Error(), genericKey) || strings.Contains(err.Error(), foreignKey) {
					t.Fatal("profile-resolution failure disclosed a credential")
				}
			})
		}
	}
	if result, err := ResolveAPIKey("", "sandbox-b", env(map[string]string{EnvAPIKey: genericKey}), nil); err == nil || result != (Resolution{}) {
		t.Fatal("selected profile with no store used the generic tenant key")
	}
}

func TestCRMDeliveryEnvironmentProfileSwitchResolvesEachSandboxIndependently(t *testing.T) {
	keys := map[string]string{
		"sandbox-a": "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa",
		"sandbox-b": "fact_test_bbbbbbbbbbbbbbbbbbbbbbbb",
		"default":   "fact_live_dddddddddddddddddddddddd",
	}
	store := &crmDeliveryProfileStore{fakeStore: &fakeStore{keys: keys}}
	values := map[string]string{EnvAPIKey: "fact_live_eeeeeeeeeeeeeeeeeeeeeeee"}
	for _, profile := range []string{"sandbox-a", "sandbox-b", "sandbox-a"} {
		values[EnvProfile] = profile
		result, err := ResolveAPIKey("", "", env(values), store)
		if err != nil || result.Profile != profile || result.APIKey != keys[profile] || result.Source != "profile" || result.Environment != "test" {
			t.Fatal("environment-selected sandbox reused another profile, default or live environment credential")
		}
	}
	if strings.Join(store.reads, ",") != "sandbox-a,sandbox-b,sandbox-a" {
		t.Fatal("profile switch read credentials outside the selected sandbox")
	}
}
