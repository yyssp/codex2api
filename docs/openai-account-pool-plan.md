# OpenAI Account Pool Optimization Plan

## Goal

This repository should evolve toward a focused OpenAI account-pool runtime rather than a SaaS-style multi-tenant platform.

The optimization target is:

- lower request-path failure rate
- stronger duplicate-account protection
- clearer runtime/account state
- selective borrowing from `sub2api` and `cliproxyapi` without importing their full complexity

## Product Direction

### Why `codex2api` remains the mainline

- The runtime layering is cleaner than `sub2api`.
- The scheduler, retry path, and admin plane are already account-pool oriented.
- Validation signal is better than `cliproxyapi`.
- Complexity is still controllable.

### What to borrow selectively

From `sub2api`:

- stronger 401 recovery flow
- layered sticky routing based on `previous_response_id` and `session_hash`

From `cliproxyapi`:

- separate account-level state from model-level state, so one bad model does not poison the whole account

### What not to borrow

- do not import `sub2api` snapshot/outbox/websocket/platform complexity
- do not copy `cliproxyapi` refresh-loop behavior as-is

## Priority Roadmap

### P0: Must fix first

1. Scheduler slot acquisition race
   - Area: `auth/store.go`
   - Problem: non-fast scheduler selected an account using a stale load snapshot, then incremented `ActiveRequests` directly.
   - Risk: oversubscription under contention, bursty failure concentration on one account.
   - Acceptance:
     - selection uses atomic CAS-based acquisition
     - selection retries with another candidate if the first candidate becomes full

2. Expired AT-only accounts still look schedulable
   - Area: `auth/store.go`, `auth/fast_scheduler.go`
   - Problem: AT-only accounts can remain in rotation until upstream returns 401.
   - Risk: avoidable request failures exposed to callers.
   - Acceptance:
     - AT-only accounts whose `ExpiresAt` is in the past are excluded from scheduling
     - RT-backed accounts keep current behavior

3. Missing database-level credential uniqueness
   - Area: `database/sqlite.go`, `database/postgres.go`
   - Problem: duplicate prevention currently relies too much on application-side scans.
   - Risk: concurrent imports or multiple write paths can create duplicate RT/AT records.
   - Acceptance:
     - database creates partial unique indexes for `refresh_token` and `access_token`
     - startup does not hard fail when historical duplicates already exist; log and skip the index instead

### P1: High-value next

4. Rework 401 recovery path
   - Area: `proxy/handler.go`, `auth/store.go`, `cache/`
   - Borrow from `sub2api`, but keep it lightweight.
   - Target flow:
     - invalidate token cache
     - force access token expiry
     - prefer refresh-first or short isolation before long cooldown

5. Remove advanced feature drift
   - Area: `main.go`, `auth/refresh_scheduler*.go`, `auth/proxy_pool*.go`
   - Decision needed:
     - either wire refresh scheduler and enhanced proxy pool into runtime
     - or explicitly disable/remove them
   - Current status is the worst of both worlds: extra code surface without hot-path benefit

6. Session affinity lifecycle cleanup
   - Area: `auth/store.go`
   - Add explicit TTL pruning or cache-backed storage to avoid lazy-only cleanup

7. Single-source model mapping
   - Area: backend model mapping and frontend settings/usage pages
   - Remove duplicated Anthropic/OpenAI mapping tables

### P2: Medium-term refinement

8. Per-account per-model runtime state
   - Borrow the idea, not the whole implementation, from `cliproxyapi`
   - Example:
     - a model-specific 404 should not fully punish the account
     - a model-specific 429 should cool down only that model path where possible

9. Frontend smoke coverage
   - Minimum target:
     - account list load
     - add/import action happy path
     - settings page payload contract

10. Large-file decomposition
   - Split `auth/store.go`, `admin/handler.go`, `proxy/handler.go`, `database/postgres.go`
   - Goal is maintainability, not cosmetic restructuring

## Current Implementation Status

This fork aligns the first batch with P0:

- implemented: CAS-based non-fast scheduler acquisition retry
- implemented: expired AT-only accounts excluded from scheduling
- implemented: database partial unique index creation for RT/AT with duplicate-safe startup behavior
- implemented: 401 recovery flow now invalidates cached/local AT first, then attempts async RT refresh before treating the account as dead
- implemented: session affinity now has active background GC and account-removal cleanup
- implemented: refresh scheduler startup is now wired from environment flags rather than staying disconnected from runtime
- implemented: enhanced proxy pool is now wired into the real request path, including request-time proxy acquisition plus success/failure feedback
- implemented: frontend/backend model catalog and default Anthropic mapping are now backend-owned, with the admin frontend consuming a single source of truth
- implemented: first version of per-account per-model runtime state, so model-specific 403/404/429 no longer poison the whole account by default

## Recommended Next Change Set

If continuing immediately after this batch, the next patch should be:

1. validate whether model-scoped 429 heuristics should be narrowed further with more upstream body/code samples
2. expose model-level runtime state in the admin plane for observability and manual reset
3. decide whether the legacy simple proxy slice path should remain as fallback or be retired entirely
4. split the large hotspot files to slow future complexity drift

## Non-goals

- no SaaS tenant model
- no broad provider-agnostic abstraction rewrite
- no migration to `sub2api` architecture
- no direct copy of `cliproxyapi` selector or refresh conductor
