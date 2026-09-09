# DinaPay Binance Pay connector

Independent Binance Pay adapter for the DinaPay V2 connector contract. It owns
Binance credentials and protocol details; it does not update Dinacore or send
merchant webhooks. Provider notifications are persisted to an outbox and sent
as normalized events to the V2 orchestrator.

## Configuration

- `DATABASE_URL`: PostgreSQL connection string.
- `SERVICE_TOKEN`: bearer token shared by routing, orchestrator and connector.
- `WEBHOOK_BASE_URL`: public connector URL, without trailing slash.
- `DINAPAY_V2_URL`: internal V2 orchestrator URL.
- `PORT`: defaults to `8092`.
- `CONNECTIONS_JSON`: provider connections. Credential properties contain env
  variable names, never secret values.

Example:

```json
[{"id":"binancepay-sandbox","mode":"sandbox","baseUrl":"https://bpay.binanceapi.com","apiKeyEnv":"BINANCEPAY_API_KEY","secretKeyEnv":"BINANCEPAY_SECRET_KEY","webhookPublicKeyEnv":"BINANCEPAY_WEBHOOK_PUBLIC_KEY"}]
```

The service exposes `POST /v1/payments`, `GET /v1/payments/{id}`,
`POST /v1/payments/{id}/cancel`, `GET /v1/capabilities`, and
`POST /webhooks/binancepay/{connectionId}`.
