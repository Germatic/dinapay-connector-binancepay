package core

import (
	"encoding/json"
	"fmt"
	"time"
)

type Binding struct {
	BindingID          string `json:"bindingId"`
	EntityType         string `json:"entityType"`
	EntityID           string `json:"entityId"`
	ExternalEntityType string `json:"externalEntityType"`
	ExternalEntityID   string `json:"externalEntityId"`
}
type CreatePaymentCommand struct {
	OperationID          string            `json:"operationId"`
	TransactionID        string            `json:"transactionId"`
	Provider             string            `json:"provider"`
	ProviderConnectionID string            `json:"providerConnectionId"`
	Binding              *Binding          `json:"binding,omitempty"`
	Amount               string            `json:"amount"`
	Currency             string            `json:"currency"`
	PaymentMethod        string            `json:"paymentMethod"`
	Rail                 string            `json:"rail,omitempty"`
	Description          string            `json:"description,omitempty"`
	ExpiresAt            time.Time         `json:"expiresAt,omitempty"`
	ReturnURLs           map[string]string `json:"returnUrls,omitempty"`
	Customer             map[string]any    `json:"customer"`
	Metadata             map[string]any    `json:"metadata,omitempty"`
}
type CancelPaymentCommand struct {
	OperationID          string `json:"operationId"`
	TransactionID        string `json:"transactionId"`
	ProviderConnectionID string `json:"providerConnectionId"`
	Reason               string `json:"reason,omitempty"`
}
type CreateRefundCommand struct {
	OperationID          string         `json:"operationId"`
	RefundID             string         `json:"refundId"`
	TransactionID        string         `json:"transactionId"`
	ProviderConnectionID string         `json:"providerConnectionId"`
	Amount               string         `json:"amount"`
	Currency             string         `json:"currency"`
	Reason               string         `json:"reason,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
}
type ProviderRefund struct {
	RefundID             string         `json:"refundId"`
	TransactionID        string         `json:"transactionId"`
	Provider             string         `json:"provider"`
	ProviderConnectionID string         `json:"providerConnectionId"`
	ProviderRefundID     string         `json:"providerRefundId"`
	Status               string         `json:"status"`
	RawStatus            string         `json:"rawStatus,omitempty"`
	Amount               string         `json:"amount"`
	Currency             string         `json:"currency"`
	ObservedAt           time.Time      `json:"observedAt"`
	ProviderData         map[string]any `json:"providerData,omitempty"`
}
type ProviderPayment struct {
	TransactionID        string         `json:"transactionId"`
	Provider             string         `json:"provider"`
	ProviderConnectionID string         `json:"providerConnectionId"`
	ProviderPaymentID    string         `json:"providerPaymentId"`
	ProviderReference    string         `json:"providerReference,omitempty"`
	Status               string         `json:"status"`
	RawStatus            string         `json:"rawStatus,omitempty"`
	ObservedAt           time.Time      `json:"observedAt"`
	ExpiresAt            time.Time      `json:"expiresAt,omitempty"`
	Completion           map[string]any `json:"completion,omitempty"`
	ProviderData         map[string]any `json:"providerData,omitempty"`
	Amount               string         `json:"-"`
	Currency             string         `json:"-"`
}
type Connection struct {
	ID                  string `json:"id"`
	Mode                string `json:"mode"`
	BaseURL             string `json:"baseUrl"`
	ProxyURL            string `json:"proxyUrl,omitempty"`
	APIKeyEnv           string `json:"apiKeyEnv"`
	SecretKeyEnv        string `json:"secretKeyEnv"`
	WebhookPublicKeyEnv string `json:"webhookPublicKeyEnv,omitempty"`
}
type ProviderEvent struct {
	EventID              string    `json:"eventId"`
	EventType            string    `json:"eventType"`
	EventVersion         string    `json:"eventVersion"`
	Source               string    `json:"source"`
	OccurredAt           time.Time `json:"occurredAt"`
	ObservedAt           time.Time `json:"observedAt"`
	TransactionID        string    `json:"transactionId"`
	Provider             string    `json:"provider"`
	ProviderConnectionID string    `json:"providerConnectionId"`
	ProviderPaymentID    string    `json:"providerPaymentId"`
	Data                 EventData `json:"data"`
}
type EventData struct {
	Status            string         `json:"status"`
	RawStatus         string         `json:"rawStatus"`
	Amount            string         `json:"amount,omitempty"`
	Currency          string         `json:"currency,omitempty"`
	ProviderReference string         `json:"providerReference,omitempty"`
	ProviderData      map[string]any `json:"providerData,omitempty"`
}
type WebhookEnvelope struct {
	BizType   string          `json:"bizType"`
	BizID     flexibleString  `json:"bizId"`
	BizIDStr  string          `json:"bizIdStr"`
	BizStatus string          `json:"bizStatus"`
	Data      json.RawMessage `json:"data"`
}
type WebhookData struct {
	MerchantTradeNo string         `json:"merchantTradeNo"`
	PrepayID        string         `json:"prepayId"`
	TransactionID   string         `json:"transactionId"`
	Currency        string         `json:"currency"`
	TotalFee        flexibleString `json:"totalFee"`
}
type flexibleString string

func (s *flexibleString) UnmarshalJSON(raw []byte) error {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		*s = flexibleString(text)
		return nil
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		*s = flexibleString(number.String())
		return nil
	}
	return fmt.Errorf("invalid string value: %s", raw)
}
func (s flexibleString) String() string { return string(s) }
