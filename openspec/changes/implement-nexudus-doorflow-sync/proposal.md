## Why

This repo is the ground-up rewrite (Generation 3) of the Nexudus→Doorflow door-access sync. The prior
system is fragile and its failure modes directly impact members: silent credential rejections left
fobs/PINs non-functional with no logged cause (§3.2, §3.7), a 3-week PIN-conflict loop (§3.3), lossy
event delivery with no reconciliation (§3.4), and a 1-hour full-sync that was too costly to run often
(§3.5). This repo currently contains only a 14-line NATS-connect stub plus a vendored Nexudus REST
client. We now have a working design (validated against the legacy `v3_polling` code and the real
Doorflow credential API) that should be implemented as a stable, recoverable, observable service.

## What Changes

- Add a **NATS/JetStream consumer** that receives Nexudus webhook events (published by the generic
  `http-nats-proxy`) at-least-once, extracts the affected coworker, and triggers a sync. JetStream
  provides the durable log that gives us replay-on-downtime; the proxy stays fully generic.
- Add a **desired-state sync engine** for a coworker (`syncOneCoworker`): fetch authoritative state
  from Nexudus (never trusted from the webhook payload), and reconcile the Doorflow **person**
  (name, email, `enabled`, exact-set groups) and each **credential** (PIN / key-fob / card → add,
  change, **remove**), then commit with Doorflow's `POST /sync`. Writes are idempotent and diff-driven
  (only-what-changed).
- **Fix a legacy correctness gap**: legacy matched groups as a *superset*, so a member removed from a
  team kept their door-group access. Sync to **exact-set** group membership so removed teams desync.
- **PIN auto-remediation (new, bidirectional)**: when a member-chosen PIN is rejected by Doorflow as
  in-use, regenerate it via the dedicated credential endpoint (`value: *`), **write the generated value
  back into Nexudus** (read-modify-write `PUT /api/spaces/coworkers`), and notify the member. Guard
  against a regeneration churn-loop with a small durable "pending back-write" ledger. This amends the
  "Nexudus is read-only ground truth" model — Nexudus becomes the exception for auto-generated PINs.
- Add a **reconciliation path**: recover from downtime / detect drift by pulling authoritative state
  from Nexudus (coworker `updated-since`, invoice `paid-since`, and an incremental/periodic sweep),
  feeding the same `syncOneCoworker`. JetStream replay is the fast recovery; reconcile is the
  correctness floor (JetStream retention is bounded).
- Default **disable — not delete** on ineligibility (no contracts / dues unpaid), retaining the
  `system_id: NX<coworker-id>` marker so records can be re-enabled. Contacts (no `UserId`) are skipped.
- Add **structured, traceable logging** across the whole event→Nexudus→Doorflow→write-back chain, and
  visible failure (no more "stopped at 4am, no logged reason").
- **Secrets via env/service secret only**; the repo is treated as publicly readable.

## Capabilities

### New Capabilities
- `doorflow-sync`: Desired-state synchronization of a coworker's Doorflow footprint — person record
  (identity, `enabled`, exact-set groups, `system_id`), per-credential reconciliation (add/change/remove),
  the `POST /sync` commit barrier, enablement policy (contracts ∧ dues-clear), and idempotency under
  at-least-once redelivery.
- `event-ingestion`: Consuming Nexudus webhook events from the durable JetStream log, extracting the
  affected coworker, and dispatching syncs; ack/redelivery semantics and observable handling.
- `reconciliation`: Recovering from downtime and detecting drift by authoritatively pulling Nexudus state
  (updated-since sweeps + scheduled/periodic full reconcile) and driving the same sync path.
- `pin-remediation`: Auto-remediating in-use PIN conflicts — regenerate the PIN in Doorflow, write the
  generated value back into Nexudus, prevent regeneration churn, and notify the member.

### Modified Capabilities
<!-- none: this is a greenfield repo with no prior specs -->

## Impact

- **Code**: greenfield — `cmd/main.go` (entrypoint), new `internal/` packages for the NATS/JetStream
  consumer, the sync engine (Nexudus→Doorflow translation), the Doorflow client, the reconcile loop, the
  PIN remediation + write-back, and the small durable "pending write-back" store.
- **APIs/dependencies**:
  - **Doorflow**: a new generated/hand-wired REST client. Person endpoints (`/api/{2,3}/people`,
    `/api/{2,3}/person/{id}`, `POST /sync`) plus the **dedicated credential endpoints**
    (`GET|POST /api/3/people/{id}/credentials`, `PUT|DELETE /api/3/people/{id}/credentials/{id}`).
    Requires startup discovery of `credential_type_id`s for `pin` / `keyfob` / `card` and resolution of
    the auth scheme (legacy HTTP-basic vs the Bearer/scoped `account.person` seen in v3 examples).
  - **Nexudus**: reuse the vendored REST client for reads (`GET /api/spaces/coworkers[...]`, invoices);
    **newly write** a coworker's `AccessPincode` via `PUT /api/spaces/coworkers` (read-modify-write), and
    optionally `POST /api/spaces/coworkers/{id}/sendpinreminder`. Requires **write permission** on
    coworker PIN in the service credential (legacy was read-only) — to be verified.
  - **NATS/JetStream**: the consumer depends on a JetStream stream bound to the proxy's webhook subject
    pattern; durable consumer + retention/ack configuration.
- **Other systems**: `http-nats-proxy` (unchanged, stays generic); `nexudus-doorflow-doctor` and
  `nexudus-notifier` siblings are natural homes for the diff-reporting and member-notification concerns
  but are out of scope for this change (notification is a *requirement* of `pin-remediation`, its
  implementation may live in the notifier).
- **Risk**: back-to-Nexudus writes are a **new, member-visible mutation** and the highest-risk behavior;
  the durable churn-guard and default disable-not-delete are the mitigations.
