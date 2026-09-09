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
