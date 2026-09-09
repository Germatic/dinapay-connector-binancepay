package postgres

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/Germatic/dinapay-connector-binancepay/internal/core"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migration.sql
var migration string

type Store struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if _, err = pool.Exec(ctx, migration); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{Pool: pool}, nil
}

func (s *Store) Close() { s.Pool.Close() }

func (s *Store) ReserveCreate(ctx context.Context, key, hash string, payload []byte) (*core.ProviderPayment, error) {
	result, err := s.Pool.Exec(ctx, `INSERT INTO binancepay_v2_operations(idempotency_key,request_hash,request_payload,status) VALUES($1,$2,$3,'processing') ON CONFLICT DO NOTHING`, key, hash, payload)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 1 {
		return nil, nil
	}
	var existingHash, status string
	var response []byte
	err = s.Pool.QueryRow(ctx, `SELECT request_hash,status,response_payload FROM binancepay_v2_operations WHERE idempotency_key=$1`, key).Scan(&existingHash, &status, &response)
	if err != nil {
		return nil, err
	}
	if existingHash != hash {
		return nil, core.ErrConflict
	}
	if status != "completed" {
		return nil, core.ErrConflict
	}
	var payment core.ProviderPayment
	if err = json.Unmarshal(response, &payment); err != nil {
		return nil, err
	}
	return &payment, nil
}

func (s *Store) CompleteCreate(ctx context.Context, key string, payment core.ProviderPayment, providerResponse []byte) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	completion, _ := json.Marshal(payment.Completion)
	data, _ := json.Marshal(payment.ProviderData)
	encoded, _ := json.Marshal(payment)
	_, err = tx.Exec(ctx, `INSERT INTO binancepay_v2_orders(provider_connection_id,provider_payment_id,provider_reference,transaction_id,amount,currency,status,raw_status,expires_at,completion,provider_data,provider_response,observed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, payment.ProviderConnectionID, payment.ProviderPaymentID, payment.ProviderReference, payment.TransactionID, payment.Amount, payment.Currency, payment.Status, payment.RawStatus, nullableTime(payment.ExpiresAt), completion, data, providerResponse, payment.ObservedAt)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE binancepay_v2_operations SET status='completed',response_payload=$2,updated_at=now() WHERE idempotency_key=$1`, key, encoded)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) FailCreate(ctx context.Context, key, message string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE binancepay_v2_operations SET status='failed',error_message=$2,updated_at=now() WHERE idempotency_key=$1`, key, message)
	return err
}

func (s *Store) FindPayment(ctx context.Context, connection, id string) (core.ProviderPayment, error) {
	return s.find(ctx, `SELECT transaction_id,provider_connection_id,provider_payment_id,provider_reference,status,raw_status,observed_at,expires_at,completion,provider_data,amount,currency FROM binancepay_v2_orders WHERE provider_connection_id=$1 AND provider_payment_id=$2`, connection, id)
}
func (s *Store) FindPaymentByReference(ctx context.Context, connection, reference string) (core.ProviderPayment, error) {
	return s.find(ctx, `SELECT transaction_id,provider_connection_id,provider_payment_id,provider_reference,status,raw_status,observed_at,expires_at,completion,provider_data,amount,currency FROM binancepay_v2_orders WHERE provider_connection_id=$1 AND provider_reference=$2`, connection, reference)
}
func (s *Store) find(ctx context.Context, query string, args ...any) (core.ProviderPayment, error) {
	var p core.ProviderPayment
	var expires *time.Time
	var completion, data []byte
	err := s.Pool.QueryRow(ctx, query, args...).Scan(&p.TransactionID, &p.ProviderConnectionID, &p.ProviderPaymentID, &p.ProviderReference, &p.Status, &p.RawStatus, &p.ObservedAt, &expires, &completion, &data, &p.Amount, &p.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, core.ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.Provider = "binancepay"
	if expires != nil {
		p.ExpiresAt = *expires
	}
	_ = json.Unmarshal(completion, &p.Completion)
	_ = json.Unmarshal(data, &p.ProviderData)
	return p, nil
}
func (s *Store) UpdateStatus(ctx context.Context, connection, id, status, raw string) error {
	result, err := s.Pool.Exec(ctx, `UPDATE binancepay_v2_orders SET status=$3,raw_status=$4,observed_at=now(),updated_at=now() WHERE provider_connection_id=$1 AND provider_payment_id=$2`, connection, id, status, raw)
	if err == nil && result.RowsAffected() == 0 {
		return core.ErrNotFound
	}
	return err
}
func (s *Store) RecordProviderEvent(ctx context.Context, event core.ProviderEvent, raw []byte) (bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `INSERT INTO binancepay_v2_inbound_events(provider_connection_id,event_id,raw_payload) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, event.ProviderConnectionID, event.EventID, raw)
	if err != nil {
		return false, err
	}
	if result.RowsAffected() == 0 {
		return false, nil
	}
	payload, _ := json.Marshal(event)
	if _, err = tx.Exec(ctx, `INSERT INTO binancepay_v2_event_outbox(event_id,payload) VALUES($1,$2)`, event.EventID, payload); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE binancepay_v2_orders SET status=$3,raw_status=$4,observed_at=$5,updated_at=now() WHERE provider_connection_id=$1 AND provider_payment_id=$2`, event.ProviderConnectionID, event.ProviderPaymentID, event.Data.Status, event.Data.RawStatus, event.ObservedAt); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

type OutboxItem struct {
	EventID string
	Payload []byte
}

func (s *Store) PendingEvents(ctx context.Context, limit int) ([]OutboxItem, error) {
	rows, err := s.Pool.Query(ctx, `SELECT event_id,payload FROM binancepay_v2_event_outbox WHERE published_at IS NULL AND next_attempt_at<=now() ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []OutboxItem
	for rows.Next() {
		var item OutboxItem
		if err = rows.Scan(&item.EventID, &item.Payload); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func (s *Store) MarkPublished(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE binancepay_v2_event_outbox SET published_at=now() WHERE event_id=$1`, id)
	return err
}
func (s *Store) MarkFailed(ctx context.Context, id, message string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE binancepay_v2_event_outbox SET attempts=attempts+1,last_error=$2,next_attempt_at=now()+least(interval '5 minutes',interval '2 seconds'*power(2,least(attempts,8))) WHERE event_id=$1`, id, message)
	return err
}

func (s *Store) ReserveRefund(ctx context.Context, key, hash string, payload []byte, cmd core.CreateRefundCommand) (*core.ProviderRefund, error) {
	result, err := s.Pool.Exec(ctx, `INSERT INTO binancepay_v2_refunds(refund_id,transaction_id,provider_connection_id,provider_refund_id,refund_request_id,idempotency_key,request_hash,request_payload,amount,currency,status,raw_status,provider_data,observed_at) VALUES($1,$2,$3,'',replace($1,'-',''),$4,$5,$6,$7,$8,'processing','',jsonb_build_object('refundRequestId',replace($1,'-','')),now()) ON CONFLICT DO NOTHING`, cmd.RefundID, cmd.TransactionID, cmd.ProviderConnectionID, key, hash, payload, cmd.Amount, cmd.Currency)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 1 {
		return nil, nil
	}
	var existingHash, status string
	var response []byte
	err = s.Pool.QueryRow(ctx, `SELECT request_hash,status,response_payload FROM binancepay_v2_refunds WHERE idempotency_key=$1`, key).Scan(&existingHash, &status, &response)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if existingHash != hash || status == "processing" || len(response) == 0 {
		return nil, core.ErrConflict
	}
	var refund core.ProviderRefund
	if err = json.Unmarshal(response, &refund); err != nil {
		return nil, err
	}
	return &refund, nil
}

func (s *Store) CompleteRefund(ctx context.Context, key string, r core.ProviderRefund, providerResponse []byte) error {
	encoded, _ := json.Marshal(r)
	data, _ := json.Marshal(r.ProviderData)
	result, err := s.Pool.Exec(ctx, `UPDATE binancepay_v2_refunds SET provider_refund_id=$2,status=$3,raw_status=$4,provider_data=$5,response_payload=$6,provider_response=$7,observed_at=$8,updated_at=now() WHERE idempotency_key=$1`, key, r.ProviderRefundID, r.Status, r.RawStatus, data, encoded, providerResponse, r.ObservedAt)
	if err == nil && result.RowsAffected() == 0 {
		return core.ErrNotFound
	}
	return err
}
func (s *Store) FailRefund(ctx context.Context, key, message string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE binancepay_v2_refunds SET status='failed',error_message=$2,updated_at=now() WHERE idempotency_key=$1`, key, message)
	return err
}
func (s *Store) FindRefund(ctx context.Context, connection, id string) (core.ProviderRefund, error) {
	var r core.ProviderRefund
	var data []byte
	err := s.Pool.QueryRow(ctx, `SELECT refund_id,transaction_id,provider_connection_id,provider_refund_id,status,raw_status,amount,currency,observed_at,provider_data FROM binancepay_v2_refunds WHERE provider_connection_id=$1 AND (provider_refund_id=$2 OR refund_id=$2)`, connection, id).Scan(&r.RefundID, &r.TransactionID, &r.ProviderConnectionID, &r.ProviderRefundID, &r.Status, &r.RawStatus, &r.Amount, &r.Currency, &r.ObservedAt, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, core.ErrNotFound
	}
	r.Provider = "binancepay"
	_ = json.Unmarshal(data, &r.ProviderData)
	return r, err
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
