---
id: 0006-arch-go-fitness-tests
slug: /adr/0006-arch-go-fitness-tests
title: 0006. arch-go fitness tests to enforce the dependency rule
sidebar_label: 0006. arch-go fitness tests
description: ADR 0006 — make the hexagonal dependency rule executable and blocking in CI, rather than a convention.
---

# 0006. arch-go fitness tests to enforce the dependency rule

## Status

Accepted. Delivered as `Task 14` (commit *"Task 14: add architecture fitness
tests with arch-go, wire arch-test CI job"*), specified in
`ARCH_TEST_TASK.md`.

## Context

[ADR 0001](./0001-hexagonal-ports-and-adapters.md) established the dependency
rule — domain depends on nothing, application depends on domain, adapters
depend on application/domain — and `CLAUDE.md` marks it **NON-NEGOTIABLE**.

Nothing enforced it. The Go compiler is perfectly happy if a use case imports
`pgx`, or if the HTTP adapter reaches into the Postgres adapter to "just grab
the pool." Neither `go vet` nor `golangci-lint` has an opinion about layering.

Architectural rules that live only in a document erode predictably: not through
a decision to abandon them, but through a series of small, individually
defensible shortcuts taken under deadline pressure, each reviewed by someone
who did not have the rule in mind that day. By the time the erosion is visible,
unwinding it is a refactor rather than a code review comment.

The Java ecosystem solved this with **ArchUnit**: express architectural rules
as ordinary unit tests, so a violation is a red build rather than a review
opinion. Go's equivalent is [`arch-go`](https://github.com/arch-go/arch-go),
which can be driven either from a YAML config or as a library from a normal Go
test.

Constraints:

- **Strictly additive.** No production code was to be touched. If a real
  violation existed, it was to be reported, not silently worked around.
- **It must run in CI as a blocking gate**, not as an advisory report.

## Decision

**We will encode the hexagonal dependency rule as executable fitness tests in
`internal/architecture/architecture_test.go`, using `arch-go` v1.7.0 as a
library, and run them as a blocking `arch-test` CI job.**

The rules encoded, one `t.Run` subtest each:

| Rule | Meaning |
| --- | --- |
| `domain depends on nothing internal except domain` | `**.internal.domain.**` may not import any other `internal/` package |
| `application depends only on domain` | `**.internal.application.**` may not import `internal/adapters/**` |
| `inbound adapters do not depend on outbound adapters` | `**.internal.adapters.inbound.**` ↛ `**.internal.adapters.outbound.**` |
| `outbound adapters do not depend on inbound adapters` | the reverse direction |
| `only cmd is the composition root wiring every layer` | nothing under `**.internal.**` may import `cmd/**` — otherwise `cmd` stops being a leaf |
| `ports package only contains interfaces` | `internal/application/ports` may define no concrete struct or function, or an adapter type leaks into the application layer |

That last rule is a *content* rule rather than a dependency rule, and it is
worth having: a helper struct quietly added to `ports` is how an
implementation detail crosses the boundary without any import looking wrong.

Two `arch-go` quirks are documented inline in the test file, because both cost
time to discover:

- **The package-pattern DSL uses `.` as the path-segment separator**, mirroring
  Java package notation rather than Go import paths. `**.internal.domain.**`
  matches any import path containing an `internal/domain` segment. `arch-go`'s
  own `arch-go.yml` uses the same convention.
- **`arch-go` loads packages with `Tests: false`**, so imports that appear only
  in `_test.go` files are not evaluated. Test files may therefore cross layers
  (an inbound handler test wiring in-memory outbound repos, which is exactly
  what `server_test.go` does) without registering as violations. That is the
  right behaviour — the rule is about production dependencies — but it must be
  understood, or the tests will be read as more thorough than they are.

## Consequences

### Easier

- **The rule is now a build failure, not an opinion.** A use case importing
  `pgx` turns CI red with a named rule, and a reviewer can point at the rule
  instead of arguing style.
- **The architecture is self-documenting and current.** The test file is a
  precise, executable statement of the intended structure that cannot drift
  from reality, because drift breaks the build.
- **Onboarding is cheaper.** A new contributor discovers the constraint the
  first time they cross a boundary, not in review.
- **It confirmed the codebase was already clean.** All subtests passed on the
  first run — zero pre-existing violations — which makes the gate a ratchet
  rather than a cleanup project.

### Harder

- **A dependency on `arch-go`'s package DSL.** Its glob semantics are
  permissive (`.` translates to a regex matching any character, so it also
  matches `/`), so rules are broader than they look and need care to write
  precisely.
- **Test-only imports are invisible to it.** Documented above; the gap is
  acceptable but real.
- **Another blocking CI job.** `arch-test` was added to `docker-publish`'s
  `needs` list, so a layering violation now also blocks the container
  publish — deliberate, but it is one more thing that can stop a release.
- **Rules must be maintained.** A new top-level package that does not match any
  existing pattern is silently unconstrained until someone adds a rule for it.
