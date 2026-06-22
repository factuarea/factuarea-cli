package trigger

import (
	"context"
	"fmt"
	"sort"

	"github.com/factuarea/factuarea-cli/internal/client"
)

type fixture func(ctx context.Context, c *client.Client, ov map[string]string) error

var registry = map[string]fixture{}

func Supported() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func Run(ctx context.Context, c *client.Client, event string, ov map[string]string) error {
	fx, ok := registry[event]
	if !ok {
		return fmt.Errorf("evento %q no soportado por trigger. Soportados: %v", event, Supported())
	}
	return fx(ctx, c, ov)
}
