package binancepay

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL, apiKey, secretKey string
	http                       *http.Client
}

// APIError preserves Binance Pay's stable business code separately from its
// human-readable message. A failed API call is not automatically a terminal
// payment failure; callers decide whether to retry or query the order.
type APIError struct {
	Operation  string
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("binancepay %s failed: %s %s", e.Operation, e.Code, e.Message)
	}
	return fmt.Sprintf("binancepay %s http %d: %s", e.Operation, e.HTTPStatus, e.Message)
}

func businessError(operation, status, code, message string) error {
	if status == "SUCCESS" {
		return nil
	}
	return &APIError{Operation: operation, Code: code, Message: message}
}

func New(baseURL, apiKey, secretKey, proxyURL string) (*Client, error) {
	if baseURL == "" {
		baseURL = "https://bpay.binanceapi.com"
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 64
	transport.MaxIdleConnsPerHost = 16
	transport.MaxConnsPerHost = 32
	transport.IdleConnTimeout = 90 * time.Second
	transport.ResponseHeaderTimeout = 15 * time.Second
	if strings.TrimSpace(proxyURL) != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(u)
	}
	return &Client{strings.TrimRight(baseURL, "/"), apiKey, secretKey, &http.Client{Timeout: 20 * time.Second, Transport: transport}}, nil
}

type Merchant struct {
	SubMerchantID string `json:"subMerchantId,omitempty"`
}
type Env struct {
	TerminalType string `json:"terminalType"`
}
type GoodsDetail struct {
	GoodsType        string `json:"goodsType"`
	GoodsCategory    string `json:"goodsCategory"`
	ReferenceGoodsID string `json:"referenceGoodsId"`
	GoodsName        string `json:"goodsName"`
	GoodsDetail      string `json:"goodsDetail,omitempty"`
}
type CreateOrderRequest struct {
	Env             Env           `json:"env"`
	Merchant        *Merchant     `json:"merchant,omitempty"`
	MerchantTradeNo string        `json:"merchantTradeNo"`
	OrderAmount     string        `json:"orderAmount"`
	Currency        string        `json:"currency"`
	Description     string        `json:"description"`
	GoodsDetails    []GoodsDetail `json:"goodsDetails"`
	ReturnURL       string        `json:"returnUrl,omitempty"`
	CancelURL       string        `json:"cancelUrl,omitempty"`
	WebhookURL      string        `json:"webhookUrl,omitempty"`
	PassThroughInfo string        `json:"passThroughInfo,omitempty"`
	OrderExpireTime int64         `json:"orderExpireTime,omitempty"`
}
type CreateOrderData struct {
	PrepayID     string `json:"prepayId"`
	CheckoutURL  string `json:"checkoutUrl"`
	QRCodeLink   string `json:"qrcodeLink"`
	QRContent    string `json:"qrContent"`
	Deeplink     string `json:"deeplink"`
	UniversalURL string `json:"universalUrl"`
	Currency     string `json:"currency"`
	TotalFee     string `json:"totalFee"`
	ExpireTime   int64  `json:"expireTime"`
}
type CreateOrderResponse struct {
	Status       string          `json:"status"`
	Code         string          `json:"code"`
	ErrorMessage string          `json:"errorMessage"`
	Data         CreateOrderData `json:"data"`
	Raw          json.RawMessage `json:"-"`
}
type QueryOrderData struct {
	MerchantTradeNo string `json:"merchantTradeNo"`
	PrepayID        string `json:"prepayId"`
	TransactionID   string `json:"transactionId"`
	Status          string `json:"status"`
	Currency        string `json:"currency"`
	OrderAmount     string `json:"orderAmount"`
	TotalFee        string `json:"totalFee"`
}

func (d QueryOrderData) Amount() string {
	if d.OrderAmount != "" {
		return d.OrderAmount
	}
	return d.TotalFee
}

type QueryOrderResponse struct {
	Status       string          `json:"status"`
	Code         string          `json:"code"`
	ErrorMessage string          `json:"errorMessage"`
	Data         QueryOrderData  `json:"data"`
	Raw          json.RawMessage `json:"-"`
}
type CloseOrderResponse struct {
	Status       string `json:"status"`
	Code         string `json:"code"`
	Data         bool   `json:"data"`
	ErrorMessage string `json:"errorMessage"`
}
type RefundOrderRequest struct {
	RefundRequestID string `json:"refundRequestId"`
	PrepayID        string `json:"prepayId"`
	RefundAmount    string `json:"refundAmount"`
	RefundReason    string `json:"refundReason,omitempty"`
}
type RefundResult struct {
	RefundID        json.Number `json:"refundId"`
	RefundRequestID string      `json:"refundRequestId"`
	PrepayID        string      `json:"prepayId"`
	RefundAmount    string      `json:"refundAmount"`
	RefundedAmount  string      `json:"refundedAmount"`
	RefundStatus    string      `json:"refundStatus"`
}
type RefundResponse struct {
	Status       string          `json:"status"`
	Code         string          `json:"code"`
	Data         RefundResult    `json:"data"`
	ErrorMessage string          `json:"errorMessage"`
	Raw          json.RawMessage `json:"-"`
}

func (c *Client) CreateOrder(ctx context.Context, in CreateOrderRequest) (CreateOrderResponse, error) {
	var out CreateOrderResponse
	raw, err := c.call(ctx, "/binancepay/openapi/v3/order", in, &out)
	out.Raw = raw
	if err == nil {
		err = businessError("create", out.Status, out.Code, out.ErrorMessage)
	}
	return out, err
}
func (c *Client) QueryOrder(ctx context.Context, tradeNo, prepayID string) (QueryOrderResponse, error) {
	var out QueryOrderResponse
	payload := map[string]string{}
	if tradeNo != "" {
		payload["merchantTradeNo"] = tradeNo
	}
	if prepayID != "" {
		payload["prepayId"] = prepayID
	}
	raw, err := c.call(ctx, "/binancepay/openapi/v2/order/query", payload, &out)
	out.Raw = raw
	if err == nil {
		err = businessError("query", out.Status, out.Code, out.ErrorMessage)
	}
	return out, err
}
func (c *Client) CloseOrder(ctx context.Context, tradeNo, prepayID string) (CloseOrderResponse, error) {
	var out CloseOrderResponse
	payload := map[string]string{}
	if tradeNo != "" {
		payload["merchantTradeNo"] = tradeNo
	}
	if prepayID != "" {
		payload["prepayId"] = prepayID
	}
	_, err := c.call(ctx, "/binancepay/openapi/order/close", payload, &out)
	if err == nil {
		err = businessError("close", out.Status, out.Code, out.ErrorMessage)
		if err == nil && !out.Data {
			err = &APIError{Operation: "close", Code: out.Code, Message: out.ErrorMessage}
		}
	}
	return out, err
}
func (c *Client) RefundOrder(ctx context.Context, in RefundOrderRequest) (RefundResponse, error) {
	var out RefundResponse
	raw, err := c.call(ctx, "/binancepay/openapi/order/refund", in, &out)
	out.Raw = raw
	if err == nil {
		err = businessError("refund", out.Status, out.Code, out.ErrorMessage)
	}
	return out, err
}
func (c *Client) QueryRefund(ctx context.Context, requestID string) (RefundResponse, error) {
	var out RefundResponse
	raw, err := c.call(ctx, "/binancepay/openapi/order/refund/query", map[string]string{"refundRequestId": requestID}, &out)
	out.Raw = raw
	if err == nil {
		err = businessError("refund_query", out.Status, out.Code, out.ErrorMessage)
	}
	return out, err
}
func (c *Client) call(ctx context.Context, path string, payload, out any) (json.RawMessage, error) {
	if c.apiKey == "" || c.secretKey == "" {
		return nil, fmt.Errorf("binancepay credentials missing")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	nonce, err := randomNonce()
	if err != nil {
		return nil, err
	}
	signed := timestamp + "\n" + nonce + "\n" + string(body) + "\n"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("BinancePay-Timestamp", timestamp)
	req.Header.Set("BinancePay-Nonce", nonce)
	req.Header.Set("BinancePay-Certificate-SN", c.apiKey)
	req.Header.Set("BinancePay-Signature", Sign(signed, c.secretKey))
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope struct {
			Code         string `json:"code"`
			ErrorMessage string `json:"errorMessage"`
		}
		_ = json.Unmarshal(raw, &envelope)
		message := envelope.ErrorMessage
		if message == "" {
			message = strings.TrimSpace(string(raw))
		}
		return raw, &APIError{Operation: "request", Code: envelope.Code, Message: message, HTTPStatus: resp.StatusCode}
	}
	return raw, json.Unmarshal(raw, out)
}

func randomNonce() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	return hex.EncodeToString(value), nil
}
func Sign(payload, secret string) string {
	mac := hmac.New(sha512.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}
