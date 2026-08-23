# doorflow-sync Specification

## Purpose

Define how the service synchronizes a single coworker's Doorflow footprint — person record, group memberships, and credentials — to a desired state derived from authoritative Nexudus data, and commits it so physical readers converge.

## Requirements

### Requirement: Desired state is derived from authoritative Nexudus, not the triggering event
The service SHALL compute a coworker's desired Doorflow state from authoritative Nexudus REST data (the coworker record and invoicing state) at the time of syncing. The service MUST NOT rely on the payload of the triggering webhook event to determine door state, so that a partial, stale, or absent event body cannot produce an incorrect Doorflow write.

#### Scenario: Event payload disagrees with Nexudus
- **WHEN** a sync is triggered for a coworker whose event body differs from their current Nexudus record
- **THEN** the service uses the current Nexudus record to determine the desired Doorflow state

### Requirement: Person record reconciliation
The service SHALL reconcile a coworker's Doorflow person record (first/last name, email, `enabled`, group memberships, and a stable management identifier of the form `NX<coworker-id>`). If no matching Doorflow person exists, the service SHALL create one; if one exists, the service SHALL update it to the desired state.

#### Scenario: New member
- **WHEN** an eligible coworker has no corresponding Doorflow person
- **THEN** the service creates the person with the desired name, email, groups, and `NX<coworker-id>` identifier

#### Scenario: Existing member updated
- **WHEN** an eligible coworker already has a Doorflow person and any desired attribute differs
- **THEN** the service updates the person to the desired state

#### Scenario: No difference
- **WHEN** the Doorflow person already matches the desired state
- **THEN** the service performs no person write

### Requirement: Exact-set group membership
The service SHALL make a coworker's Doorflow group memberships equal to the set of their mapped Nexudus teams plus the base membership group. A team the member no longer belongs to in Nexudus SHALL be removed from their Doorflow groups, not merely left in place.

#### Scenario: Member added to a team
- **WHEN** a member gains a team in Nexudus that maps to a Doorflow group
- **THEN** the member's Doorflow groups include that group

#### Scenario: Member removed from a team
- **WHEN** a member loses a team in Nexudus that maps to a Doorflow group the member currently has in Doorflow
- **THEN** the member's Doorflow groups no longer include that group

### Requirement: Enablement is policy-driven
A coworker's Doorflow `enabled` state SHALL be true if and only if the coworker has at least one active contract in Nexudus AND has no overdue invoice in Nexudus. Otherwise the member SHALL be disabled in Doorflow, with their record retained.

#### Scenario: Dues cleared
- **WHEN** a member with a contract pays all overdue invoices
- **THEN** the member is enabled in Doorflow

#### Scenario: Dues become overdue
- **WHEN** a member with a contract has an overdue invoice
- **THEN** the member is disabled in Doorflow and their record is retained

#### Scenario: No contract
- **WHEN** a coworker has no active contract in Nexudus
- **THEN** the member is disabled in Doorflow (or not created) and no door access is granted

### Requirement: Credential reconciliation
For each supported simple credential type (PIN, key-fob, access card), the service SHALL reconcile the member's Doorflow credential to match their Nexudus value: create it if absent and present in Nexudus, change it if it differs, and (subject to the credential-removal policy) remove it if absent in Nexudus. The service SHALL treat at most one credential per type per person, and reprocessing the same desired state SHALL be a no-op.

#### Scenario: Add a missing PIN
- **WHEN** a member has a PIN in Nexudus but no PIN credential in Doorflow
- **THEN** the service creates the PIN credential in Doorflow

#### Scenario: Change a PIN
- **WHEN** a member's Nexudus PIN differs from their Doorflow PIN
- **THEN** the service updates the Doorflow PIN to the Nexudus value unless the update is rejected as in-use (see `pin-remediation`)

#### Scenario: Repeating the same state
- **WHEN** the member re-syncs and their Doorflow credential already matches the desired value
- **THEN** no credential write is performed and no duplicate is created

### Requirement: Commit changes for readers
After applying one or more Doorflow mutations (person and/or credential) for a batch of members, the service SHALL issue Doorflow's "sync changes" command so that physical readers pull the updated access database. The command SHALL only be issued when at least one mutation occurred.

#### Scenario: Member change applied
- **WHEN** at least one person or credential mutation is successfully written to Doorflow
- **THEN** the service issues the Doorflow sync-changes command after the batch

#### Scenario: Nothing changed
- **WHEN** a full pass results in zero mutations
- **THEN** the service does not issue the sync-changes command

### Requirement: Idempotency under at-least-once delivery
Because events may be delivered more than once, reprocessing a coworker's sync SHALL NOT create duplicate Doorflow persons or duplicate credentials and SHALL converge to the same final state.

#### Scenario: Duplicate event
- **WHEN** the same sync is triggered twice for the same coworker with unchanged Nexudus state
- **THEN** the second pass performs no additional mutations

### Requirement: Contacts are skipped
A Nexudus coworker who has no member identity (no underlying user) SHALL be skipped: the service MUST NOT create or update a Doorflow person for such a record.

#### Scenario: Contact-only record
- **WHEN** a synced record is a contact rather than a member
- **THEN** no Doorflow person is created or modified for it

### Requirement: Traceable, observable processing
Each sync SHALL be logged with enough detail to reconstruct the outcome: the coworker identifier, and for each Nexudus and Doorflow request, the status and (on failure) the response body. A rejected or failing Doorflow write MUST NOT be treated as a silent success.

#### Scenario: Doorflow write rejected
- **WHEN** a Doorflow request returns a 4xx or 5xx status
- **THEN** the service logs the coworker, the endpoint, the status, and the response body, and marks the outcome as failed
