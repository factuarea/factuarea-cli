package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/webhook"
	"github.com/spf13/cobra"
)

func newListenCmd() *cobra.Command {
	var forwardTo, events string
	var allowRemote, printJSON bool
	var pollInterval, exitAfter time.Duration
	c := &cobra.Command{
		Use:   "listen",
		Short: "Reenvía los eventos webhook a un endpoint local (devloop)",
		Args:  UsageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			g := globalsFrom(cmd)
			if err := validateForwardTo(forwardTo, allowRemote); err != nil {
				return err
			}
			cc, err := newCLIContext(g, "")
			if err != nil {
				return err
			}
			secret := webhook.GenerateSecret()
			fmt.Fprintf(cmd.ErrOrStderr(), "Reenviando eventos a %s\nSecret de firma (configúralo en tu verificador): %s\n", forwardTo, secret)

			filter := parseEventsFilter(events)
			fwd := &http.Client{Timeout: 10 * time.Second}

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			if exitAfter > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, exitAfter)
				defer cancel()
			}

			watermark, err := latestEventID(ctx, cc)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Escuchando eventos nuevos (Ctrl-C para parar)\n")

			ticker := time.NewTicker(pollInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
					watermark, err = drainNewEvents(ctx, cc, fwd, watermark, filter, secret, forwardTo, printJSON, cmd)
					if err != nil && ctx.Err() == nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "aviso: %v\n", err)
					}
				}
			}
		},
	}
	c.Flags().StringVar(&forwardTo, "forward-to", "", "URL local a la que reenviar los eventos (obligatorio)")
	c.Flags().StringVar(&events, "events", "", "lista de tipos a reenviar (coma-separada); vacío = todos")
	c.Flags().BoolVar(&allowRemote, "allow-remote-forward", false, "permite reenviar a hosts no-loopback (envía datos reales del tenant)")
	c.Flags().BoolVar(&printJSON, "print-json", false, "imprime el cuerpo de cada evento reenviado")
	c.Flags().DurationVar(&pollInterval, "poll-interval", 2*time.Second, "intervalo de sondeo del feed de eventos")
	c.Flags().DurationVar(&exitAfter, "exit-after", 0, "para automáticamente tras esta duración (0 = nunca)")
	_ = c.MarkFlagRequired("forward-to")
	return c
}

func validateForwardTo(raw string, allowRemote bool) error {
	if raw == "" {
		return apierr.Usagef("--forward-to es obligatorio")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return apierr.Usagef("--forward-to no es una URL válida: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return apierr.Usagef("--forward-to debe usar http:// o https:// (got %q)", u.Scheme)
	}
	if u.Host == "" {
		return apierr.Usagef("--forward-to no tiene host")
	}
	if allowRemote {
		return nil
	}
	if !isLoopbackHost(u.Hostname()) {
		return apierr.Usagef("--forward-to apunta a un host no-loopback (%s); reenviaría datos reales del tenant. Usa --allow-remote-forward si es intencional", u.Hostname())
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func parseEventsFilter(csv string) map[string]bool {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	out := map[string]bool{}
	for _, part := range strings.Split(csv, ",") {
		if t := strings.TrimSpace(part); t != "" {
			out[t] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type eventsPage struct {
	Data    []json.RawMessage `json:"data"`
	HasMore bool              `json:"has_more"`
}

func fetchEventsPage(ctx context.Context, cc *cliContext, after string) (*eventsPage, error) {
	q := url.Values{}
	q.Set("limit", "100")
	if after != "" {
		q.Set("starting_after", after)
	}
	resp, err := cc.client.Do(ctx, http.MethodGet, "/v1/events?"+q.Encode(), nil, nil)
	if err != nil {
		return nil, err
	}
	var page eventsPage
	if err := json.Unmarshal(resp.Body, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func lastEventID(raw json.RawMessage) (string, error) {
	var env struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", err
	}
	return env.ID, nil
}

func latestEventID(ctx context.Context, cc *cliContext) (string, error) {
	cursor := ""
	last := ""
	for {
		page, err := fetchEventsPage(ctx, cc, cursor)
		if err != nil {
			return "", err
		}
		if len(page.Data) == 0 {
			return last, nil
		}
		id, err := lastEventID(page.Data[len(page.Data)-1])
		if err != nil {
			return "", err
		}
		if id == "" || id == cursor {
			return last, nil
		}
		last = id
		cursor = id
		if !page.HasMore {
			return last, nil
		}
	}
}

func drainNewEvents(ctx context.Context, cc *cliContext, fwd *http.Client, watermark string, filter map[string]bool, secret, forwardTo string, printJSON bool, cmd *cobra.Command) (string, error) {
	for {
		page, err := fetchEventsPage(ctx, cc, watermark)
		if err != nil {
			return watermark, err
		}
		if len(page.Data) == 0 {
			return watermark, nil
		}
		for _, raw := range page.Data {
			body, eventID, eventType, err := webhook.RemapEvent(raw)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "aviso: evento ilegible: %v\n", err)
				continue
			}
			if filter == nil || filter[eventType] {
				if err := forwardEvent(ctx, fwd, forwardTo, secret, eventID, eventType, body, printJSON, cmd); err != nil && ctx.Err() == nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "aviso: no se pudo entregar %s: %v\n", eventType, err)
				}
			}
			if eventID != "" {
				watermark = eventID
			}
		}
		if !page.HasMore {
			return watermark, nil
		}
	}
}

func forwardEvent(ctx context.Context, fwd *http.Client, forwardTo, secret, eventID, eventType string, body []byte, printJSON bool, cmd *cobra.Command) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, forwardTo, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "factuarea-cli-listen")
	req.Header.Set("Factuarea-Signature", webhook.Signature(secret, time.Now().Unix(), body))
	req.Header.Set("Factuarea-Event-Id", eventID)
	req.Header.Set("Factuarea-Event-Type", eventType)
	req.Header.Set("Factuarea-Delivery-Id", newDeliveryID())

	start := time.Now()
	resp, err := fwd.Do(req)
	latency := time.Since(start)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	fmt.Fprintf(cmd.ErrOrStderr(), "%s → %d (%dms)\n", eventType, resp.StatusCode, latency.Milliseconds())
	if printJSON {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n", body)
	}
	return nil
}

func newDeliveryID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "del_" + hex.EncodeToString(b)
}
