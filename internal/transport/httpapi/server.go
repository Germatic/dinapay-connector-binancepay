package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Germatic/dinapay-connector-binancepay/internal/app"
	"github.com/Germatic/dinapay-connector-binancepay/internal/binancepay"
	"github.com/Germatic/dinapay-connector-binancepay/internal/core"
	"github.com/Germatic/dinapay-connector-binancepay/internal/observability"
)

type WebhookCredential struct{ HMACSecret, PublicKey string }
type Server struct {
	service            *app.Service
	serviceToken       string
	webhookCredentials map[string]WebhookCredential
}

func New(service *app.Service, token string, credentials map[string]WebhookCredential) http.Handler {
	s := &Server{service: service, serviceToken: token, webhookCredentials: credentials}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { write(w, 200, map[string]string{"status": "up"}) })
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) { write(w, 200, map[string]string{"status": "ready"}) })
	mux.Handle("GET /metrics", internalOnly(observability.Handler()))
	mux.HandleFunc("GET /v1/capabilities", s.auth(s.capabilities))
	mux.HandleFunc("POST /v1/payments", s.auth(s.create))
	mux.HandleFunc("GET /v1/payments/{providerPaymentId}", s.auth(s.get))
	mux.HandleFunc("POST /v1/payments/{providerPaymentId}/cancel", s.auth(s.cancel))
	mux.HandleFunc("POST /v1/payments/{providerPaymentId}/refunds", s.auth(s.refund))
	mux.HandleFunc("GET /v1/refunds/{providerRefundId}", s.auth(s.getRefund))
	mux.HandleFunc("POST /webhooks/binancepay/{connectionId}", s.webhook)
	return observe(mux)
}
func internalOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Real-IP") != "" {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := r.Header.Get("X-Request-Id")
		if !validRequestID(requestID) {
			requestID = randomHex(16)
		}
		trace := r.Header.Get("traceparent")
		if !validTraceparent(trace) {
			trace = fmt.Sprintf("00-%s-%s-01", randomHex(16), randomHex(8))
		}
		w.Header().Set("X-Request-Id", requestID)
		w.Header().Set("traceparent", trace)
		capture := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		request := r.WithContext(core.WithObservability(r.Context(), requestID, trace))
		next.ServeHTTP(capture, request)
		elapsed := time.Since(started)
		observability.ObserveHTTP(r.Method, request.Pattern, capture.status, elapsed)
		if r.URL.Path != "/health" && r.URL.Path != "/ready" && r.URL.Path != "/metrics" {
			slog.Info("http request", "method", r.Method, "path", r.URL.Path, "status", capture.status, "duration_ms", elapsed.Milliseconds(), "request_id", requestID, "traceparent", trace)
		}
	})
}
func randomHex(size int) string {
	raw := make([]byte, size)
	_, _ = rand.Read(raw)
	return fmt.Sprintf("%x", raw)
}
func validRequestID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	return true
}
func validTraceparent(value string) bool {
	if len(value) != 55 || value[2] != '-' || value[35] != '-' || value[52] != '-' {
		return false
	}
	for i, c := range value {
		if i == 2 || i == 35 || i == 52 {
			continue
		}
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return value[3:35] != strings.Repeat("0", 32) && value[36:52] != strings.Repeat("0", 16)
}
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.serviceToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.serviceToken)) != 1 {
			problem(w, 401, "unauthorized", "invalid service credentials")
			return
		}
		next(w, r)
	}
}
func (s *Server) capabilities(w http.ResponseWriter, _ *http.Request) {
	binding := core.BindingRequirement{Code: "merchant_sub_merchant", EntityType: "merchant", ExternalEntityType: "sub_merchant", Required: true, Cardinality: "one", ProvisioningMode: "external_registration"}
	write(w, 200, core.Capabilities{Provider: "binancepay", ContractVersion: "1", Capabilities: []core.Capability{
		{Operation: "payment", Countries: []string{"AR", "BR", "UY", "VE"}, Currencies: []string{"USDT"}, PaymentMethods: []string{"crypto_payment"}, Rails: []string{"binance_pay"}, Features: []string{"refund", "partial_refund", "cancel", "reconciliation"}, BindingRequirement: &core.BindingRequirement{EntityType: binding.EntityType, ExternalEntityType: binding.ExternalEntityType}, BindingRequirements: []core.BindingRequirement{binding}},
		{Operation: "refund", Countries: []string{"AR", "BR", "UY", "VE"}, Currencies: []string{"USDT"}, PaymentMethods: []string{"crypto_payment"}, Rails: []string{"binance_pay"}, Features: []string{"partial_refund"}},
	}})
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var cmd core.CreatePaymentCommand
	if err := decode(w, r, &cmd); err != nil {
		problem(w, 400, "invalid_request", err.Error())
		return
	}
	p, err := s.service.Create(r.Context(), cmd, r.Header.Get("Idempotency-Key"))
	if err != nil {
		mapError(w, err)
		return
	}
	write(w, 201, p)
}
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	connection := r.Header.Get("Provider-Connection-Id")
	p, err := s.service.Get(r.Context(), connection, r.PathValue("providerPaymentId"))
	if err != nil {
		mapError(w, err)
		return
	}
	write(w, 200, p)
}
func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	var command core.CancelPaymentCommand
	if err := decode(w, r, &command); err != nil {
		problem(w, 400, "invalid_request", err.Error())
		return
	}
	p, err := s.service.Cancel(r.Context(), command.ProviderConnectionID, r.PathValue("providerPaymentId"))
	if err != nil {
		mapError(w, err)
		return
	}
	write(w, 200, p)
}
func (s *Server) refund(w http.ResponseWriter, r *http.Request) {
	var command core.CreateRefundCommand
	if err := decode(w, r, &command); err != nil {
		problem(w, 400, "invalid_request", err.Error())
		return
	}
	result, err := s.service.CreateRefund(r.Context(), r.PathValue("providerPaymentId"), r.Header.Get("Idempotency-Key"), command)
	if err != nil {
		mapError(w, err)
		return
	}
	write(w, 201, result)
}
func (s *Server) getRefund(w http.ResponseWriter, r *http.Request) {
	connection := r.Header.Get("Provider-Connection-Id")
	result, err := s.service.GetRefund(r.Context(), connection, r.PathValue("providerRefundId"))
	if err != nil {
		mapError(w, err)
		return
	}
	write(w, 200, result)
}
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		problem(w, 400, "invalid_request", err.Error())
		return
	}
	connection := r.PathValue("connectionId")
	credential, ok := s.webhookCredentials[connection]
	if !ok {
		problem(w, 404, "not_found", "unknown provider connection")
		return
	}
	if err = binancepay.VerifyWebhook(r.Header.Get("BinancePay-Timestamp"), r.Header.Get("BinancePay-Nonce"), r.Header.Get("BinancePay-Signature"), body, credential.HMACSecret, credential.PublicKey, time.Now()); err != nil {
		observability.Webhook("rejected")
		problem(w, 401, "invalid_signature", err.Error())
		return
	}
	_, err = s.service.HandleWebhook(r.Context(), connection, body)
	if err != nil && !errors.Is(err, core.ErrNotFound) {
		observability.Webhook("rejected")
		problem(w, 422, "webhook_rejected", err.Error())
		return
	}
	observability.Webhook("accepted")
	write(w, 200, map[string]string{"returnCode": "SUCCESS", "returnMessage": "OK"})
}
func decode(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, code, message string) {
	write(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func mapError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrNotFound):
		problem(w, 404, "not_found", err.Error())
	case errors.Is(err, core.ErrConflict):
		problem(w, 409, "idempotency_conflict", err.Error())
	default:
		problem(w, 422, "provider_error", err.Error())
	}
}
