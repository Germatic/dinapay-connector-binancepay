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
	"github.com/Germatic/dinapay-connector-binancepay/internal/observability"
)

type Service struct {
	store      core.Store
	clients    map[string]*binancepay.Client
	webhookURL string
	now        func() time.Time
	queryOrder func(context.Context, string, string, string) (binancepay.QueryOrderResponse, error)
}

func New(store core.Store, clients map[string]*binancepay.Client, webhookURL string) *Service {
	service := &Service{store: store, clients: clients, webhookURL: strings.TrimRight(webhookURL, "/"), now: time.Now}
	service.queryOrder = func(ctx context.Context, connectionID, tradeNo, prepayID string) (binancepay.QueryOrderResponse, error) {
		client, ok := clients[connectionID]
		if !ok {
			return binancepay.QueryOrderResponse{}, fmt.Errorf("unknown provider connection")
		}
		return client.QueryOrder(ctx, tradeNo, prepayID)
	}
	return service
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
	started := time.Now()
	response, err := client.CreateOrder(ctx, request)
	observability.ObserveProvider("create_payment", err, time.Since(started))
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
		ProviderPaymentID: response.Data.PrepayID, ProviderReference: tradeNo, Status: "created", RawStatus: "INITIAL",
		ObservedAt: s.now().UTC(), ExpiresAt: expiresAt, Amount: cmd.Amount, Currency: strings.ToUpper(cmd.Currency),
		Completion:   redirectCompletion(response.Data),
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
	started := time.Now()
	response, err := client.QueryOrder(ctx, payment.ProviderReference, providerPaymentID)
	observability.ObserveProvider("get_payment", err, time.Since(started))
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
	started := time.Now()
	_, err = client.CloseOrder(ctx, payment.ProviderReference, providerPaymentID)
	observability.ObserveProvider("cancel_payment", err, time.Since(started))
	if err != nil {
		return payment, err
	}
	payment.Status, payment.RawStatus, payment.ObservedAt = "cancelled", "PAY_CLOSED", s.now().UTC()
	err = s.store.UpdateStatus(ctx, connectionID, providerPaymentID, payment.Status, payment.RawStatus)
	return payment, err
}

func (s *Service) CreateRefund(ctx context.Context, providerPaymentID, key string, cmd core.CreateRefundCommand) (core.ProviderRefund, error) {
	if key == "" || cmd.RefundID == "" || cmd.TransactionID == "" || cmd.ProviderConnectionID == "" || cmd.Amount == "" || cmd.Currency == "" {
		return core.ProviderRefund{}, fmt.Errorf("missing required field")
	}
	client, ok := s.clients[cmd.ProviderConnectionID]
	if !ok {
		return core.ProviderRefund{}, fmt.Errorf("unknown provider connection")
	}
	payment, err := s.store.FindPayment(ctx, cmd.ProviderConnectionID, providerPaymentID)
	if err != nil {
		return core.ProviderRefund{}, err
	}
	if payment.TransactionID != cmd.TransactionID || !strings.EqualFold(payment.Currency, cmd.Currency) {
		return core.ProviderRefund{}, core.ErrConflict
	}
	body, _ := json.Marshal(cmd)
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	previous, err := s.store.ReserveRefund(ctx, key, hash, body, cmd)
	if err != nil {
		return core.ProviderRefund{}, err
	}
	if previous != nil {
		return *previous, nil
	}
	requestID := strings.ReplaceAll(cmd.RefundID, "-", "")
	started := time.Now()
	response, err := client.RefundOrder(ctx, binancepay.RefundOrderRequest{RefundRequestID: requestID, PrepayID: providerPaymentID, RefundAmount: cmd.Amount, RefundReason: cmd.Reason})
	observability.ObserveProvider("create_refund", err, time.Since(started))
	if err != nil {
		_ = s.store.FailRefund(ctx, key, err.Error())
		return core.ProviderRefund{}, err
	}
	refund := core.ProviderRefund{RefundID: cmd.RefundID, TransactionID: cmd.TransactionID, Provider: "binancepay", ProviderConnectionID: cmd.ProviderConnectionID, ProviderRefundID: response.Data.RefundID.String(), Status: normalizeRefundStatus(response.Data.RefundStatus), RawStatus: response.Data.RefundStatus, Amount: cmd.Amount, Currency: strings.ToUpper(cmd.Currency), ObservedAt: s.now().UTC(), ProviderData: map[string]any{"refundRequestId": requestID}}
	if err = s.store.CompleteRefund(ctx, key, refund, response.Raw); err != nil {
		return core.ProviderRefund{}, err
	}
	return refund, nil
}
func (s *Service) GetRefund(ctx context.Context, connectionID, refundID string) (core.ProviderRefund, error) {
	client, ok := s.clients[connectionID]
	if !ok {
		return core.ProviderRefund{}, fmt.Errorf("unknown provider connection")
	}
	refund, err := s.store.FindRefund(ctx, connectionID, refundID)
	if err != nil {
		return refund, err
	}
	requestID, _ := refund.ProviderData["refundRequestId"].(string)
	started := time.Now()
	response, err := client.QueryRefund(ctx, requestID)
	observability.ObserveProvider("get_refund", err, time.Since(started))
	if err != nil {
		return refund, err
	}
	refund.Status, refund.RawStatus, refund.ObservedAt = normalizeRefundStatus(response.Data.RefundStatus), response.Data.RefundStatus, s.now().UTC()
	if id := response.Data.RefundID.String(); id != "" {
		refund.ProviderRefundID = id
	}
	return refund, nil
}
func normalizeRefundStatus(v string) string {
	switch strings.ToUpper(v) {
	case "REFUNDED", "REFUND_SUCCESS":
		return "confirmed"
	case "CANCELLED", "REFUND_FAIL", "FAILED":
		return "failed"
	default:
		return "pending"
	}
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
	case "INITIAL":
		return "created"
	case "PENDING", "PAID":
		return "pending"
	case "PAY_SUCCESS":
		return "confirmed"
	case "PAY_CLOSED":
		return "cancelled"
	case "PAY_FAIL":
		return "failed"
	default:
		return "pending"
	}
}

func nonEmptyLinks(universal, app, web string) map[string]string {
	links := map[string]string{}
	if universal != "" {
		links["universal"] = universal
	}
	if app != "" {
		links["app"] = app
	}
	if web != "" {
		links["web"] = web
	}
	return links
}

func redirectCompletion(data binancepay.CreateOrderData) map[string]any {
	completion := map[string]any{"type": "redirect", "links": nonEmptyLinks(data.UniversalURL, data.Deeplink, data.CheckoutURL)}
	if data.QRContent != "" {
		completion["qrContent"] = data.QRContent
	}
	if data.QRCodeLink != "" {
		completion["qrImageUrl"] = data.QRCodeLink
	}
	return completion
}
