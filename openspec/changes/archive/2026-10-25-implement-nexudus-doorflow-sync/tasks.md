# Tasks — implement-nexudus-doorflow-sync

Groups are ordered by dependency: clients and infra first, then the desired-state engine, then the two
paths that drive it (events, reconcile), then remediation, then hardening/migration. Each task states
how to verify it.

## 1. Foundation & Configuration

- [x] 1.1 Define env/secret-driven config (NATS/JetStream URL + creds, Nexudus base URL + credential, Doorflow admin/api hosts + auth, team→group mapping source, reconcile schedule, write-feature-flag) and verify `go build ./...` passes with a config-load test
- [x] 1.2 Add a structured logging setup (level, member-id, request-id, outcome fields; status + body captured on failure) and verify a unit test asserts a failed request logs the response body
- [x] 1.3 Add a health/readiness HTTP endpoint that reports JetStream consumer connected + last-processed offset and verify it returns 200 in a local integration run and 503 when the consumer is not ready
- [x] 1.4 Load the legacy `team_group_map.csv` (nexudus_id → doorflow_id + BasicMember) into a typed mapping and verify a unit test resolves known ids (BasicMember→4482, Woodshop→4622) and that teams not in the map are excluded

## 2. Clients

- [x] 2.1 Wrap the vendored Nexudus client for the reads used by `syncOneCoworker` (get-coworker-by-id, coworker updated-since, invoices paid/unpaid-since, list-all) and verify a stubbed client test returns the expected DTO fields (`AccessPincode`, `KeyFobNumber`, `AccessCardId`, `TeamIds`, `CoworkerContractIds`, `UserId`, `Email`)
- [x] 2.2 Add a Nexudus PIN write-back: read a coworker, set `AccessPincode` on the full DTO, `PUT /api/spaces/coworkers`; and a `sendpinreminder` wrapper — verified by a stubbed test asserting the request body preserves the other coworker fields unchanged
- [x] 2.3 Build the Doorflow person client (lookup by email/`system_id`, upsert to create/update, `enabled`, `group_ids`, `system_id`) and verify a stubbed test issues POST for a new person and PUT for an existing one
- [x] 2.4 Build the Doorflow credentials client over the dedicated endpoints (list for person, create with `credential_type_id`+value incl. `*` autogen, get/put/delete by credential id) and verify stubbed tests assert create uses the discovered type id and 409 is treated as "already exists" not an error
- [x] 2.5 Add a `POST /sync` commit call in the Doorflow client and verify a stubbed test issues exactly one sync after a batch of mutations and none after a no-op
- [x] 2.6 Implement startup discovery of the account's `credential_type_id` for `pin`/`keyfob`/`card` from the `credential_types` endpoint and verify a test asserts all three resolve to non-zero ids before any credential write is enabled

## 3. Desired-state engine

- [x] 3.1 Implement `syncOneCoworker(nexudusId)` to compute the desired person state (names/email, `enabled` = has-contract ∧ no-overdue-invoice, exact-set `group_ids` incl. BasicMember, `system_id: NX<id>`) from Nexudus — never from the event payload — and verify a table-driven test covers: eligible, no-contract, overdue-invoice, added-team, and removed-team (assert removed group is targeted for deletion)
- [x] 3.2 Implement the per-credential reconcile (PIN/key-fob/card): create-if-absent-present-in-Nexudus, update-if-differs, and the removal path — verified by tests that assert create→no-op on repeat, change→PUT, and that an unchanged credential issues no write (idempotency)
- [x] 3.3 Implement the contact/eligibility gate: skip members with no Nexudus `UserId`, and apply disable-not-delete on ineligibility (retain person, `enabled: false`) — verified by tests asserting no Doorflow person is created for a contact and that a disabled member's person is retained not deleted
- [x] 3.4 Gate `syncOneCoworker` on desired-vs-current diff so a fully-converged member performs zero writes, then batch a set of members and issue exactly one `POST /sync` when ≥1 mutation occurred — verified by a test asserting no `POST /sync` on an all-no-op batch
- [x] 3.5 Serialize per-member processing (in-memory lock keyed by member id) so concurrent event/reconcile paths can't race the same member — verified by a concurrency test that redelivers the same member in parallel and asserts a single final state with no duplicate person/credential

## 4. Event ingestion

- [x] 4.1 Wire the current `cmd/main.go` NATS stub into a durable JetStream consumer over the proxy's webhook stream subject pattern (ack only after durably handled, bounded max-redeliveries + ack-wait, a dead-letter path) — verified by a local run where a consumed-and-acked event does not redeliver and an unacked one does
- [x] 4.2 Parse the JetStream message (subject + the copied `X-HTTP-Path`/`X-HTTP-Query`/body + the Nexudus signature header) to extract the affected coworker id and dispatch `syncOneCoworker`; events that map to no coworker are acked as no-ops — verified by a test with a member event (triggers sync) and a non-member event (no sync, no error)
- [x] 4.3 Make malformed/unparseable and persistently-failing events log + ack-or-dead-letter without crashing or permanently blocking the consumer — verified by a test that feeds a bad payload then a good one and asserts the consumer advances
- [x] 4.4 Prove replay across downtime: publish an event while the service is stopped, start it, and verify the event is processed once — verified by a local stop/publish/start run (within the stream's retention)
- [x] 4.5 Enforce event authentication (verify the Nexudus signature header; reject or log-and-skip unsigned/forged events per the trust model) — verified by a test asserting a tampered/unsigned event produces no door write

## 5. Reconciliation

- [x] 5.1 Implement the incremental sweep: identify coworkers updated-since a reference time and coworker-invoices paid-since, then drive each through `syncOneCoworker` — verified by a test asserting a member is (re)enabled when their overdue invoice clears
- [x] 5.2 Implement a start-up reconciliation that converges members touched since last run (independent of durable-log replay) — verified by a run that misses a change while down and corrects it on start
- [x] 5.3 Implement a scheduled full-membership sweep that pages all cows (no legacy `size=50` cap), converging every member, batched + rate-limited within Nexudus and Doorflow API limits, issued on a configurable low-hours schedule — verified by a test asserting all members are covered and API calls stay within a set concurrent-limit
- [x] 5.4 Confirm reconcile and the event path share the single `syncOneCoworker` (no second Nexudus→Doorflow translation) and that drift is corrected not just logged — verified by a code-review check plus a test showing a member whose Doorflow state was altered is restored on the next sweep
- [x] 5.5 Verify the reader commit is issued after a reconcile batch that made changes (and not after an all-no-op batch) — verified by a test asserting exactly one `POST /sync` on a mutating batch

## 6. PIN remediation

- [x] 6.1 Detect an in-use PIN as a distinct, actionable outcome (separate from generic 4xx) when a member's Nexudus PIN is rejected — verified by a test asserting the conflict is classified and logged distinctly
- [x] 6.2 On conflict, generate a unique PIN via the credential API (`value: *`) and capture the generated value from Doorflow's response, then confirm the member ends with that working PIN only in Doorflow — verified by a test asserting the response's value is used and the member's PIN is not the original
- [x] 6.3 Write the generated PIN back to Nexudus (read-modify-write `PUT /api/spaces/coworkers`) and confirm via a Nexudus read-back that the member's PIN now equals the Doorflow PIN — verified by a test using a stubbed Nexudus where the PIN field is updated and other fields are unchanged
- [x] 6.4 Add the durable pending-back-write guard (single table: `member_id`, `doorflow_pin`, `attempts`, `updated_at`): while set, a re-sync retries the back-write and does not generate a new PIN; clear it once Nexudus confirms — verified by a test that forces a failed back-write and two further re-syncs, asserting exactly one generated PIN across all three passes
- [x] 6.5 Notify the member that their PIN changed and is viewable (use `sendpinreminder` or the notifier sibling) — verified by a test asserting the notification side-effect fires on remediation and not on a no-op sync
- [x] 6.6 Scope remediation to PIN only: a PIN remediation run MUST NOT touch the member's key-fob or access-card — verified by a test asserting fob/card credentials are unchanged after a PIN remediation

## 7. Hardening & Migration

- [x] 7.1 Implement a `doctor` dry-run mode that walks the membership and prints the desired-vs-current diffs it would apply, without writing — verified by a run against a dev account that produces a diff report with zero Doorflow/Nexudus writes
- [x] 7.2 Put all writes behind a feature flag with a read-only-logging mode, and confirm flag-off issues no Doorflow/Nexudus mutations — verified by a test asserting zero write calls when the flag is off
- [x] 7.3 Verify the end-to-end happy path and the in-use-PIN path against a sandboxed Doorflow account: a member added is enabled with correct groups + a working PIN; a member whose PIN is already taken ends with a regenerated PIN reflected back in Nexudus plus a notification — verified by manual/sandbox run with the log transcript confirming each request/response
- [x] 7.4 Confirm the service credential has write permission on a Coworker PIN (Nexudus) and that the Doorflow auth scheme + `credential_types` resolve cleanly (resolve Design Open Questions 1-4 against the live API) — verified by a startup probe that logs the resolved auth, type ids, and write permission rather than failing mid-sync
- [x] 7.5 Cutover: run the new service in parallel with `v3_polling`, confirm `doctor` reports zero divergence over a full-reconcile window, then decommission the legacy poller (keep `team_group_map.csv` + the recovery CLI in-tree for rollback) — verified by a sign-off that the legacy poller was stopped after a clean dry-run parity pass
