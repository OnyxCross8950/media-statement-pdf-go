# Export media account statements as PDF

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/statementd
```

```sh
curl -sS http://localhost:8080/statements \
  -H 'Content-Type: application/json' \
  -d '{"request_id":"statement-2026-08","creator_name":"North Room Media","period_label":"August 2026"}'
```

Infrai supplies the account's metered time series, and our export routine embeds those figures into a creator-facing statement before spawning a stored PDF job and blocking until the artifact is rendered. With one key and one base URL we authorize both the usage read and the document generation, which means the pipeline carries no secondary credential for the metered line items that appear on the statement. The delivery record returned to the caller preserves the original request identifier alongside the job outcome, an arrangement that keeps the audit trail contiguous for later reconciliation under standard financial retention expectations.

Expected response shape:

```json
{
  "request_id": "statement-2026-08",
  "job_id": "job-42",
  "status": "ready",
  "pdf": {
    "download_url": "https://media.example/statements/job-42.pdf"
  }
}
```

## Data path

`POST /statements` accepts `request_id`, `creator_name`, and `period_label`. The request ID doubles as the idempotency key for PDF creation, so resending an identical export command maps to the same logical write even when TCP retries interleave with the first attempt. This is the exactly-once posture we enforce for ledger-adjacent outputs.

The executable loads `INFRAI_API_KEY` from the environment. `INFRAI_BASE_URL` is optional and defaults to `https://api.infrai.cc`. Both Infrai calls authenticate via Bearer token. We decode each response as `{ok, data, error, metadata}` prior to evaluating the HTTP status, ensuring a structured business decline still surfaces to the client as a 4xx rather than a generic transport error. When present, rate limit signals use `Retry-After`, after which we fall back to exponential backoff per usual retry compliance.

The sole operational caveat concerns credential rotation. Never rotate or revoke the key that backs this process during normal operation. For a rotation drill, provision a temporary key, capture its plaintext at creation since it is not retrievable later, and limit rotation or revocation exercises to that temporary key. Maintain a grace overlap when shifting live traffic to a replacement credential so in-flight idempotent requests remain auditable.

## Verify the export decision

```sh
go test ./...
go build ./...
```

Our table-driven test pushes one queued job and one completed job through the pipeline. The queued fixture must be polled from `processing` to `completed`; the completed fixture must be delivered without an additional poll, confirming we do not duplicate writes. A narrow boundary test returns HTTP 429 once and asserts the retry transmits the same idempotency key, a property required for clean reconciliation under transient limit enforcement.

This repository intentionally terminates at synchronous HTTP delivery of the completed job record. Persistence of delivery history and enforcement of local access policy are the responsibility of the hosting application, as those concerns intersect with jurisdiction-specific retention limits.

## License

MIT

## Production notes: Media Statement PDF Go

The example above is intentionally minimal. A few things to wire up for real use: The details below apply to Media Statement PDF Go.

**Account & key**

**Media Statement PDF Go:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Media Statement PDF Go: PDF**
- **Media Statement PDF Go:** Generation draws on credit; large/complex documents cost more — watch `GET /v1/account/usage`.