package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Germatic/dinapay-connector-binancepay/internal/binancepay"
	"github.com/Germatic/dinapay-connector-binancepay/internal/core"
	"github.com/Germatic/dinapay-connector-binancepay/internal/observability"
)

func (s *Service) RunReconciler(ctx context.Context, concurrency int) {
	if concurrency < 1 {
		concurrency = 1
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		s.reconcileBatch(ctx, concurrency)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) reconcileBatch(ctx context.Context, concurrency int) {
	payments, err := s.store.ClaimPaymentsForReconciliation(ctx, 50)
	if err != nil {
		observability.Reconciliation("claim_error")
		return
	}
	parallelEach(ctx, payments, concurrency, func(payment core.ProviderPayment) {
		s.reconcilePayment(ctx, payment)
	})
}

func (s *Service) reconcilePayment(ctx context.Context, payment core.ProviderPayment) {
	started := time.Now()
	response, err := s.queryOrder(ctx, payment.ProviderConnectionID, payment.ProviderReference, payment.ProviderPaymentID)
	observability.ObserveProvider("reconcile_payment", err, time.Since(started))
	if err != nil {
		observability.Reconciliation("error")
		_ = s.store.RetryPaymentReconciliation(ctx, payment.ProviderConnectionID, payment.ProviderPaymentID, err.Error())
		return
	}
	if response.Data.PrepayID != "" && response.Data.PrepayID != payment.ProviderPaymentID {
		s.retryMismatch(ctx, payment, "provider payment mismatch")
		return
	}
	if response.Data.Currency != "" && !strings.EqualFold(response.Data.Currency, payment.Currency) {
		s.retryMismatch(ctx, payment, "currency mismatch")
		return
	}
	if amount := response.Data.Amount(); amount != "" && !binancepay.AmountsEqual(amount, payment.Amount) {
		s.retryMismatch(ctx, payment, "amount mismatch")
		return
	}
	status := NormalizeStatus(response.Data.Status)
	if strings.EqualFold(response.Data.Status, "PAY_CLOSED") && !payment.ExpiresAt.IsZero() && !s.now().Before(payment.ExpiresAt) {
		status = "expired"
	}
	payment.Status, payment.RawStatus, payment.ObservedAt = status, response.Data.Status, s.now().UTC()
	if status == "created" || status == "pending" {
		if err = s.store.UpdatePaymentObservation(ctx, payment); err != nil {
			observability.Reconciliation("store_error")
			return
		}
		observability.Reconciliation("pending")
		return
	}
	event := providerStatusEvent(payment, "poll", response.Data.TransactionID)
	raw := response.Raw
	if len(raw) == 0 {
		raw, _ = json.Marshal(response)
	}
	if _, err = s.store.RecordProviderEvent(ctx, event, raw); err != nil {
		observability.Reconciliation("store_error")
		_ = s.store.RetryPaymentReconciliation(ctx, payment.ProviderConnectionID, payment.ProviderPaymentID, err.Error())
		return
	}
	observability.Reconciliation(status)
}

func (s *Service) retryMismatch(ctx context.Context, payment core.ProviderPayment, message string) {
	observability.Reconciliation("mismatch")
	_ = s.store.RetryPaymentReconciliation(ctx, payment.ProviderConnectionID, payment.ProviderPaymentID, message)
}

func providerStatusEvent(payment core.ProviderPayment, source, transactionID string) core.ProviderEvent {
	eventID := deterministicUUID("poll:" + payment.ProviderConnectionID + ":" + payment.ProviderPaymentID + ":" + strings.ToUpper(payment.RawStatus))
	return core.ProviderEvent{EventID: eventID, EventType: "payment.provider_" + payment.Status, EventVersion: "1", Source: source, OccurredAt: payment.ObservedAt, ObservedAt: payment.ObservedAt, TransactionID: payment.TransactionID, Provider: "binancepay", ProviderConnectionID: payment.ProviderConnectionID, ProviderPaymentID: payment.ProviderPaymentID, Data: core.EventData{Status: payment.Status, RawStatus: payment.RawStatus, Amount: payment.Amount, Currency: payment.Currency, ProviderReference: payment.ProviderReference, ProviderData: map[string]any{"binanceTransactionId": transactionID}}}
}

func deterministicUUID(value string) string {
	sum := sha256.Sum256([]byte(value))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func parallelEach[T any](ctx context.Context, items []T, concurrency int, work func(T)) {
	if len(items) == 0 || ctx.Err() != nil {
		return
	}
	jobs := make(chan T)
	var workers sync.WaitGroup
	for range min(concurrency, len(items)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				work(item)
			}
		}()
	}
	for _, item := range items {
		select {
		case jobs <- item:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return
		}
	}
	close(jobs)
	workers.Wait()
}
