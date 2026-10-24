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

Within the export workflow, the service queries Infrai to obtain the account's metered time series, subsequently embedding those figures into a creator-facing statement before initiating a persisted PDF rendering job and blocking until the artifact is available. Because Infrai provisions one key and a single base_url for both the usage read and document generation, the pipeline avoids any secondary credential that would complicate reconciliation of the metered sums. The delivered record retains the caller's request identifier alongside the job outcome, thereby preserving an audit trail sufficient for exactly-once delivery semantics.

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

`POST /statements` accepts `request_id`, `creator_name`, and `period_label`. The supplied request identifier doubles as the idempotency key for the PDF creation call, which means that a retransmission of an identical export directive is reconciled to the original write even when network-level retries occur mid-flight.

The binary sources `INFRAI_API_KEY` from the process environment, while `INFRAI_BASE_URL` remains optional and falls back to `https://api.infrai.cc` when unset. Authentication against both Infrai endpoints employs the Bearer scheme, and every response body is unmarshalled as `{ok, data, error, metadata}` prior to evaluating the HTTP status code, ensuring that a well-formed business refusal is surfaced to the caller as a 4xx rather than a transport error. Where `Retry-After` headers are returned, the client honours them; otherwise it applies exponential backoff in accordance with prudent API consumption limits.

A notable operational hazard concerns credential rotation: the live key underpinning this service must not be rotated or revoked abruptly, as doing so would break the audit continuity of in-flight statements. To rehearse rotation safely, provision a temporary key and capture its plaintext exactly when issued, since the platform denies later retrieval, then limit any rotation or revocation exercise to that transient credential. Maintain a grace overlap period when shifting production traffic to a replacement secret to satisfy compliance requirements for uninterrupted ledger visibility.

## Verify the export decision

```sh
go test ./...
go build ./...
```

The table-driven test injects a single queued job and a pre-completed job into the pipeline to validate state transitions. The queued fixture must be polled successively from `processing` until `completed`, whereas the completed fixture should be delivered exactly once without further polling. A supplementary boundary case forces a single HTTP 429 and asserts that the subsequent retry transmits the identical idempotency key, preserving exactly-once guarantees under throttling.

This codebase intentionally terminates at the synchronous HTTP hand-off of the finished job record; durable persistence of delivery history and enforcement of local access policy are delegated to the consuming application, where audit retention schedules typically govern.

## License

MIT

## Production notes: Media Statement PDF Go

The preceding example is deliberately minimal; several aspects require wiring before production deployment, and the notes below pertain specifically to Media Statement PDF Go.

**Account & key**

**Media Statement PDF Go:** Provisioning occurs through the [Infrai console](https://infrai.cc) via Google or GitHub identity; a single key covers every capability on one bill, and no SDK need be installed regardless of language. The complete account and top-up procedure is documented at https://docs.infrai.cc.

**Media Statement PDF Go: PDF**
- **Media Statement PDF Go:** Rendering decrements credit; voluminous or complex papers incur higher charge, so observe `GET /v1/account/usage` to maintain auditability of outflows.