## Purpose

Define how the service automatically resolves PIN collisions: when a member-chosen PIN is rejected as in-use, regenerate a unique PIN in Doorflow, reflect that generated value back into Nexudus (the member-facing record), notify the member, and prevent a regeneration churn-loop.

## ADDED Requirements

### Requirement: Detect an in-use PIN as a distinct outcome
When a member's Nexudus PIN cannot be set because it is already in use, the service SHALL treat this as a distinct, recognizable, actionable outcome, not as a silent success or an undifferentiated failure.

#### Scenario: PIN already in use
- **WHEN** the service attempts to set a member's PIN and Doorflow indicates the value is already in use
- **THEN** the service recognizes this as a PIN-in-use conflict and proceeds to remediate

### Requirement: Auto-regenerate a unique PIN
On a PIN-in-use conflict, the service SHALL generate a new, unique PIN in Doorflow using Doorflow's auto-generation, and SHALL capture the generated value from Doorflow's response so it can be recorded and reported.

#### Scenario: Conflict resolved by generation
- **WHEN** a member's PIN is in use
- **THEN** the member ends with a working, unique PIN in Doorflow, and the service has recorded what that generated value is

### Requirement: Reflect the generated PIN back into Nexudus
The service SHALL set the generated PIN on the member's Nexudus coworker record so that Nexudus (where members view and change their PIN) reflects the value that is actually active in Doorflow. This update SHALL be a read-modify-write of the coworker so that unrelated coworker fields are not clobbered.

#### Scenario: Read-modify-write write-back
- **WHEN** the generated PIN is written back to Nexudus
- **THEN** the coworker's other fields remain unchanged and the PIN field equals the generated value

### Requirement: Guard against regeneration churn
The service SHALL remember, durably, when a generated PIN has been created but its Nexudus write-back is not yet confirmed. While that state is pending, subsequent passes for the same member MUST retry the write-back and MUST NOT generate a new PIN again. Once the Nexudus record is confirmed to equal the Doorflow PIN, the pending state SHALL be cleared.

#### Scenario: Failed write-back does not loop
- **WHEN** the Nexudus write-back fails and the member is re-synced on a later pass
- **THEN** the service retries the write-back of the existing Doorflow PIN instead of generating a different new PIN

#### Scenario: Write-back then confirmed
- **WHEN** a subsequent pass confirms the Nexudus PIN equals the Doorflow PIN
- **THEN** the pending write-back state for that member is cleared

### Requirement: Notify the member of a PIN change
When a member's PIN is changed as a result of remediation, the member SHALL be notified that their PIN changed and able to see/use the new value.

#### Scenario: Notification on remediation
- **WHEN** the service auto-changes a member's PIN
- **THEN** the member is notified of the change

### Requirement: Scoped to PIN only
The remediation behavior applies to PINs only. Key-fob and access-card credentials SHALL NOT be auto-generated or auto-removed as a side effect of remediation; their removal is governed by the credential-removal policy.

#### Scenario: Conflicting PIN does not touch the fob
- **WHEN** a member's PIN is in use and remediated
- **THEN** the member's key-fob and access-card credentials are left unchanged
