# Design — Nexudus → Doorflow Sync

Motivation and scope live in `proposal.md`. This document covers *how* we build it.

Requirements live in the four capability specs under `specs/`; this document explains the technical
decisions behind them, not the requirements themselves.

## Context

- **Repo today:** `cmd/main.go` is a 14-line NATS-connect stub. A go-swagger-generated Nexudus REST
  client is vendored under `internal/nexudus/` (reads: `coworkers`, `teams`, invoices; and a
  read-modify-write-capable `PUT /api/spaces/coworkers`, plus `sendpinreminder`). There is **no**
  Doorflow client yet.
- **Upstream (fixed, out of scope):** `http-nats-proxy` stays deliberately *generic* — it takes an
  inbound Nexudus webhook, maps the URL to a NATS subject, copies headers + body, and issues
  `RequestMsg`. A **JetStream stream is bound to that subject pattern server-side**, so publishes are
  durably logged and the proxy's synchronous wait is satisfied by the JetStream ACK. The proxy learns
  nothing about Nexudus or webhooks; durability lives in NATS. *This is a hard constraint: we design
  the consumer, not the producer.*
- **Downstream (Doorflow):** two API surfaces exist. Person CRUD (`/api/{2,3}/people`,
  `/api/{2,3}/person/{id}`, `POST /sync`) can carry credentials *inline*, but there are also
  **dedicated credential endpoints** (`GET|POST /api/3/people/{id}/credentials`,
  `PUT|DELETE /api/3/people/{id}/credentials/{id}`) with `409`-returns-existing on duplicate. Auth
  is either HTTP-basic (legacy) or Bearer + `account.person` scope (v3 examples) — *unconfirmed*; see
  Open Questions.
- **Prior art:** the legacy `v3_polling` code works but has known gaps (superset group matching that
  never desyncs removed teams; inline credentials that swallow PIN-conflict rejections; additive-only
  credential handling; no idempotency under redelivery; `size=50` invoice cap). We inherit its domain
  logic (enablement = contracts ∧ dues-clear; groups = mapped teams ∪ BasicMember; `system_id` =
  `NX<coworker-id>`; one `POST /sync` commit) and fix its gaps.
- **Operating constraints from history:** full reconcile is expensive (~1 hr, burns quota) so it can
  rarely run; outages can be long and were undiagnosed; members pay directly when access is wrong.
  The repo is treated as publicly readable (secrets via env/secret only).

## Goals / Non-Goals

**Goals:**

- A **single desired-state unit of work** (`syncOneCoworker(nexudusId)`) shared by both the fast path
  (events) and the recovery path (reconcile), so there is exactly one Nexudus→Doorflow translation.
- **Recoverability:** at-least-once delivery, replay across downtime, and an independent reconcile leg
  that does not depend on having received events.
- **Correctness of access both added and removed** (exact-set groups; credential add/change/remove).
- **Resilient, observable failure:** no silent rejections, no undiagnosed outages; every outcome is
  logged member→Nexudus→Doorflow→write-back.
- **Safe remediation** of PIN conflicts with no churn loop and a Nexudus write-back that reflects the
  real value.

**Non-Goals:**

- Changing `http-nats-proxy` (it stays generic). We consume, not produce.
- Membership/billing *authorization* logic (what the rules *should* be) — we implement the known
  rules (contracts ∧ dues-clear), we do not redesign membership policy.
- The `doctor` (diff-reporting) and `notifier` (member comms) siblings as separate services — their
  *behavior* is required here (`reconciliation`, `pin-remediation`), their implementation homes may
  live in those repos.
- HID Mobile / PassFlow / QR credential lifecycles — only the three simple credentials (PIN, key-fob,
  card) are in scope.
- UI, dashboards, or multi-tenant support.

## Decisions

**D1 — One desired-state unit of work, fed by two paths.** The atomic operation is
`syncOneCoworker(nexudusId)`, which pulls the *current* coworker record and invoicing state from the
Nexudus REST client, computes the desired Doorflow state, diffs it against the current Doorflow state,
and applies only the delta. The event path (`event-ingestion`) and the recovery path (`reconciliation`)
both *select sets of coworker ids* and then call this single function.

- *Why:* the event payload is unreliable/unknown in shape (we confirmed we only really need the
  coworker id); pulling authoritative data makes a partial or forged event body harmless; it lets the
  reconcile leg be literally the same code path (requirement: single source of desired-state logic);
  and it collapses "add access" vs "remove access" into one diff computation.
- *Alternative — translate the event payload:* rejected. Lossy, fragile, unknown fields, and forces a
  second translation for reconcile.
- *Alternative — hold a local SQLite copy + diff (legacy Gen-1 model):* rejected. Extra durable state
  to keep correct; we already have Nexudus as the source and can just re-read it.

**D2 — Two sub-reconcilers: person, then credentials, with a single commit.** `syncOneCoworker` first
reconciles the Doorflow **person** (name/email/`enabled`/exact-set groups/`system_id`) and, once the
person id is known, reconciles each **credential type** (PIN, key-fob, card) via the dedicated
credential endpoints. Doorflow's `POST /sync` is issued **once per batch** only if at least one
mutation was applied.

- *Why the dedicated credential endpoints (not inline-on-person):* inline-on-person (the legacy v2
  path) was the root cause of both §3.2 (fob 401s that vanished) and §3.3 (PIN collision swallowed
  into a 201). The credential CRUD API gives us *per-credential observable transactions* — a PIN
  conflict is a distinct, decidable 409 rather than a masked rejection. It also lets us `DELETE`
  a credential in isolation, which inline-on-person cannot do cleanly. The `409`-returns-existing
  semantic is a *free idempotency primitive* for create, which meshes with JetStream redelivery.
- *Alternative — keep inline-on-person:* rejected because it can't express "revoke one credential
  without touching others" and buries conflict errors. We keep the inline path as a *fallback* only if
  the dedicated endpoints prove unavailable for a required operation (see Open Questions).
- *Batching:* the legacy called `POST /sync` once at the end of a sweep. We keep that, and also apply
  it per-event-batch (e.g., an inbound burst of webhooks) — one sync per batch regardless of member
  count keeps reader convergence fast without burning a sync per member.

**D3 — Exact-set group reconciliation, not superset.** The target group set is
`{ map(team) for team in coworker.TeamIds if team is in team_map } ∪ { BasicMember }`, and a member's
Doorflow groups SHALL equal that set. Groups present in Doorflow but not in the target set are
removed. This is the fix for the legacy "removed team keeps door access" gap (proposal, What Changes).

- *Why:* legacy only verified that *required* groups were present; leaving extra mapped groups in
  place is the "stale grant" class of bug in our history. Exact-set is the only interpretation under
  which "member left Woodshop" actually removes Woodshop access.
- *Alternative — additive with an explicit "revoke team membership" step:* rejected, more moving
  parts than the diff gives us for free.

**D4 — Disable-not-delete on ineligibility.** A coworker whose desired state is "disable" (no
contract / overdue invoices) is written with `enabled: false`; their Doorflow person is *retained*
with the `system_id: NX<coworker-id>` marker. Hard deletion is reserved for the recovery play that
existed in legacy (a deliberate staff action), not the normal sync path.

- *Why:* the primary failure we need to recover from is "member paid but was stuck disabled." Re
  enabling requires the Doorflow person to still exist with a stable identifier. Deleting on every
  disable makes the recover path worse, not better. Keeping the record also preserves the audit trail
  of "we manage this person."

**D5 — Idempotency + a stable identity, keyed on `NX<coworker-id>` with email for lookup.** Because the
consumer is at-least-once, every write must be safe to repeat:

- *Person:* upsert keyed by a stable identifier (`system_id: NX<coworker-id>`; located via email)
  rather than "create if not found" alone, so a redelivered event can't spawn a duplicate person.
- *Credentials:* rely on Doorflow's one-credential-per-type + `409`-returns-existing to make create
  idempotent; use `PUT`/`DELETE` on the type's credential id for change/remove.
- *Global gate:* `syncOneCoworker` computes desired vs current first and, if they match, performs **no
  write at all**. This no-op gate is the real idempotency primitive and is what makes event/reconcile
  overlap safe.

*Alternative — deduplicate events via a seen-set before processing:* rejected; it treats the symptom.
Making the *writes* idempotent is the durable fix and also covers reconcile/event collisions.

**D6 — PIN conflicts are remediated by generating in Doorflow, writing back to Nexudus, guarded by a

durable pending-back-write state.** This is the *deliberate exception* to "Nexudus is read-only ground

truth": for the value of a PIN the system generates, authority temporarily flips to Doorflow.

*Sequence (per member):* honor member intent first → attempt `PUT/POST` the member's Nexudus PIN in

Doorflow → if Doorflow rejects it as in-use, generate a unique PIN (`value: *`) via the credential API

and *read the generated value from Doorflow's response* → write that value back onto the Nexudus

coworker record via read-modify-write `PUT /api/spaces/coworkers` → notify the member → mark resolved

once a Nexudus read-back confirms the two agree.

*Churn guard (the reason we need a little durable state):* if the Nexudus write-back has not yet

succeeded, we MUST remember that a generated Doorflow PIN exists for this member. On every subsequent

pass we retry the *write-back* and we do NOT generate a new PIN again (else a persistently failing

back-write would spin forever generating fresh random PINs). Once confirmed, the guard clears. We hold

this as a single small durable table (e.g. `pending_pin_writeback(member_id, doorflow_pin, attempts,

updated_at)`) in SQLite/Postgres — the one place we break pure statelessness, and it is justified by

correctness, not convenience.

*Priority:* we never overwrite a confirmed member-chosen PIN except to resolve an actual in-use

rejection; a *newly* chosen Nexudus PIN is honored before any doorflow-side value.

**D7 — Deliver on JetStream with a durable consumer; ack only after durably handled.** We run a single

durable JetStream consumer over the webhook stream. A batch/burst of events, start-up, and the

reconcile leg each drive the *same* `syncOneCoworker`. Ack only after the coworker's sync is durably

resolved (applied, or no-op, or routed to a defined failure path) — because the proxy is satisfied by

the JetStream ACK, "ack" is our delivery guarantee, and an unacked-but-crashed event is redelivered.

*Delivery tuning:* bounded *max-redeliveries* + ack-wait + a dead-letter topic so a persistently

failing coworker is surfaced, not silently retried forever or used as a block on others. A single slow

coworker MUST NOT stall the consumer for the fleet.

*Alternative — core NATS request/reply + REST reconcile only (no JetStream):* less replay fidelity

and no durable backlog; rejected because recoverability from long outages is an explicit requirement.

*Alternative — HTTP callback into a queueing broker we own:* re-invents what JetStream already gives us

for free (durability, replay, redelivery, acks) and adds a dependency; rejected.

**D8 — Two thin, generated/hand-wired clients + a startup discovery step.**

- *Nexudus client:* reuse the vendored go-swagger client for reads. Add a small wrapper for the two

  write paths it already exposes: `PUT /api/spaces/coworkers` (read-modify-write, setting only the PIN

  field on the full DTO) and `sendpinreminder` (the natural notice carrier).

- *Doorflow client:* no spec vendored yet; build a minimal typed client covering person upsert (by

  email / `system_id`), the dedicated credential CRUD, and `POST /sync`. Prefer generating it from

  the Doorflow OpenAPI (as we did for Nexudus) to keep the API a *contract we control*; hand-wire

  only if no spec is obtainable (Open Question).

- *Startup discovery:* resolve the account's `credential_type_id`s for `pin` / `keyfob` / `card` from

  the `credential_types` endpoint once at boot and cache them — the credential CRUD API takes numeric

  type ids, not slugs, so the service cannot hardcode legacy slugs.

*Alternative — hand-rolled HTTP without a spec:* rejected for the same reason the Nexudus client was

generated — a contract we keep in the tree beats one that lives in our heads.

**D9 — Reconcile is diff-driven and batched; write only what changed, commit once.** Per member we

still make ~5–6 calls vs the legacy 2–3, so cost is the pressure point (the whole point of the §3.5

constraint). Mitigations: (1) the no-op gate (D5) skips every doorflow write for already-converged

members, so reconcile spends calls only on *actual* drift; (2) a member whose *only* change is

eligibility (dues) touches just the person `enabled` flag, not the credential round-trip; (3) a full

sweep runs on a schedule in low-hours, batched + rate-limited within Nexudus and Doorflow API limits,

while the continuous incremental leg (updated-since / invoice-paid-since) handles steady-state churn;

(4) page through invoices properly instead of the legacy `size=50` cap.

*Alternative — run the full sweep frequently:* rejected, it is the expensive path the legacy

author avoided; we only run it as the correctness floor when events/replay cannot cover a window.

## Risks / Trade-offs

- **[Writing back to Nexudus is a member-visible mutation; a clobber is a support incident.]** → We read-modify-write the full coworker DTO (change only the PIN field), honor a member-chosen PIN unless it is actually rejected, and scope the exception to PINs only (D6). We verify and gate on the service credential having *write* permission on coworker PIN, since legacy was read-only.
- **[A failing Nexudus write-back spins a regeneration loop.]** → Durable pending-back-write guard (D6): once generated, only retry the back-write; never generate again while pending. This is the one intentional piece of state.
- **[Doorflow may have moved from an HTTP-basic API key to a bearer/scoped token — possibly the root of the legacy §3.2 fob 401s.]** → Treat auth as an *unconfirmed* input (Open Question) and make auth + `credential_type_id` resolution an explicit, tested startup step (D8) rather than assuming legacy semantics still work.
- **[Two Doorflow hosts (admin for reads, api for writes) and v2/v3 version skew.]** → Encode both endpoints and standardize on one major version for the new client; verify which version actually serves the credential CRUD before relying on v3-only paths.
- **[JetStream retention can be shorter than an outage.]** → Reconcile (D9) is the independent correctness floor: a member is converged even if their event has aged out of the stream.

- **[Full reconciliation cost vs freshness (§3.5).]** → Trade-off accepted: incremental leg for steady-state, scheduled full sweep for the floor (D9). We do not aim for both frequent full sweeps and low cost.

- **[Silently treating a rejected Doorflow write as success — the original sin of §3.2/§3.3.]** → Every request logs status + body on failure; a 4xx on a credential create/update is a first-class observable state; the no-op gate + dead-lettering mean "failed" is a logged state, not an absence of logs.

- **[Treating a `null` PIN in Nexudus as "delete the Doorflow PIN" could mass-revoke working doors.]** → Credential *removal* on Nexudus-absent is a policy decision (Open Question) with a conservative default (log-only / retained-but-disabled) instead of automated hard delete in the first iterations.
- **[Two in-flight `syncOneCoworker(mem)` calls for the same member (event burst + reconcile overlap) can race between read and write.]** → Serialize per-member inside the service (a small in-process lock keyed by member id), and rely on the no-op gate to keep any stragglers benign. Doorflow's `409`-returns-existing on credential create is our safety net against duplicate-credential races (D5).

## Migration Plan

**Phase 0 — Foundation (safe to run anywhere).**

- Bring up the service as a NATS+JetStream consumer; connect to Nexudus with the existing read-only
  credential and to Doorflow with the same legacy HTTP-basic key as a *read-only* baseline (verify the
  admin/api hosts and which major version serves the credential CRUD).
- Add **structured logging**, a **health/readiness endpoint**, and a **`doctor` dry-run subcommand**
  that walks the membership and prints the diffs it *would* apply, without writing anything. This is our
  biggest safety net and our verification tool. It is what makes Phases 1+ auditable.

**Phase 1 — Enable writes, gated by diff parity.**

- Enable `syncOneCoworker` writes under a flag. Keep the legacy `v3_polling` running in parallel.
- Before clobbering anything, verify per-member: our desired state equals the legacy's current
  outcome (spot-check the top N members by change frequency). Any divergence → log it, do not write.

**Phase 2 — Cutoff and decommission legacy.**

- Once a full reconcile pass has run and `doctor` reports *zero* divergences against the legacy
  running service, stop the legacy poller. Keep the legacy branch + `team_group_map.csv` in-tree and
  the recovery CLI (`update_all_users` equivalent) at hand in case of rollback.

**Rollback.**

- Flag-gated: if the new service misbehaves, turn off writes (it becomes a read-only logger), let the
  legacy poller take over again. No data migration; no destructive operations on either the Nexudus or
  Doorflow side in our first iterations (see the `null-credential → delete` risk and its conservative
  default above). The pending-back-write state can be replayed from the durable store on restart;
  nothing else to clean up.

## Open Questions

*None of these block the chosen approach, the specs, or the task breakdown — they are answers to be
confirmed against the live API / environment.*

1. **Doorflow auth scheme for the v3 credential API.** Bearer token + `account.person` scope, or the
   legacy HTTP-basic key? (This may be the root of the §3.2 401s.) — confirm against the live endpoint.
2. **Which Doorflow API major version actually serves person CRUD + credential CRUD + `POST /sync`.**
   v2 (legacy) vs v3 (credential docs). Pick one and pin it.
3. **`credential_types` values in this account** for the `pin` / `keyfob` / `card` slugs — needed
   for the D8 startup discovery step. Discoverable, but worth confirming the shapes (id + name fields).
4. **Nexudus write scope on the service credential.** Legacy was read-only. Confirm the PIN write-back
   (`PUT /api/spaces/coworkers`) is permitted, and whether `sendpinreminder` requires a distinct scope.
5. **Credential-removal policy when Nexudus is `null`.** Conservative default (log-only / retained-but-
   disabled) assumed above — decide whether to ever auto-`DELETE` based on Nexudus-absent.
6. **JetStream stream retention sizing** (max msgs / max bytes / max age) — driven by the expected
   outage length we want to fully replay before falling back to the full-reconcile leg (D9).
