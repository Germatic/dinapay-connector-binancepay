CREATE TABLE IF NOT EXISTS binancepay_v2_operations (
  idempotency_key text PRIMARY KEY,
  request_hash text NOT NULL,
  request_payload jsonb NOT NULL,
  status text NOT NULL,
  response_payload jsonb,
  error_message text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS binancepay_v2_orders (
  provider_connection_id text NOT NULL,
  provider_payment_id text NOT NULL,
  provider_reference text NOT NULL,
  transaction_id text NOT NULL UNIQUE,
  amount text NOT NULL,
  currency text NOT NULL,
  status text NOT NULL,
  raw_status text NOT NULL,
  expires_at timestamptz,
  completion jsonb,
  provider_data jsonb,
  response_payload jsonb,
  provider_response jsonb,
  observed_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (provider_connection_id, provider_payment_id),
  UNIQUE (provider_connection_id, provider_reference)
);
CREATE TABLE IF NOT EXISTS binancepay_v2_inbound_events (
  provider_connection_id text NOT NULL,
  event_id text NOT NULL,
  raw_payload jsonb NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (provider_connection_id, event_id)
);

CREATE TABLE IF NOT EXISTS binancepay_v2_event_outbox (
  event_id text PRIMARY KEY,
  payload jsonb NOT NULL,
  attempts integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  published_at timestamptz,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS binancepay_v2_refunds (
  refund_id text PRIMARY KEY,
  transaction_id text NOT NULL,
  provider_connection_id text NOT NULL,
  provider_refund_id text NOT NULL,
  refund_request_id text NOT NULL UNIQUE,
  idempotency_key text NOT NULL UNIQUE,
  request_hash text NOT NULL,
  request_payload jsonb NOT NULL,
  amount text NOT NULL,
  currency text NOT NULL,
  status text NOT NULL,
  raw_status text NOT NULL,
  provider_data jsonb,
  provider_response jsonb,
  observed_at timestamptz NOT NULL,
  error_message text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE binancepay_v2_refunds ADD COLUMN IF NOT EXISTS response_payload jsonb;
