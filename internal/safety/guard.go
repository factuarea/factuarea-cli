package safety

import (
	"fmt"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

func RequireLive(environment string, liveFlag bool) error {
	if environment == "test" || liveFlag {
		return nil
	}
	if environment == "live" {
		return apierr.Usagef("operación en entorno LIVE: añade --live para confirmar que NO es una prueba")
	}
	return apierr.Usagef("entorno de la API desconocido (%q): revisa tu API key o añade --live para operar", environment)
}

func RequireSandbox(environment string) error {
	if environment != "test" {
		return apierr.Usagef("este comando solo funciona en sandbox: usa una key fact_test_ (entorno actual: %s)", environment)
	}
	return nil
}

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
