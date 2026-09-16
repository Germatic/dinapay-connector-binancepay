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

## Failure mapping

Binance Pay API response errors retain `code`, `errorMessage`, HTTP status and
operation in an internal structured error. An API request failure is not by
itself treated as a terminal payment failure: query errors can leave the
financial result unknown and must remain eligible for reconciliation.

Terminal order statuses are mapped as follows:

| Binance status | Dinaria status | Dinaria failure |
| --- | --- | --- |
| `PAID`, `PAY_SUCCESS` | `confirmed` | none |
| `ERROR`, `PAY_FAIL` | `failed` | `processing_error` |
| `CANCELED`, `CANCELLED`, `PAY_CLOSED` | `cancelled` | `payer_cancelled` |
| `EXPIRED` | `expired` | `payment_expired` |

When `PAY_CLOSED` is observed at or after the locally stored Binance expiry,
it is mapped to `expired` rather than `cancelled`. Every terminal failure also
retains the original Binance status in internal `providerFailure` evidence.

Sources:

- <https://developers.binance.com/en/docs/products/binance-pay-merchant/api-common>
- <https://developers.binance.com/en/docs/products/binance-pay-merchant/api-order-create-v3>
- <https://developers.binance.com/en/docs/products/binance-pay-merchant/api-order-query>
- <https://developers.binance.com/en/docs/products/binance-pay-merchant/api-order-refund>
- <https://developers.binance.com/en/docs/products/binance-pay-merchant/api-order-refund-query>
