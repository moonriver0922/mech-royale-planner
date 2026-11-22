# Observed Projectile Defense Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Track completed checks in the execution ledger.

**Goal:** Test whether reacting to actual predicted projectile collisions improves the independent S02 opening survival and placement.

**Architecture:** Preserve the existing action planner and add a gated movement-only layer. Its short kinematic simulation predicts finite-body collision with visible straight projectiles; no observed danger means the original action is returned unchanged.

**Tech Stack:** Go 1.26.1, standard library, existing own arena geometry.

**Spec:** `docs/S07.md`

## Global Constraints

- Base exact scored S02 `0e6fe8893373923952fa67fd5c0a450246ac10f2`; do not read other bot implementations.
- Change only Move/Boost after the original action is fully computed. Airborne or no-risk actions are unchanged.
- Prediction horizon at most 0.6 seconds, original plus at most 11 alternatives.
- Use acceleration, body dimensions, map occlusion, charge slowdown, energy and shrinking-zone constraints.
- No new official request or final-entry change by the implementer. Do not troubleshoot Docker.

## Review Focus

- Fast projectiles must use swept collision, including finite vertical extent.
- Walls can invalidate both incoming bullets and alternate escape paths.
- Current velocity and charge slowdown can prevent an apparently sufficient sideways command.
- Outside-zone Boost and short-horizon safety must not be weakened.
- No-threat observations must retain all original output fields and internal action ownership.

### Task 1: Implement the gated movement layer

**Files:** create `internal/decision/projectile_defense.go` and `projectile_defense_test.go`; modify the end of `internal/decision/controller.go` before `rememberLaser`/`moving` bookkeeping.

**Interfaces:** `func (c *Controller) defendProjectiles(o wire.Observation, original wire.Action) wire.Action`. Consume existing `wire` types, stat helpers and map methods; return an action with only Move/Boost possibly changed. Internal helpers may be named by the implementer.

- [ ] Write geometry and integration tests named for the conditions in S07 Validation step 1. Record their initial failure before implementing.
- [ ] Implement `defendProjectiles` and integrate it once, keeping no-threat behavior unchanged.
- [ ] Run Go tests using the supplied pinned Go executable; fix actual failures without broad unrelated changes.
- [ ] Build a stripped Linux/amd64 candidate, run the native official checker, record identity and timing.
- [ ] Self-review against every scope item; commit code, tests and design. Report exact SHA and any modeling limit.

### Task 2: Review and measured selection (root owner)

- [ ] Review the complete diff against S02 with an independent reviewer.
- [ ] Resolve correctness findings, then freeze a source and binary identity.
- [ ] Run fixed-observation comparisons and the predeclared development pairs. Stop on a mechanism failure or clear placement regression.
- [ ] If justified, run the frozen candidate on the new holdout. Publish exact source only after checks; report official evaluation status honestly.
