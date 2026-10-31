# Ortsnetz Voltage Reporting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Optionally report grid connection point voltage samples and per-phase statistics to Ortsnetz-Auslastung using existing `/metrics` scrapes.

**Architecture:** Extend the existing grid-meter response with its measurement timestamp. A shared, synchronized reporter accepts validated measurements from the meter collector, aggregates scrape samples, and starts a bounded asynchronous POST at most once per 300 seconds. It never requests the inverter independently.

**Tech Stack:** Go, existing configuration and Prometheus HTTP handler, standard library HTTP and synchronization, Go testing and existing testify dependency.

**Spec:** The approved design is recorded below; [API reference](https://github.com/thomaslehmann1234/datenerfassung-ortsnetz-auslastung/blob/0614151558a82012aad13379e5815fb0802349d6/API.md) and [Fronius integration](https://github.com/thomaslehmann1234/datenerfassung-ortsnetz-auslastung/blob/0614151558a82012aad13379e5815fb0802349d6/fronius-ortsnetz-auslastung/README.md) describe the receiver and meter fields.

## Global Constraints

- Only `--ortsnetz.location=latitude,longitude` enables reporting; its environment equivalent is `ORTSNETZ__LOCATION`. Empty/unset means disabled. Meter realtime collection must be enabled.
- Send only timestamp, coordinates, phase voltages, and optional per-phase minimum, maximum and arithmetic mean; do not send frequency or unrelated data.
- Use actual valid samples from the selected grid connection meter, never cached Prometheus gauges. Missing phases use `-1`; at least one valid phase is required.
- First valid scrape after startup sends current values without statistics. Subsequent attempts are at least 300 seconds apart; discard failed attempts and reset their interval. No scrape means no send.
- Use a background request with a timeout; the metrics response must not wait for the remote endpoint. Synchronize shared state across concurrent scrapes. No durable queue or additional inverter requests.
- Do not create commits without explicit user instruction. Keep managed Makefiles unchanged.

## Review Focus

- Missing or malformed coordinates, including `0,0`: configuration tests in Task 1.
- Meter timestamps missing, stale, or in the future: validation tests in Task 2.
- Invalid and missing phase values, including all missing: sampling tests in Task 2.
- Overlapping scrapes or a blocked remote endpoint: concurrency and HTTP tests in Task 3.
- Rejected or failed POSTs: interval reset and retry timing tests in Task 3.

## File Structure

- `cfg/types.go`, `cfg/config.go`, `cfg/config_test.go`: location option and validation.
- `pkg/fronius/symo.go`, `pkg/fronius/symo_test.go`: preserve grid meter timestamp.
- `ortsnetz.go`, `ortsnetz_test.go`: validation, aggregation, scheduling and HTTP reporting.
- `main.go`, `metrics.go`, `metrics_test.go`: initialize one reporter and pass each successful meter response to it.
- `README.md`: configuration, transmitted data and sampling behavior.

### Task 1: Configuration and grid meter timestamp

**Files:** Modify `cfg/types.go`, `cfg/config.go`, `cfg/config_test.go`, `pkg/fronius/symo.go`, `pkg/fronius/symo_test.go`, `metrics_test.go`.

**Interfaces:** Add `OrtsnetzConfig` with `Location string` and `Configuration.Ortsnetz`; preserve `TimeStamp` on `SymoMeterRealtimeData` in a form that tolerates malformed JSON values without failing the meter response. Expose a configuration validation function returning parsed coordinates and an error, including whether reporting is enabled.

- [ ] Add table-driven tests for unset location, valid `52.52,13.40` and `0,0`, whitespace, missing/extra values, non-numbers, non-finite values, latitude outside [-90,90], longitude outside [-180,180], and a disabled meter collector when location is set. Verify flag and environment parsing and CLI precedence.
- [ ] Add client tests that preserve a selected grid meter's Unix timestamp, distinguish a missing timestamp from zero, and retain meter data when the timestamp has an unexpected JSON type. Verify existing metrics remain available with reporting disabled and missing or malformed timestamps.
- [ ] Run `go test ./...`; confirm the new tests fail for the missing option and timestamp.
- [ ] Implement the option and validation, and decode the timestamp. Make invalid enabled configuration fail clearly on startup without changing disabled behavior.
- [ ] Run `go test ./...`; expect success.

### Task 2: Validate and aggregate voltage samples

**Files:** Create `ortsnetz.go`, `ortsnetz_test.go`.

**Interfaces:** Define an internal `voltageSample` containing measurement time and three optional voltages, a validation function for selected meter responses, and an accumulator that adds samples and returns and resets per-phase statistics. Use the meter's Unix `TimeStamp` for `observed_at` and pass the current time explicitly for deterministic validation tests.

- [ ] Test that only finite voltages greater than 0 and at most 500 are sampled; represent missing or invalid phases as `-1` in required fields and omit their statistics. Skip measurements with no valid phase.
- [ ] Test that missing, zero, malformed or more than 600 seconds old or future-skewed timestamps are rejected; accept timestamps within 600 seconds of the observation time. This threshold matches the reference Fronius integration's default. A failed meter request or rejected measurement must never reuse an earlier value.
- [ ] Test per-phase minimum, maximum and arithmetic mean over valid scrape samples, including changing phase availability and reset between intervals. Statistics describe samples, not interpolated values.
- [ ] Run `go test ./...`; confirm the new tests fail before implementation.
- [ ] Implement sample validation and per-phase aggregation with an explicit reset operation.
- [ ] Run `go test ./...`; expect success.

### Task 3: Schedule and send measurements

**Files:** Modify `ortsnetz.go`, `ortsnetz_test.go`, `main.go`, `metrics.go`, `metrics_test.go`.

**Interfaces:** Initialize one synchronized `voltageReporter` when configured and pass it to the existing collector path. Its `Observe(data *fronius.SymoMeterRealtimeData, now time.Time)` validates and accumulates samples, makes an immutable payload snapshot under a mutex and schedules a POST to `https://www.ortsnetz-auslastung.de/v1/measurements` using an injectable HTTP client/endpoint in tests.

- [ ] Use `httptest.Server` and a controlled clock to test the first POST, no second attempt before 300 seconds, a new attempt at 300 seconds on a valid scrape, required JSON fields, UTC measurement timestamp, omitted frequency and correctly calculated statistics.
- [ ] Test concurrent calls at the deadline schedule exactly one POST. If the previous request is blocked, verify no new POST starts until it completes, and exactly one is scheduled by eligible concurrent scrapes afterward. Verify the scrape handler completes while the server blocks and that requests have a finite timeout.
- [ ] Test network failure and non-202 responses are logged, do not fail `/metrics`, are not queued, and do not trigger retries on every scrape; the next valid scrape at or after 300 seconds starts a new interval. Test disabled configuration performs no POST.
- [ ] Run `go test ./...`; confirm the new tests fail before integration.
- [ ] Implement reporting with a bounded HTTP timeout, status handling and response-body closure. Include the sample triggering a subsequent send in that interval; atomically reserve the attempt time and snapshot/reset aggregation before launching the request. Do not permit overlapping POSTs; if one is still in flight, retain samples and defer the next attempt to a later scrape.
- [ ] Connect the reporter to successful meter collection in sequential and parallel modes, without changing existing metrics or making a second Fronius request.
- [ ] Run `go test ./...` and `go test -race ./...`; expect success.

### Task 4: Document and verify

**Files:** Modify `README.md`.

- [ ] Document flag and environment examples, required meter collection, coordinates sent to the external service, phase voltages and statistics, 300-second minimum attempt spacing, first scrape behavior, discarded failures, scrape dependence and the fact that statistics cover samples only.
- [ ] Run `go fmt ./...`, `go vet ./...`, `go test ./...`, and `go test -race ./...`; expect success. Review the diff for unrelated changes and simplify where appropriate.
- [ ] Have an independent reviewer inspect the completed changes, address relevant findings, and re-review substantial corrections.
