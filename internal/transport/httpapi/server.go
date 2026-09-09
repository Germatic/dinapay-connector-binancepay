package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Germatic/dinapay-connector-binancepay/internal/app"
	"github.com/Germatic/dinapay-connector-binancepay/internal/binancepay"
	"github.com/Germatic/dinapay-connector-binancepay/internal/core"
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
	mux.HandleFunc("GET /v1/capabilities", s.auth(s.capabilities))
	mux.HandleFunc("POST /v1/payments", s.auth(s.create))
	mux.HandleFunc("GET /v1/payments/{providerPaymentId}", s.auth(s.get))
	mux.HandleFunc("POST /v1/payments/{providerPaymentId}/cancel", s.auth(s.cancel))
	mux.HandleFunc("POST /webhooks/binancepay/{connectionId}", s.webhook)
	return mux
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
	write(w, 200, map[string]any{"provider": "binancepay", "operations": []string{"create", "get", "cancel"}, "paymentMethods": []string{"redirect"}, "currencies": []string{"USDT"}})
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
		problem(w, 401, "invalid_signature", err.Error())
		return
	}
	_, err = s.service.HandleWebhook(r.Context(), connection, body)
	if err != nil && !errors.Is(err, core.ErrNotFound) {
		problem(w, 422, "webhook_rejected", err.Error())
		return
	}
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
