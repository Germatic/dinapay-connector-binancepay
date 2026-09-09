package binancepay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestCreateOrderUsesBinanceSignatureAndContract(t *testing.T) {
	const secret = "test-secret"
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/binancepay/openapi/v3/order" {
			t.Errorf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		signed := r.Header.Get("BinancePay-Timestamp") + "\n" + r.Header.Get("BinancePay-Nonce") + "\n" + string(body) + "\n"
		if got, want := r.Header.Get("BinancePay-Signature"), Sign(signed, secret); got != want {
			t.Errorf("signature = %s, want %s", got, want)
		}
		if got := r.Header.Get("BinancePay-Certificate-SN"); got != "api-key" {
			t.Errorf("certificate = %s", got)
		}
		var request CreateOrderRequest
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		if request.Merchant == nil || request.Merchant.SubMerchantID != "sub-merchant" {
			t.Errorf("merchant = %#v", request.Merchant)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"SUCCESS","code":"000000","data":{"prepayId":"123","universalUrl":"https://pay.example/123"}}`))}, nil
	})
	client := &Client{baseURL: "https://binance.test", apiKey: "api-key", secretKey: secret, http: &http.Client{Transport: transport, Timeout: time.Second}}
	response, err := client.CreateOrder(context.Background(), CreateOrderRequest{Env: Env{TerminalType: "WEB"}, Merchant: &Merchant{SubMerchantID: "sub-merchant"}, MerchantTradeNo: "tx1", OrderAmount: "0.25", Currency: "USDT", Description: "Test", GoodsDetails: []GoodsDetail{{GoodsType: "02", GoodsCategory: "Z000", ReferenceGoodsID: "tx1", GoodsName: "Test"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Data.PrepayID != "123" || len(response.Raw) == 0 {
		t.Fatalf("response = %#v", response)
	}
}

func TestRefundOrderUsesExpectedEndpointAndPayload(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/binancepay/openapi/order/refund" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var request RefundOrderRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.RefundRequestID != "refund1" || request.PrepayID != "prepay1" || request.RefundAmount != "0.10" {
			t.Fatalf("request = %#v", request)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"SUCCESS","code":"000000","data":{"refundId":419936748741509122,"refundRequestId":"refund1","prepayId":"prepay1","refundAmount":"0.10","refundStatus":"REFUNDED"}}`))}, nil
	})
	client := &Client{baseURL: "https://binance.test", apiKey: "api-key", secretKey: "secret", http: &http.Client{Transport: transport, Timeout: time.Second}}
	response, err := client.RefundOrder(context.Background(), RefundOrderRequest{RefundRequestID: "refund1", PrepayID: "prepay1", RefundAmount: "0.10"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Data.RefundID.String() != "419936748741509122" || response.Data.RefundStatus != "REFUNDED" {
		t.Fatalf("response = %#v", response)
	}
}
