## Purpose

Define how the service consumes Nexudus webhook events from a durable message log and turns them into coworker-scoped syncs, with safe at-least-once delivery and recovery across downtime.

## ADDED Requirements

### Requirement: Consume from the durable event log
The service SHALL consume Nexudus webhook events from a durable message log that is populated by the generic HTTP-to-NATS proxy. The service SHALL acknowledge an event only after it has been durably handled (processed or routed to a defined failure path), so that a crash does not lose an in-flight event.

#### Scenario: Successful processing
- **WHEN** an event is consumed and its coworker sync completes (or converges to a defined no-op)
- **THEN** the service acknowledges the event and advances the consumer position

#### Scenario: Crash while processing
- **WHEN** the service crashes before acknowledging an event
- **THEN** the event is redelivered after the service recovers and is not lost

### Requirement: Map each event to the affected coworker
The service SHALL determine, from each event, the Nexudus coworker it concerns, and dispatch a sync for that coworker. Events that do not correspond to a syncable coworker MAY be ignored, but MUST NOT cause processing to fail.

#### Scenario: Member event
- **WHEN** an event identifies a coworker
- **THEN** the service triggers a sync for that coworker

#### Scenario: Non-member-relevant event
- **WHEN** an event does not map to a syncable coworker
- **THEN** the service acknowledges the event without a sync and does not error

### Requirement: Safe under redelivery
The event path SHALL be safe to execute repeatedly, so that redelivery of an already-applied event produces no duplicate or incorrect Doorflow state.

#### Scenario: Redelivered event
- **WHEN** an event for a coworker whose Doorflow state already matches the desired state is delivered again
- **THEN** the resulting sync performs no additional mutations

### Requirement: Recover events arriving during downtime
Events that occur while the service is unavailable SHALL still be processed after it restarts, for as long as the durable log retains them.

#### Scenario: Service was down
- **WHEN** an event is published while the service is down and the service is later started
- **THEN** the service processes that event (within the durable log's retention bounds)

### Requirement: Robust handling of malformed or slow processing
A malformed, unrecognized, or persistently failing event MUST NOT crash the consumer or permanently block processing of other events. Such events MUST be logged and routed to a defined failure path (e.g. skipped or dead-lettered) so that the consumer can continue.

#### Scenario: Malformed payload
- **WHEN** an event body cannot be parsed into a coworker
- **THEN** the service logs the event, acknowledges or dead-letters it, and continues processing subsequent events

#### Scenario: Repeatedly failing sync
- **WHEN** a coworker sync repeatedly fails
- **THEN** the service applies backoff / redelivery limits and surfaces the failure rather than blocking the pipeline indefinitely
