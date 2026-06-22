package config

import "regexp"

var keyRe = regexp.MustCompile(`^(fact_live_|fact_test_)[A-Za-z0-9]{24}$`)

func Environment(apiKey string) string {
	switch {
	case len(apiKey) >= 10 && apiKey[:10] == "fact_test_":
		return "test"
	case len(apiKey) >= 10 && apiKey[:10] == "fact_live_":
		return "live"
	default:
		return "unknown"
	}
}

func ValidKeyFormat(apiKey string) bool { return keyRe.MatchString(apiKey) }

// RedactKey deja visible el prefijo de entorno y los últimos 4 caracteres.
func RedactKey(apiKey string) string {
	if len(apiKey) < 14 {
		return "…"
	}
	return apiKey[:10] + "…" + apiKey[len(apiKey)-4:]
}
