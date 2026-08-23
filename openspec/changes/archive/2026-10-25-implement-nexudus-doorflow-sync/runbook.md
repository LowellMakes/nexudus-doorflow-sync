# Cutover & verification runbook

This runbook records the **manual, live-credential** verifications that close out
tasks 7.3, 7.4, and 7.5. All code for these paths is implemented and unit-covered
(in-process fakes + `httptest`); the steps below are the operator's final
confirmation against real Nexudus/Doorflow accounts.

## 7.4 — Startup capability probe (resolve Design Open Questions 1–4)

Run before ever enabling writes:

```
nexudus-doorflow-sync probe
```

What to look for in the log transcript:
- `doorflow credential types resolved` — the `credential_types` endpoint
  answered and the pin/keyfob/card type-ids resolved (Open Question 3).
- the resolved `auth` scheme (`basic` vs `bearer`) is echoed back, confirming the
  auth path works (Open Questions 1–2).
- a failing probe exits non-zero with the auth/type error (no mid-sync surprise).

Nexudus PIN write permission (Open Question 4) is confirmed live by the in-use-PIN
path in 7.3 (it performs the documented `UpdateCoworkerPin` write-back).

## 7.3 — Sandbox happy path + in-use-PIN path

Against a **sandboxed** Doorflow account and a dev Nexudus account, with
`WRITES_ENABLED=true` and `NEXUDUS_WEBHOOK_...`/PIN-notify enabled:

1. **Happy path** — add/enable a member in Nexudus (contract active, PIN set).
   Confirm the member's Doorflow person is `enabled` with the exact mapped groups
   (team group ∪ BasicMember) **and** a working PIN:
   ```
   member <id>: ... (log shows person upsert + credential set + 1x POST /sync)
   ```
2. **In-use-PIN** — set the member's Nexudus PIN to a value already taken by
   another Doorflow member. Confirm the transcript shows:
   - the `409 value-in-use` on the PIN set,
   - autogeneration to a NEW value (Doorflow + Nexudus read-back match),
   - the back-write to Nexudus succeeds,
   - exactly one "PIN changed" notification to the member.

Log-level `debug` prints Doorflow request/response for each mutation.

## 7.5 — Cutover (parallel → verify parity → decommission)

1. **Parallel**: keep the legacy `v3_polling` running; start `nexudus-doorflow-sync
   run` with `WRITES_ENABLED=false` (read-only/dry-run) so the two never race on
   writes.
2. **Parity**: run the read-only doctor across the membership and confirm it
   reports **zero** members needing change over a full-reconcile window:
   ```
   nexudus-doorflow-sync doctor
   # expect: "<N> member(s); 0 need change(s); 0 writes issued (read-only)"
   ```
   Re-run after a full sweep (`run` startup reconcile) — it must stay at 0.
3. **Enable writes**: set `WRITES_ENABLED=true`, watch the event path re-enable and
   reconcile clean for one full business day, with no `ErrInUse`, no missing
   credentials, and the doctor report staying at zero divergence.
4. **Decommission**: stop the legacy `v3_polling` process. **Retain** in-tree for
   rollback:
   - `data/team_group_map.csv` (the authoritative team→group map),
   - the `doctor` subcommand (read-only inspection), and
   - the `run`/remediation code path (re-enabling + PIN remediation).
   **Rollback**: if the new service misbehaves, re-point the Doorflow webhook back
   to the legacy consumer, flip `WRITES_ENABLED=false` here, and re-run
   `v3_polling`; use `doctor` to confirm the members are in the expected state
   before and after.

Sign-off: `_legacy v3_polling stopped on ___ after a clean doctor parity pass,
by ___._`
