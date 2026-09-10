package app

import (
	"context"
	"testing"
	"time"

	"github.com/Germatic/dinapay-connector-binancepay/internal/binancepay"
	"github.com/Germatic/dinapay-connector-binancepay/internal/core"
)

type reconcileStore struct {
	observation *core.ProviderPayment
	event       *core.ProviderEvent
	retry       string
}

func (*reconcileStore) ReserveCreate(context.Context, string, string, []byte) (*core.ProviderPayment, error) {
	return nil, nil
}
func (*reconcileStore) CompleteCreate(context.Context, string, core.ProviderPayment, []byte) error {
	return nil
}
func (*reconcileStore) FailCreate(context.Context, string, string) error { return nil }
func (*reconcileStore) FindPayment(context.Context, string, string) (core.ProviderPayment, error) {
	return core.ProviderPayment{}, nil
}
func (*reconcileStore) FindPaymentByReference(context.Context, string, string) (core.ProviderPayment, error) {
	return core.ProviderPayment{}, nil
}
func (*reconcileStore) UpdateStatus(context.Context, string, string, string, string) error {
	return nil
}
func (*reconcileStore) ClaimPaymentsForReconciliation(context.Context, int) ([]core.ProviderPayment, error) {
	return nil, nil
}
func (s *reconcileStore) UpdatePaymentObservation(_ context.Context, payment core.ProviderPayment) error {
	s.observation = &payment
	return nil
}
func (s *reconcileStore) RetryPaymentReconciliation(_ context.Context, _, _ string, message string) error {
	s.retry = message
	return nil
}
func (s *reconcileStore) RecordProviderEvent(_ context.Context, event core.ProviderEvent, _ []byte) (bool, error) {
	s.event = &event
	return true, nil
}
func (*reconcileStore) ReserveRefund(context.Context, string, string, []byte, core.CreateRefundCommand) (*core.ProviderRefund, error) {
	return nil, nil
}
func (*reconcileStore) CompleteRefund(context.Context, string, core.ProviderRefund, []byte) error {
	return nil
}
func (*reconcileStore) FailRefund(context.Context, string, string) error { return nil }
func (*reconcileStore) FindRefund(context.Context, string, string) (core.ProviderRefund, error) {
	return core.ProviderRefund{}, nil
}

func TestReconcilePayment(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		amount     string
		wantEvent  string
		wantUpdate string
		wantRetry  string
	}{
		{name: "confirmed emits durable event", status: "PAY_SUCCESS", amount: "0.25", wantEvent: "confirmed"},
		{name: "pending is rescheduled", status: "PENDING", amount: "0.25000000", wantUpdate: "pending"},
		{name: "amount mismatch is retried", status: "PAY_SUCCESS", amount: "0.50", wantRetry: "amount mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &reconcileStore{}
			service := New(store, nil, "https://example.test")
			service.queryOrder = func(context.Context, string, string, string) (binancepay.QueryOrderResponse, error) {
				return binancepay.QueryOrderResponse{Status: "SUCCESS", Data: binancepay.QueryOrderData{MerchantTradeNo: "trade", PrepayID: "prepay", TransactionID: "binance-tx", Status: test.status, Currency: "USDT", OrderAmount: test.amount}}, nil
			}
			service.now = func() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) }
			payment := core.ProviderPayment{TransactionID: "transaction", Provider: "binancepay", ProviderConnectionID: "connection", ProviderPaymentID: "prepay", ProviderReference: "trade", Status: "created", RawStatus: "INITIAL", Amount: "0.25", Currency: "USDT"}

			service.reconcilePayment(context.Background(), payment)

			if test.wantEvent != "" && (store.event == nil || store.event.Data.Status != test.wantEvent || store.event.Source != "poll" || store.event.Data.ProviderData["binanceTransactionId"] != "binance-tx") {
				t.Fatalf("unexpected event: %#v", store.event)
			}
			if test.wantUpdate != "" && (store.observation == nil || store.observation.Status != test.wantUpdate) {
				t.Fatalf("unexpected observation: %#v", store.observation)
			}
			if store.retry != test.wantRetry {
				t.Fatalf("retry=%q want=%q", store.retry, test.wantRetry)
			}
		})
	}
}

func TestProviderStatusEventIsDeterministic(t *testing.T) {
	payment := core.ProviderPayment{ProviderConnectionID: "connection", ProviderPaymentID: "prepay", RawStatus: "PAY_SUCCESS", Status: "confirmed", ObservedAt: time.Now()}
	first := providerStatusEvent(payment, "poll", "tx")
	second := providerStatusEvent(payment, "poll", "tx")
	if first.EventID != second.EventID {
		t.Fatalf("event ids differ: %s %s", first.EventID, second.EventID)
	}
}
