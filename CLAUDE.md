# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

LLM API performance benchmark tool, written in Go (module `github.com/user/llm-test-tool`, go 1.25.2). Drives high-concurrency load against an OpenAI-compatible `chat/completions` endpoint and emits HTML/JSON/Markdown reports. The only third-party dep is `gopkg.in/yaml.v2` — config parsing only.

The `cmd/main.go` entry point exclusively wires up the **performance tester**. The other test runners under `internal/tester/` (functional, stability, security, compatibility) compile but are not reachable from `main` — they exist as scaffolding and should not be assumed to be production-ready.

## Build / Run

```bash
go mod tidy
go build ./cmd/main.go        # produces ./main
./main -test-case-file test-data/large-test-cases.json
```

There are no unit tests in the repo (`*_test.go` files don't exist). `go vet ./...` and `go build ./...` are the available correctness checks.

Key CLI flags (all override the matching `config.yaml` value, see precedence below):

- `-config` — path to YAML config (default `config/config.yaml`)
- `-concurrency`, `-requests-per-worker` — load shape; total requests = product of the two
- `-api-key`, `-base-url`, `-model` — API target
- `-test-case-file` — **required** (no built-in default); JSON array of full request bodies
- `-retry-count` — pass `0` for benchmarks (retries hide real latency/failure rates)
- `-format` — `html` (default) | `json` | `markdown`

Real API keys go in `config/config.local.yaml` (gitignored). `config/config.yaml` holds placeholders only.

## Architecture

### Configuration precedence

`cmd/main.go` enforces **CLI flag (only when explicitly set) > YAML > built-in fallback**. The "explicitly set" check uses `flag.Visit` to populate a `setFlags` map — this is deliberate: it prevents the case where the CLI default value (e.g. `concurrency=0`) silently overrides a real YAML setting. When editing flag handling, preserve this pattern.

`config/config.go` `setDefaults` intentionally does NOT default `RetryCount==0` to anything else (benchmarks need to keep retries off), and does NOT auto-enable Security/Compatibility test sections (so users can disable them via YAML).

### Performance test flow (`internal/tester/performance.go`)

This is the core of the tool. Read this file first when changing benchmark behavior.

1. `loadTestCases` reads the test-case file as `[]json.RawMessage` — each element is a **complete request body** that gets passed through to the API verbatim. The tester does not mutate fields (model, temperature, etc. all come from the file).
2. `RunPerformanceTest` spawns `Concurrency` workers, each issuing `RequestsPerWorker` serial requests. Workers pick test cases via a global atomic cursor (`caseCursor`) so the load is evenly spread across cases regardless of worker count.
3. Per-worker results are collected into a worker-private slice — **no lock on the hot path**. Slices are merged and sorted by `StartTime` after `wg.Wait()`.
4. Each request is sent via `client.RetryCallRawWithStats`, which returns a `CallStats{TTFT, RequestID}`. TTFT is measured by the SSE parser at the first content-bearing frame (see below).
5. Metrics use **separate denominators per metric** to avoid skew:
    - `AvgLatency` ← successful requests only
    - `TTFT` ← only requests where the parser saw a first content frame (`ttftSamples`)
    - `TPOT` ← only requests with `outputTokens >= 2` (`tpotSamples`); formula: `(latency - ttft) / (outputTokens - 1)`
    - `AvgGenerationRate` ← only requests with `outputTokens > 0` (`genRateSamples`)
    - Throughput rates (RPS/RPM/TPS/TPM/Output TPS/TPM) use `actualTestDuration` (wall clock since `testStartTime`) as the denominator.
6. `logRequestLine` formats one line per request and writes it with a single `fmt.Fprintln` call — multiple `Printf`s would interleave under high concurrency. Preserve the single-write pattern.

### SSE / TTFT parsing (`internal/api/client.go` `parseChatCompletionSSE`)

Counts the first SSE frame containing **any** of `delta.content`, `reasoning_content`, `reasoning`, or `thinking` as the "first token" — without this, reasoning models (DeepSeek, Moonshot, Anthropic-style) would have TTFT systematically inflated by their reasoning phase. If you add support for another reasoning-style field, add it here too.

The parser also tracks whether the stream ended cleanly (`[DONE]` marker, finish reason, or usage block) so we can detect upstream-truncated streams.

### HTTP client (`internal/api/client.go` `NewClient`)

`maxConns` should be passed as the worker concurrency from `cmd/main.go` (it currently is). Going through the net/http defaults (2 idle conns/host) causes constant TLS reconnection at high concurrency and pollutes TTFT measurements. Keep `MaxIdleConnsPerHost`, `MaxConnsPerHost`, and `MaxIdleConns` aligned with this value.

### Reporting (`cmd/main.go` + `internal/reporter/reporter.go`)

The report combines:

- **Per-request rows** (`PERF-REQ-XXXX`) — one row per real HTTP request, with timestamps, in/out tokens, TTFT, output TPS. Sorted chronologically.
- **14 aggregate metric rows** (`PERF-001` … `PERF-014`) — one row per summary metric, appended after the per-request rows.

`buildPerformanceSummary` constructs `Summary` from `metrics` directly. Do **not** let the reporter auto-derive `TotalTests` from `len(allResults)` — the 14 aggregate rows would inflate the count and break the success-rate display. `MinTime`/`MaxTime` deliberately ignore failed requests (their `Duration` is often the timeout ceiling).

`AvgGenerationRate` is in tokens/second, not a duration — it goes into `Message`, with `Duration: 0`. Same pattern for any rate-style metric.

## Conventions worth keeping

- Comments and user-facing log strings are bilingual (Chinese + English); match the surrounding style of the file you're editing.
- Test reports (`performance-test-report-*`) and `config/config.local.yaml` are gitignored — never commit them.
- The `tools/` directory contains LLM-driven test-case generation scripts and a large corpus; `tools/tools/` and `tools/corpus/` are gitignored to stay under GitHub's 100MB limit.
- `internal/tester/{functional,stability,security,compatibility}.go` are scaffolding — don't rely on them being correct, and don't bring them online without explicit user direction.
