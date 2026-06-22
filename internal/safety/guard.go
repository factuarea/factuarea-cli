package safety

import (
	"fmt"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

// RequireLive exige el flag --live para operaciones mutadoras en entorno live.
// Fail-closed: cualquier entorno distinto de "test" (incluido "unknown") exige
// --live, para que una key con prefijo desconocido nunca mute sin confirmación.
func RequireLive(environment string, liveFlag bool) error {
	if environment == "test" || liveFlag {
		return nil
	}
	if environment == "live" {
		return apierr.Usagef("operación en entorno LIVE: añade --live para confirmar que NO es una prueba")
	}
	return apierr.Usagef("entorno de la API desconocido (%q): revisa tu API key o añade --live para operar", environment)
}

// RequireSandbox exige entorno sandbox (key fact_test_). Lo usa `trigger`.
func RequireSandbox(environment string) error {
	if environment != "test" {
		return apierr.Usagef("este comando solo funciona en sandbox: usa una key fact_test_ (entorno actual: %s)", environment)
	}
	return nil
}

// Confirm exige confirmación tipada del id exacto para operaciones irreversibles.
// Nunca bloquea esperando stdin: si no es TTY o es --no-input, falla de inmediato.
func Confirm(resourceID, confirmFlag string, isTTY, noInput bool, prompt func(string) (string, error)) error {
	if confirmFlag != "" {
		if confirmFlag == resourceID {
			return nil
		}
		return apierr.Usagef("--confirm=%q no coincide con %q", confirmFlag, resourceID)
	}
	if noInput || !isTTY {
		return apierr.Usagef("acción irreversible: pasa --confirm=%s para confirmarla", resourceID)
	}
	typed, err := prompt(fmt.Sprintf("Esto es IRREVERSIBLE. Escribe %q para confirmar: ", resourceID))
	if err != nil {
		return err
	}
	if strings.TrimSpace(typed) != resourceID {
		return apierr.Usagef("confirmación cancelada (no coincidió con %q)", resourceID)
	}
	return nil
}
