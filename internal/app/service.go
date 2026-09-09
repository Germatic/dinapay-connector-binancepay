package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Germatic/dinapay-connector-binancepay/internal/binancepay"
	"github.com/Germatic/dinapay-connector-binancepay/internal/core"
)

type Service struct {
	store      core.Store
	clients    map[string]*binancepay.Client
	webhookURL string
	now        func() time.Time
}

func New(store core.Store, clients map[string]*binancepay.Client, webhookURL string) *Service {
	return &Service{store: store, clients: clients, webhookURL: strings.TrimRight(webhookURL, "/"), now: time.Now}
}

func (s *Service) Create(ctx context.Context, cmd core.CreatePaymentCommand, idempotencyKey string) (core.ProviderPayment, error) {
	client, err := s.validate(cmd, idempotencyKey)
	if err != nil {
		return core.ProviderPayment{}, err
	}
	body, _ := json.Marshal(cmd)
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	previous, err := s.store.ReserveCreate(ctx, idempotencyKey, hash, body)
	if err != nil {
		return core.ProviderPayment{}, err
	}
	if previous != nil {
		return *previous, nil
	}

	description := strings.TrimSpace(cmd.Description)
	if description == "" {
		description = "Dinaria Payment"
	}
	tradeNo := strings.ReplaceAll(cmd.TransactionID, "-", "")
	request := binancepay.CreateOrderRequest{
		Env: binancepay.Env{TerminalType: "WEB"}, MerchantTradeNo: tradeNo,
		OrderAmount: cmd.Amount, Currency: strings.ToUpper(cmd.Currency), Description: description,
		GoodsDetails: []binancepay.GoodsDetail{{GoodsType: "02", GoodsCategory: "Z000", ReferenceGoodsID: cmd.TransactionID, GoodsName: description}},
		ReturnURL:    cmd.ReturnURLs["success"], CancelURL: cmd.ReturnURLs["cancel"],
		WebhookURL:      s.webhookURL + "/webhooks/binancepay/" + cmd.ProviderConnectionID,
		PassThroughInfo: cmd.TransactionID,
	}
	if cmd.Binding != nil {
		request.Merchant = &binancepay.Merchant{SubMerchantID: cmd.Binding.ExternalEntityID}
	}
	if !cmd.ExpiresAt.IsZero() {
		request.OrderExpireTime = cmd.ExpiresAt.UnixMilli()
	}
	response, err := client.CreateOrder(ctx, request)
	if err != nil {
		_ = s.store.FailCreate(ctx, idempotencyKey, err.Error())
		return core.ProviderPayment{}, err
	}
	expiresAt := cmd.ExpiresAt
	if response.Data.ExpireTime > 0 {
		expiresAt = time.UnixMilli(response.Data.ExpireTime).UTC()
	}
	payment := core.ProviderPayment{
		TransactionID: cmd.TransactionID, Provider: "binancepay", ProviderConnectionID: cmd.ProviderConnectionID,
		ProviderPaymentID: response.Data.PrepayID, ProviderReference: tradeNo, Status: "started", RawStatus: "INITIAL",
		ObservedAt: s.now().UTC(), ExpiresAt: expiresAt, Amount: cmd.Amount, Currency: strings.ToUpper(cmd.Currency),
		Completion:   map[string]any{"type": "redirect", "actionUrl": first(response.Data.UniversalURL, response.Data.CheckoutURL), "links": map[string]string{"universal": response.Data.UniversalURL, "app": response.Data.Deeplink, "web": response.Data.CheckoutURL}, "qrContent": response.Data.QRContent, "qrImageUrl": response.Data.QRCodeLink},
		ProviderData: map[string]any{"merchantTradeNo": tradeNo},
	}
	if err := s.store.CompleteCreate(ctx, idempotencyKey, payment, response.Raw); err != nil {
		return core.ProviderPayment{}, err
	}
	return payment, nil
}

func (s *Service) Get(ctx context.Context, connectionID, providerPaymentID string) (core.ProviderPayment, error) {
	client, ok := s.clients[connectionID]
	if !ok {
		return core.ProviderPayment{}, fmt.Errorf("unknown provider connection")
	}
	payment, err := s.store.FindPayment(ctx, connectionID, providerPaymentID)
	if err != nil {
		return payment, err
	}
	response, err := client.QueryOrder(ctx, payment.ProviderReference, providerPaymentID)
	if err != nil {
		return payment, err
	}
	payment.RawStatus = response.Data.Status
	payment.Status = NormalizeStatus(response.Data.Status)
	payment.ObservedAt = s.now().UTC()
	_ = s.store.UpdateStatus(ctx, connectionID, providerPaymentID, payment.Status, payment.RawStatus)
	return payment, nil
}

func (s *Service) Cancel(ctx context.Context, connectionID, providerPaymentID string) (core.ProviderPayment, error) {
	client, ok := s.clients[connectionID]
	if !ok {
		return core.ProviderPayment{}, fmt.Errorf("unknown provider connection")
	}
	payment, err := s.store.FindPayment(ctx, connectionID, providerPaymentID)
	if err != nil {
		return payment, err
	}
	if _, err = client.CloseOrder(ctx, payment.ProviderReference, providerPaymentID); err != nil {
		return payment, err
	}
	payment.Status, payment.RawStatus, payment.ObservedAt = "cancelled", "PAY_CLOSED", s.now().UTC()
	err = s.store.UpdateStatus(ctx, connectionID, providerPaymentID, payment.Status, payment.RawStatus)
	return payment, err
}

func (s *Service) validate(cmd core.CreatePaymentCommand, key string) (*binancepay.Client, error) {
	if key == "" || cmd.TransactionID == "" || cmd.ProviderConnectionID == "" || cmd.Amount == "" || cmd.Currency == "" {
		return nil, fmt.Errorf("missing required field")
	}
	if cmd.Provider != "" && cmd.Provider != "binancepay" {
		return nil, fmt.Errorf("unsupported provider")
	}
	if cmd.Binding == nil || cmd.Binding.ExternalEntityID == "" {
		return nil, fmt.Errorf("merchant provider binding is required")
	}
	client, ok := s.clients[cmd.ProviderConnectionID]
	if !ok {
		return nil, fmt.Errorf("unknown provider connection")
	}
	return client, nil
}

func NormalizeStatus(status string) string {
	switch strings.ToUpper(status) {
	case "PAY_SUCCESS":
		return "confirmed"
	case "PAY_CLOSED":
		return "cancelled"
	case "PAY_FAIL":
		return "failed"
	default:
		return "started"
	}
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
