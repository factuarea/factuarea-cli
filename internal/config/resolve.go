package config

import "errors"

type Store interface {
	GetKey(profile string) (string, error)
	SetKey(profile, key string) error
	DeleteKey(profile string) error
}

type Resolution struct {
	APIKey      string
	Source      string
	Profile     string
	Environment string
}

const (
	EnvAPIKey  = "FACTUAREA_API_KEY"
	EnvProfile = "FACTUAREA_PROFILE"
)

func ResolveAPIKey(stdinKey, profile string, getenv func(string) string, store Store) (Resolution, error) {
	selectedProfile := profile != "" || getenv(EnvProfile) != ""
	if profile == "" {
		profile = getenv(EnvProfile)
	}
	if profile == "" {
		profile = "default"
	}

	if stdinKey != "" {
		return mk(stdinKey, "stdin", profile), nil
	}
	// A selected profile is an explicit credential boundary. The generic env key
	// remains convenient only when no profile was selected (flag or environment).
	if v := getenv(EnvAPIKey); v != "" && !selectedProfile {
		return mk(v, "env", profile), nil
	}
	if store != nil {
		if k, err := store.GetKey(profile); err == nil && k != "" {
			return mk(k, "profile", profile), nil
		}
	}
	return Resolution{}, errors.New("no hay credenciales: ejecuta `factuarea login` o define FACTUAREA_API_KEY")
}

func mk(key, source, profile string) Resolution {
	return Resolution{APIKey: key, Source: source, Profile: profile, Environment: Environment(key)}
}
