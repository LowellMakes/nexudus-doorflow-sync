# Project History & Prior-Iteration Issue Log

> Context for the Go re-implementation in this repository. Records where this project
> has been, how the prior (Python) system worked, and what it broke.

## 1. Lineage

### 1.1 Generation 1 — Python polling sync (2022–2026)

The original system was written by **Ben Brown** (brown.b774@gmail.com / avimelek@proton.me)
as a collection of Python 3 scripts (`nexudus_doorflow_sync.py`, `sync_readers.py`, …) and ran
as a systemd service on a Linux container named **`cameo`**.

**Model (polling):**

1. Every X seconds, query Nexudus for member accounts with new/updated data.
2. Merge several Nexudus REST sources (users, dues/invoices, teams) into a local **SQLite3** DB
   and diff to find members whose Doorflow state changed (name, email, PIN, fob number,
   groups, enabled/disabled).
3. Push changes to the Doorflow REST API, then issue Doorflow's *sync changes* command so the
   physical readers update their local access database.
4. Repeat.

**Timeline:**

- **~2022** — repo created with an API key accidentally committed in history
  (`sync_readers.py#L5`, per Robby's 2024-04-21 note).
- **2024-04-21** — Mike Rushton opens the IT Committee thread
  ["Nexudus / Doorflow integration and sync application help"](https://app.basecamp.com/3376147/buckets/2864270/messages/7317669215),
  makes the repo public (`github.com/LowellMakes/nexudus-doorflow-sync`), and begins service-account /
  ansible-playbook work.
- **2024-04-25** — Robby prunes the leaked credentials from git history.
- **2024-05-03** — Mike observes the copy *deployed on `cameo`* is *"a broken repo (no .git)"* —
  the code on that server was shipped as plain files, with no `.git`, so the exact version
  running in production can't be read from the box. Ben responds by cleaning up `v3_polling`
  (commits `d29c514` "clean up" and `dae361a` "Update README.md", both 2024-05-03), pulling
  that version onto `cameo` and reloading the service; he verifies it works. Note: the
  remote tip of `v3_polling` is later still `f225fe3` ("Adding ResinRoom & JewleryShop Team
  IDs; adding small sleep in while true", 2026-01-08) — that is merely the last commit
  *pushed to the branch*; whether `cameo` was ever updated to it is not verifiable.
  The legacy repo root also contains `passwords.py` and `nexudus_doorflow_sync.config`,
  the natural suspect location for the §3.1 leaked key (contents not inspected).
- **2024-08 → 2026-04** — keeps running as the production sync, with outages (see §3).
  The *last* production log we have in the Basecamp record is from `cameo` on **2026-04-13**.

The Python code lives on the jj history branch `2024-11-14T16:40:28-52594f57150c` in this
repository and is worth consulting for the legacy event→Doorflow mappings.

### 1.2 Generation 2 — Webhook + NATS redesign (spring–summer 2025)

- **2025-03-31** — Mike schedules a *"Nexudus/Doorflow sync design planning"* session
  (IT Committee calendar, session 2025-04-03 18:00).
- **2025-05-29** — Basecamp card ["Recode Doorflow/nexudus Sync"](https://app.basecamp.com/3376147/buckets/2864270/card_tables/cards/8703081669)
  opens in the IT Committee (still *In progress*). Robby's design comment:
  - Receive Nexudus **webhooks** via `http-nats-proxy`
    (git.robbyzambito.me/http-nats-proxy), which republishes them onto **NATS**.
  - A consumer reads NATS, translates Nexudus events into Doorflow updates, and pushes them
    to the [Doorflow REST API](https://developer.doorflow.com/docs/).
  - **Pull Nexudus state at startup** to recover events lost while the service was down.
  - **Verify the webhook signature header** so only genuine Nexudus messages are processed.
  - Deploy to LM infrastructure.
- **2025-06-23** — status comment: Nexudus→NATS flow is working; the remaining work is
  exactly the list above (signature verification, event translation, Doorflow push,
  startup catch-up, deployment). Nothing beyond plumbing was finished.

This is the design the current codebase is implementing.

### 1.3 Generation 3 — Go rewrite (2026, this repo)

- **2026-02-05** — Robby creates the successor repos, all under `github.com/LowellMakes`,
  all jj-versioned, all initially empty:
  `nexudus-doorflow-sync` (this one), `nats-doorflow-sync`, `nexudus-doorflow-doctor`,
  and `nexudus-notifier`.
- **2026-02-05** — jj commit `start again`: fresh Go module
  (`github.com/LowellMakes/nexudus-doorflow-sync`, Go 1.23.5).
- **2026-06-22** — jj commit `add client`: a **go-swagger generated Nexudus REST client**
  vendored under `./internal/nexudus` (hundreds of DTOs across spaces, coworker,
  billing, and all supported door integrations).
  Regenerate with:
  `swagger generate client -f https://spaces.nexudus.com/swagger/docs/v1 -t ./internal/nexudus --skip-validation`
- **2026-08-22** — uncommitted jj working changes: `cmd/main.go` now connects to NATS
  (`go.uber.org/nats`) — the first step of the consumer side of the pipeline.

So: the Nexudus→NATS half (Gen 2) works; this repo is where the NATS→Doorflow half,
startup catch-up, and deployment are being rebuilt.

## 2. How the system actually works (field notes)

These behavioral facts came up in Basecamp discussions and shaped several incidents. The
re-implementation must respect all of them.

- **Nexudus is the system of record** for membership (spaces.nexudus.com): accounts, teams/
  spaces, PINs, fob numbers, and whether dues/invoices are current.
- **Doorflow readers do NOT call the Doorflow API when a badge is presented.** Each reader
  hosts a *local copy of the access database*, which Doorflow updates after backend changes.
  A change pushed to the Doorflow backend is invisible at the door until Doorflow's
  *"sync changes"* command is issued (https://developer.doorflow.com/docs/#syncing-changes).
  Stale-grant and stale-revocation bugs all trace back to this.
- **Enabling/disabling is policy-driven**, not identity-only. The legacy pipeline checked
  membership status **and** `Invoices Due` (legacy log: *"Checking if User should be enabled
  in Doorflow → Invoices Due: False → User Should be Enabled"*). Non-paying members lose door
  access; this is the core product behavior.
- **Team → Doorflow group mapping.** Legacy payloads map Nexudus teams onto Doorflow groups,
  e.g. `Basic Member` (group 4482) and `Woodshop` (group 4622). Member payloads carry
  `first_name`, `last_name`, `email`, `pin`, `key_fob_number`, `system_id` (`NX<member_id>`),
  and `notes: "Updated by syncTools"` — so Doorflow-side records can be recognized as
  sync-managed.
- **The legacy service merged multiple Nexudus REST sources** (users, invoices, teams) into
  SQLite and diffed, because no single endpoint gives the complete picture.
- **Monitoring (state of the art):** a daily `check_doorflow_api` cron that emails
  it@lowellmakes.com on failure (Mike, 2024-08-28). There is no real health endpoint or
  webhook-processing alerting.

## 3. Historical issues (from Basecamp records)

### 3.1 Credentials leaked in public repo history

**When:** repo created ~2022; discovered 2024-04-21; fixed 2024-04-25.
**What happened:** A Nexudus API key was committed to the repo before it was made public
(Robby pointed at `sync_readers.py#L5` as the likely location). After the repo was made
public on 2024-04-21, the history purge of the leaked credential began the same week and
completed by 2024-04-25.
**Consequence / lesson:** any code committed to this repo may be read by the public internet.
- Credentials must live in env vars / service secrets, never in the tree.
- The 2026-02-04 Shop Captains chat (Mike: *"sorry, why are we advertising this? This is means
  as a backend tool for the Nexudus/Doorflow sync."*) shows the public-visibility concern is
  still live — keep the repo private or scrub anything sensitive before pushing.

### 3.2 Doorflow API 401s — fobs stopped programming (Aug 2024)

**When:** 2024-08-23 → 2024-08-28.
**What happened:** Two members complained within hours that their fobs no longer worked
(*"Failed to create new user in Doorflow"*, API returning `401 Unauthorized` from
`api.doorflow.com`, per Ben's pasted logs).
**Diagnosis:** inconclusive. Mike: *"I don't know how this is fixed. Robby had said on the
sign-up reader that Doorflow might have changed something."* Either the cred was rotated /
rejected on Doorflow's side, or the API contract shifted.
**Consequence / lesson:**
- Doorflow *can* change API behavior under us, and the failure mode is silent from the
  member's POV (fob just doesn't work).
- The only diagnosis that worked was the service's own log line + the HTTP status. The
  rewrite must log Doorflow request/response status and error bodies, and alert (the
  `check_doorflow_api` cron only caught hard failure, not 4xx on individual member writes).

### 3.3 PIN conflict loop — "Mismatched Access Pins" (Apr 2026)

**When:** 2026-04-13, [IT Committee thread](https://app.basecamp.com/3376147/buckets/2864270/messages/9781397989).
**What happened:** Member Rick Harmon called three times in a month unable to enter. The sync
log (from `cameo`, 2026-04-13 14:35) shows the loop: `Update Needed: Mismatched Access Pins` ⇒
creates Doorflow payload with PIN `14453`. In the Doorflow UI (screenshot attached) the PIN
*already matched* what's in Nexudus. Mike's workaround — delete the account in Doorflow and
force a re-sync — worked once, but the system drifted back into the same state.
**Hypotheses raised:**
- Sam Brown: maybe another member shares that PIN and Doorflow rejects the duplicate
  ("try changing or removing the pin number").
- Patrick Shields: does Nexudus even prevent two members from picking the same PIN? And
  the legacy code pushes credentials *inline in the person endpoint* rather than the
  dedicated credentials endpoint — so a conflict rejection from Doorflow may be swallowed
  instead of surfaced.
**Consequence / lesson:**
- PIN uniqueness is a real constraint at Doorflow, not guaranteed by Nexudus.
- Use Doorflow's explicit credential APIs where they exist, and **read the error body** —
  a silent rejection here is what turned a data collision into a 3-week support loop.
- Detect-and-recover: this incident was fixed by delete+recreate, so idempotent full
  re-push of one member's state must remain a one-command recovery play.

### 3.4 Webhook event loss & missing reconciliation

**When:** recurring; documented 2023-03-08 and 2023-05-22.
**What happened:**
- 2023-03-08 (Town Square): *"The service that handles webhook messages from Nexudus hit a
  hitch around midnight."* — recovered by manually pushing a full update of all members.
- In the May 2023 IT Committee design discussion, Derrell characterized the running system
  as *"depends on events being generated"* and asked what protects against missed events;
  the design he described (and the rewrite adopted) is: event-driven sync **plus** a
  scheduled/one-time full reconciliation pass.
**Consequence / lesson:** webhooks (and in Gen 2, the NATS proxy hop) are lossy in practice.
The rewrite must treat NATS as an optimization, and Nexudus REST as ground truth:
startup catch-up and periodic reconciliation are non-negotiable, and webhook signature
verification is part of the trust model, not a nice-to-have.

### 3.5 Full-sync performance limits

**When:** 2023-05-22 (constraint) and 2022-01-25 (rate-limit concern).
**What happened:** Ben: *"I do have code that searches every member and pushes updates for them,
and it takes over an hour to complete."* — so he was against frequent global syncs.
Earlier, the Membership Committee asked whether our Doorflow polling was hitting the
subscription's API rate limits.
**Consequence / lesson:**
- A full reconcile is *possible but expensive*: ~1 hour for the whole membership, and it
  burns Nexudus + Doorflow API quota.
- The rewrite should make reconciliation **incremental where possible** (Nexudus's
  updated-since style queries — which is what the legacy code was polling anyway), batch
  Doorflow writes, and schedule the full path to low-hours-of-the-day.

### 3.6 Fragile deployment on `cameo`

**When:** recurring; documented 2024-04-21 → 2025-04-27.
**What happened:**
- Code ran from a **git working directory without `.git`** on `cameo` — Mike could not tell
  which branch was live there (2024-05-03; that deployment was re-based onto `v3_polling`
  the same day — see §1.1).
- It ran under a **personal user account**; Mike proposed a service account and an ansible
  playbook so servers could be rebuilt (2024-04-21). Neither shipped.
- No health check on the webhook path: *"There's no health-check to be had. The service was
  running. The service is supposed to be monitoring, but it wasn't."* (Mike, Town Square,
  2025-04-27).
**Consequence / lesson:** the rewrite needs to be deployable as a clean systemd unit (or
container) from this repo, with a health endpoint we can probe, a service account, and a
rebuild procedure that doesn't depend on the state of one server.

### 3.7 Unexplained outages (Mar 2026 and others)

**When:** recurring; most recent 2026-03-05.
**What happened:** on the *"Recode Doorflow/nexudus Sync"* card (2026-03-05):
- Mike: *"any progress here? We REALLY need that replacement. The current system just stopped
  doing anything at around 4am this morning with no logged reason why."*
- Scott Wofford: *"Do we have any indication as to what went wrong?"*
- Mike: *"No idea. I'm less concerned about how the current system is broken and more
  concerned with rewriting it from the ground up to get something that is stable and
  supportable."*
That quote is the mandate for this repo. Recurring members in Town Square / Membership
(Christopher's 2023-09 *"Sometimes when adding someone there's a communication failure and
nexudus doesn't sync right with doorflow"*; Toph's 2024-08 tour-guide fobs) are the cost the
members pay when it's broken.

**Consequence / lesson:**
- Every event must be traceable: in-bound NATS message ID, Nexudus event type, Doorflow
  request, Doorflow response. "No logged reason why" must be impossible.
- Failure must be *visible* — dead-letter / alert on Doorflow 4xx/5xx, on NATS consumer lag,
  and on reconciliation drift.

## 4. Implications for the re-implementation

Concrete requirements the design above must satisfy, each traced to a real incident:

1. **NEXUDUS IS GROUND TRUTH.** NATS/webhook events are an optimization; startup catch-up
   and periodic reconciliation pull authoritative state from the Nexudus REST API
   (the client is already vendored in `internal/nexudus/`). *(§3.4, §3.5.)*
2. **Verify Nexudus webhook signatures** before processing, so we can't be poisoned by a
   forged or misdirected event. *(Gen 2 design; §3.1.)*
3. **Issue Doorflow's "sync changes" command after each batch of member/group writes.**
   Backend updates are invisible at the reader until it does. *(§2 field note; recurring
   stale-access reports.)*
4. **Use Doorflow's explicit credentials APIs, and log + alert on 4xx/5xx responses** — a
   silent rejection is the root cause of §3.2 and §3.3.
5. **Model PIN (and fob) uniqueness as a domain constraint**, and surface collisions as
   actionable errors rather than a drifting "mismatched access pins" state. *(§3.3.)*
6. **Idempotent, member-scoped full re-push** as a one-command recovery play — the
   delete+recreate trick from §3.3 should become a CLI in this service.
7. **Structured, traceable logs**: inbound event ID, member ID, Doorflow request ID, status,
   error body. "No logged reason why" is not acceptable. *(§3.7.)*
8. **Secrets via env / service secret only**; nothing ever in this tree. The repo is
   treated as publicly readable. *(§3.1.)*
9. **Deployable as a clean systemd unit (or container) with a health endpoint**, a service
   account, and a rebuild procedure. *(§3.6.)*
10. **Batch and rate-limit Doorflow writes**; schedule full reconciles in low-hours, keep
    incremental ones continuous. *(§3.5.)*

### Repo map (today)

- `cmd/main.go` — entrypoint; NATS connect stub (uncommitted, 2026-08-22).
- `internal/nexudus/` — generated REST client (go-swagger from
  `https://spaces.nexudus.com/swagger/docs/v1`).
- Still to write: NATS consumer + signature verify, event → Doorflow translator, Doorflow
  REST client, reconcile loop, deployment (systemd + service account), tests, alerting.
- Sibling repos: `nats-doorflow-sync` (NATS→Doorflow bridge? or the http-nats-proxy side),
  `nexudus-doorflow-doctor` (diagnostics / recovery CLI?), `nexudus-notifier` (member/
  staff notifications?) — all currently empty shells.
