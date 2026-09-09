package publisher

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Germatic/dinapay-connector-binancepay/internal/adapters/postgres"
)

type Publisher struct {
	store           *postgres.Store
	endpoint, token string
	client          *http.Client
}

func New(store *postgres.Store, baseURL, token string) *Publisher {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 32
	transport.MaxIdleConnsPerHost = 16
	transport.MaxConnsPerHost = 32
	transport.IdleConnTimeout = 90 * time.Second
	transport.ResponseHeaderTimeout = 8 * time.Second
	return &Publisher{store: store, endpoint: strings.TrimRight(baseURL, "/") + "/internal/v1/provider-events", token: token, client: &http.Client{Timeout: 10 * time.Second, Transport: transport}}
}
func (p *Publisher) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.flush(ctx)
		}
	}
}
func (p *Publisher) flush(ctx context.Context) {
	items, err := p.store.PendingEvents(ctx, 50)
	if err != nil {
		return
	}
	for _, item := range items {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(item.Payload))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+p.token)
			var response *http.Response
			response, err = p.client.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, response.Body)
				response.Body.Close()
				if response.StatusCode < 200 || response.StatusCode >= 300 {
					err = fmt.Errorf("orchestrator http %d", response.StatusCode)
				}
			}
		}
		if err != nil {
			_ = p.store.MarkFailed(ctx, item.EventID, err.Error())
			continue
		}
		_ = p.store.MarkPublished(ctx, item.EventID)
	}
}
