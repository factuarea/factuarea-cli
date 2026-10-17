package trigger

import (
	"context"
	"fmt"
	"sort"

	"github.com/factuarea/factuarea-cli/internal/client"
)

// fixture produce un evento real. `base` es el prefijo del EJE DE EMPRESA
// (`/v1/companies/<id>`) que resuelve quien invoca: desde el eje, ninguna
// escritura de la v1 cuelga de una ruta plana.
type fixture func(ctx context.Context, c *client.Client, base string, ov map[string]string) error

var registry = map[string]fixture{}

func Supported() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func Run(ctx context.Context, c *client.Client, base, event string, ov map[string]string) error {
	fx, ok := registry[event]
	if !ok {
		return fmt.Errorf("evento %q no soportado por trigger. Soportados: %v", event, Supported())
	}
	return fx(ctx, c, base, ov)
}
