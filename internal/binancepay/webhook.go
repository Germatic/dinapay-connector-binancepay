package binancepay

import (
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/Germatic/dinapay-connector-binancepay/internal/core"
)

func VerifyWebhook(timestamp, nonce, signature string, body []byte, hmacSecret, publicKeyPEM string, now time.Time) error {
	millis, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp")
	}
	if delta := now.Sub(time.UnixMilli(millis)); delta > 5*time.Minute || delta < -5*time.Minute {
		return fmt.Errorf("stale timestamp")
	}
	payload := timestamp + "\n" + nonce + "\n" + string(body) + "\n"
	if strings.TrimSpace(publicKeyPEM) != "" {
		return verifyRSA(payload, signature, publicKeyPEM)
	}
	if hmacSecret == "" {
		return fmt.Errorf("webhook verification key missing")
	}
	want := Sign(payload, hmacSecret)
	if !hmac.Equal([]byte(strings.ToUpper(signature)), []byte(want)) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}

func ParseWebhook(body []byte) (core.WebhookEnvelope, core.WebhookData, error) {
	var envelope core.WebhookEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return envelope, core.WebhookData{}, err
	}
	raw := envelope.Data
	var escaped string
	if json.Unmarshal(raw, &escaped) == nil {
		raw = []byte(escaped)
	}
	var data core.WebhookData
	if err := json.Unmarshal(raw, &data); err != nil {
		return envelope, data, err
	}
	return envelope, data, nil
}

func verifyRSA(payload, signature, keyText string) error {
	block, _ := pem.Decode([]byte(strings.ReplaceAll(keyText, `\n`, "\n")))
	if block == nil {
		return fmt.Errorf("invalid public key")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return err
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("public key is not RSA")
	}
	signature = strings.TrimSpace(signature)
	var sig []byte
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		sig, err = encoding.DecodeString(signature)
		if err == nil {
			break
		}
	}
	if err != nil {
		sig, err = hex.DecodeString(signature)
	}
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(payload))
	if err = rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return fmt.Errorf("invalid signature")
	}
	return nil
}

func AmountsEqual(left, right string) bool {
	a, ok := new(big.Rat).SetString(strings.TrimSpace(left))
	if !ok {
		return false
	}
	b, ok := new(big.Rat).SetString(strings.TrimSpace(right))
	return ok && a.Cmp(b) == 0
}
