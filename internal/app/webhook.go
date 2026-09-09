package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Germatic/dinapay-connector-binancepay/internal/binancepay"
	"github.com/Germatic/dinapay-connector-binancepay/internal/core"
)

func (s *Service) HandleWebhook(ctx context.Context, connectionID string, body []byte) (bool, error) {
	envelope, data, err := binancepay.ParseWebhook(body)
	if err != nil {
		return false, err
	}
	payment, err := s.store.FindPaymentByReference(ctx, connectionID, data.MerchantTradeNo)
	if err != nil {
		return false, err
	}
	if data.PrepayID != "" && data.PrepayID != payment.ProviderPaymentID {
		return false, fmt.Errorf("provider payment mismatch")
	}
	if data.Currency != "" && !strings.EqualFold(data.Currency, payment.Currency) {
		return false, fmt.Errorf("currency mismatch")
	}
	if data.TotalFee.String() != "" && !binancepay.AmountsEqual(data.TotalFee.String(), payment.Amount) {
		return false, fmt.Errorf("amount mismatch")
	}
	status := NormalizeStatus(envelope.BizStatus)
	if strings.EqualFold(envelope.BizStatus, "PAY_CLOSED") && !payment.ExpiresAt.IsZero() && !s.now().Before(payment.ExpiresAt) {
		status = "expired"
	}
	if status == "pending" && !strings.EqualFold(envelope.BizStatus, "PAID") {
		return false, nil
	}
	now := s.now().UTC()
	eventID := fmt.Sprintf("binancepay:%s:%s", payment.ProviderPaymentID, strings.ToLower(envelope.BizStatus))
	event := core.ProviderEvent{EventID: eventID, EventType: "payment.provider_" + status, EventVersion: "1", Source: "webhook", OccurredAt: now, ObservedAt: now, TransactionID: payment.TransactionID, Provider: "binancepay", ProviderConnectionID: connectionID, ProviderPaymentID: payment.ProviderPaymentID, Data: core.EventData{Status: status, RawStatus: envelope.BizStatus, Amount: payment.Amount, Currency: payment.Currency, ProviderReference: payment.ProviderReference, ProviderData: map[string]any{"binanceTransactionId": data.TransactionID, "bizId": envelope.BizID.String(), "bizIdStr": envelope.BizIDStr}}}
	raw, _ := json.Marshal(map[string]any{"envelope": json.RawMessage(body), "receivedAt": time.Now().UTC()})
	return s.store.RecordProviderEvent(ctx, event, raw)
}
