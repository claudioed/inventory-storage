# Mutation Testing — internal/domain/...

Tool: [gremlins](https://github.com/go-gremlins/gremlins).

## Command

```sh
gremlins unleash ./internal/domain --workers 1 --timeout-coefficient 30
```

Notes on flags:
- `./internal/domain` (a directory path) is used rather than
  `./internal/domain/...` — gremlins' path argument does not accept the Go
  `...` wildcard; a directory path recurses through its subpackages on its
  own.
- `--workers 1 --timeout-coefficient 30`: the tool's defaults produced
  spurious `TIMED OUT` results on this machine when running with the default
  worker concurrency (mutants finished in ~46ms each, far too fast to be a
  real test timeout — an artifact of parallel workers contending over the
  build cache, not a real slow test). A single worker with a higher timeout
  coefficient eliminated every false timeout; both dry-run and full-run
  confirmed 41/41 mutants are genuinely coverable and completed cleanly in
  ~23s.

## Final results

```
Killed: 41, Lived: 0, Not covered: 0
Timed out: 0, Not viable: 0, Skipped: 0
Test efficacy: 100.00%
Mutator coverage: 100.00%
```

## Survived mutants triaged

One mutant survived the first run and was chased (not left as "acceptable" —
see below); the final run above reflects the fix.

- **`CONDITIONALS_BOUNDARY` at `internal/domain/shared/quantity.go:36`**
  (`if result < 0` mutated to `if result <= 0`). This revealed a genuine gap:
  no test in the `shared` package itself exercised `Quantity.Sub` with an
  exactly-zero result (e.g. a full release: `5 - 5 = 0`, which must succeed,
  not error). Cross-package tests in `domain/stock`
  (`TestStockUnit_ReleaseReservation_ReturnsToUsable`) happen to exercise
  this same boundary and would have caught a real regression, but gremlins
  (run without `-i`/`--integration`) only runs each mutated package's own
  test suite, so that cross-package coverage doesn't count for `shared`.
  Fixed by adding `TestQuantity_Sub_ResultExactlyZero_Succeeds` directly in
  `internal/domain/shared/quantity_test.go`. No source change was needed —
  this was a test-coverage gap, not a code defect.

No mutants were left unaddressed as "equivalent" or "not worth chasing" —
every mutant in this run now has a killing test.
