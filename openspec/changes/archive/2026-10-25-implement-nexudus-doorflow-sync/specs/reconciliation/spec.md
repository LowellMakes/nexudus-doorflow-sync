## Purpose

Define how the service recovers from downtime and detects drift by authoritatively pulling Nexudus state and driving the same per-coworker sync, acting as a correctness floor independent of the event log.

## ADDED Requirements

### Requirement: Durable-log replay is not the sole recovery mechanism
Because the durable event log has a bounded retention, the service SHALL provide an independent path that converges Doorflow state from Nexudus by pulling authoritative data directly, without depending on having received the corresponding events.

#### Scenario: Events beyond retention
- **WHEN** a change occurred beyond the event log's retention window while the service was down or broken
- **THEN** reconciliation can still correct the Doorflow state by reading Nexudus

### Requirement: Start-up reconciliation
On start, the service SHALL run a reconciliation to converge any state that was missed since it last ran.

#### Scenario: Restart after outage
- **WHEN** the service starts after an outage
- **THEN** it reconciles the affected or stale members and converges their Doorflow state

### Requirement: Incremental (updated-since) reconciliation
The service SHALL be able to identify members whose state changed since a reference time — including both coworker updates and invoice payment-cleared events — and sync those members, rather than always syncing the entire membership.

#### Scenario: A subset changed since last run
- **WHEN** only some members were updated or had invoice status changes since the reference time
- **THEN** the service syncs that subset

#### Scenario: Invoice paid clears dues
- **WHEN** a member's overdue invoice is paid in Nexudus
- **THEN** the service syncs that member (re-evaluating enablement)

### Requirement: Scheduled full reconciliation
The service SHALL support a full-membership reconciliation, runnable on a schedule, that converges every member's desired state. Full reconciliation MUST be batched and rate-limited so as to stay within Nexudus and Doorflow API limits.

#### Scenario: Scheduled full pass
- **WHEN** the scheduled reconciliation runs
- **THEN** every member's Doorflow state is converged and Doorflow is committed, with API usage within the configured limits

### Requirement: Single source of desired-state logic
Reconciliation SHALL use the same desired-state synchronization logic as the event path. The service MUST NOT maintain a second, separate translation of Nexudus state into Doorflow writes.

#### Scenario: Same rule for both paths
- **WHEN** a member is reconciled via the event path vs. via the reconciliation path
- **THEN** both produce the same desired Doorflow state

### Requirement: Drift is corrected, not merely reported
When a member's current Doorflow state differs from the desired state, the service SHALL apply the sync (and the reader commit) to correct it, rather than only logging the difference.

#### Scenario: Detected drift
- **WHEN** reconciliation finds a member whose Doorflow groups, enablement, or credentials differ from desired
- **THEN** the service corrects it through the doorflow-sync path
