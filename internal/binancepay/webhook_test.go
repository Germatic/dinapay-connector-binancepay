package binancepay

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func TestVerifyAndParseWebhook(t *testing.T) {
	now := time.Now()
	timestamp := strconv.FormatInt(now.UnixMilli(), 10)
	nonce := "nonce"
	body := []byte(`{"bizId":123,"bizStatus":"PAY_SUCCESS","data":"{\"merchantTradeNo\":\"trade-1\",\"prepayId\":\"prepay-1\",\"totalFee\":0.250,\"currency\":\"USDT\"}"}`)
	signature := Sign(timestamp+"\n"+nonce+"\n"+string(body)+"\n", "secret")
	if err := VerifyWebhook(timestamp, nonce, signature, body, "secret", "", now); err != nil {
		t.Fatal(err)
	}
	envelope, data, err := ParseWebhook(body)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.BizID.String() != "123" || data.MerchantTradeNo != "trade-1" || data.TotalFee.String() != "0.250" {
		encoded, _ := json.Marshal(data)
		t.Fatalf("unexpected payload: %s", encoded)
	}
	if !AmountsEqual("0.25", "0.250") {
		t.Fatal("equivalent decimal amounts differ")
	}
}
