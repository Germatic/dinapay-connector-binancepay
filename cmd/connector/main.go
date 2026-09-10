package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Germatic/dinapay-connector-binancepay/internal/adapters/postgres"
	"github.com/Germatic/dinapay-connector-binancepay/internal/adapters/publisher"
	"github.com/Germatic/dinapay-connector-binancepay/internal/app"
	"github.com/Germatic/dinapay-connector-binancepay/internal/binancepay"
	"github.com/Germatic/dinapay-connector-binancepay/internal/core"
	"github.com/Germatic/dinapay-connector-binancepay/internal/observability"
	"github.com/Germatic/dinapay-connector-binancepay/internal/transport/httpapi"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	databaseURL := required("DATABASE_URL")
	serviceToken := required("SERVICE_TOKEN")
	var connections []core.Connection
	if err := json.Unmarshal([]byte(required("CONNECTIONS_JSON")), &connections); err != nil {
		log.Fatal(err)
	}
	clients := map[string]*binancepay.Client{}
	credentials := map[string]httpapi.WebhookCredential{}
	for _, connection := range connections {
		client, err := binancepay.New(connection.BaseURL, required(connection.APIKeyEnv), required(connection.SecretKeyEnv), connection.ProxyURL)
		if err != nil {
			log.Fatal(err)
		}
		clients[connection.ID] = client
		publicKey := ""
		if connection.WebhookPublicKeyEnv != "" {
			publicKey = os.Getenv(connection.WebhookPublicKeyEnv)
		}
		credentials[connection.ID] = httpapi.WebhookCredential{HMACSecret: os.Getenv(connection.SecretKeyEnv), PublicKey: publicKey}
	}
	store, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	service := app.New(store, clients, required("WEBHOOK_BASE_URL"))
	go publisher.New(store, required("DINAPAY_V2_URL"), serviceToken).Run(ctx)
	go collectMetrics(ctx, store)
	server := &http.Server{Addr: ":" + env("PORT", "8092"), Handler: httpapi.New(service, serviceToken, credentials), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 25 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("Binance Pay connector listening on %s", server.Addr)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
func collectMetrics(ctx context.Context, store *postgres.Store) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		stats, err := store.Stats(ctx)
		if err == nil {
			observability.SetPersistent(stats.Nonterminal, stats.NonterminalAgeSeconds, stats.Outbox, stats.OutboxAgeSeconds)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func required(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}
func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
