package config

import (
	"errors"
	"testing"
)

type fakeStore struct{ keys map[string]string }

func (f *fakeStore) GetKey(p string) (string, error) {
	if k, ok := f.keys[p]; ok {
		return k, nil
	}
	return "", errors.New("not found")
}
func (f *fakeStore) SetKey(p, k string) error { f.keys[p] = k; return nil }
func (f *fakeStore) DeleteKey(p string) error { delete(f.keys, p); return nil }

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolvePrecedenceStdinWins(t *testing.T) {
	st := &fakeStore{keys: map[string]string{"default": "fact_test_dddddddddddddddddddddddd"}}
	r, err := ResolveAPIKey("fact_live_ssssssssssssssssssssssss", "", env(map[string]string{"FACTUAREA_API_KEY": "fact_test_eeeeeeeeeeeeeeeeeeeeeeee"}), st)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "stdin" || r.Environment != "live" {
		t.Fatalf("got %+v", r)
	}
}

func TestResolveEnvOverProfile(t *testing.T) {
	st := &fakeStore{keys: map[string]string{"default": "fact_test_dddddddddddddddddddddddd"}}
	r, err := ResolveAPIKey("", "", env(map[string]string{"FACTUAREA_API_KEY": "fact_test_eeeeeeeeeeeeeeeeeeeeeeee"}), st)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "env" {
		t.Fatalf("got %+v", r)
	}
}

func TestResolveProfileFallback(t *testing.T) {
	st := &fakeStore{keys: map[string]string{"default": "fact_test_dddddddddddddddddddddddd"}}
	r, err := ResolveAPIKey("", "", env(nil), st)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "profile" || r.Profile != "default" || r.Environment != "test" {
		t.Fatalf("got %+v", r)
	}
}

func TestResolveNoCredentials(t *testing.T) {
	st := &fakeStore{keys: map[string]string{}}
	if _, err := ResolveAPIKey("", "", env(nil), st); err == nil {
		t.Fatal("expected error when no credentials")
	}
}
