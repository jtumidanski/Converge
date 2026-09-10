# Audit — Task 11: Session model, errors, on-disk store, recovery, sweep

Diff reviewed: `b05f695..e799b94` (commit `e799b94`), 11 new files under `apps/backend/internal/session/`, reviewed in full in one pass (1186-line diff, no truncation).

## Job A — Spec compliance

✅ Spec compliant, with two ⚠️ items that can't be verified from this diff alone, and one behavioral note.

Verified against the brief's Produces list, symbol-by-symbol:

- `Code` + 11 constants — `apps/backend/internal/session/codes.go:1-19` (controller pre-verified strings; not re-checked here).
- `Status` + 6 constants, `Strategy` + 3 constants, 5 stage constants — `codes.go:21-49`.
- `Diagnostics{WorkspacePath, Branch, Strategy, SourceSHA}` with `omitempty` tags — `errors.go:10-15`. Matches.
- `ReviewError{Code, Message, Change, Commit, ConflictingFiles, AppliedChanges, PossibleDependency, Diagnostics}`, tags exactly as specified, `omitempty` on all but `code`/`message`, implements `error` via `func (e *ReviewError) Error() string` — `errors.go:18-32`. Matches.
- `ResolvedChangeParams{Number, Title, Author, WebURL, MergedAt, Strategy, LandingSHAs, SourceSHA}` — `model.go:14-23`. Matches field-for-field.
- `NewResolvedChange(ResolvedChangeParams) (ResolvedChange, error)` — `model.go:38`. Accessors `Number() Title() Author() WebURL() MergedAt() Strategy() SourceSHA() LandingSHAs()` — `model.go:75-83`. All eight present.
- `Session` immutable, 16 accessors (`ID ProviderID Repository BaseBranch BaseSHA HeadSHA Status Stage CreatedAt UpdatedAt ExpiresAt Error RequestedChanges ResolvedChanges Files Totals`) — `model.go:106-131`. All present, matches brief's "~17" count (16 accessors + `IsExpired`/`IsActive`/`EarliestChange`).
- `IsExpired(now) bool` — `model.go:134`; `IsActive() bool` — `model.go:137`; `EarliestChange() (ResolvedChange, bool)` — `model.go:139-145`. All present.
- Eight transitions `WithStage WithResolved WithBase Ready Conflicted Failed Finished Expired` — `model.go:149-208`, all with the exact signatures specified (return types, `(Session, error)` vs `Session`).
- `NewBuilder()` + `SetID SetProviderID SetRepository SetBaseBranch SetRequestedChanges SetCreatedAt SetTTL` + `Build() (Session, error)` — `builder.go:1-58` (offsets: setters start line 21, `Build` at line 29).
- `NewID() (string, error)` — `id.go:9-15`.
- `SchemaVersion = 1`, `Record`, `ToRecord(Session) Record`, `FromRecord(Record) (Session, error)` — `record.go:6, 27-44, 62, 98`.
- `Cleaner{Cleanup(ctx, Session) error; RemoveDir(ctx, id string) error}` — `store.go:21-24`.
- `NewStore(root, ttl, cleaner, log *slog.Logger, now func() time.Time) *Store` — `store.go:38`. Ten produced members total (`NewStore` + 9 methods): `Root Dir Save Get List Finish LoadAll Sweep RunSweeper` — `store.go:45,46,58,88,96,110,157` and `sweep.go:12,29`. All present with matching signatures.
- `ErrNotFound = errors.New("session: not found")` — `errors.go:5`. Declared correctly but **never referenced** anywhere in this package (`Get` returns a bare `bool`, `Finish` treats unknown IDs as a no-op rather than returning `ErrNotFound`). Not a spec violation — the brief only requires it be "produced" — but it is currently dead code from this package's own perspective; it's presumably for a later task's HTTP layer (Task 19) to consume when translating `Get`'s `bool` into a 404. Flagging so Task 19's reviewer knows this contract is unused/unverified so far. (⚠️ cannot fully verify intent from this diff alone.)

Not verifiable from this diff:
- ⚠️ Whether `Record`'s JSON tags exactly match "PRD §6 schema" — the PRD is not part of this diff; I can only confirm internal consistency (round-trip works, see Job B §1) and that `ResolvedChangeParams`/`ResolvedChangeRecord`/`ReviewError`/`Diagnostics` tags match the brief's literal text quoted above.
- ⚠️ Whether `diff.FileSummary`/`diff.Totals` field tags (`path/previousPath/status/additions/deletions/binary`, `files/additions/deletions`) are unchanged — the `diff` package is not part of this diff; the report claims they're untouched, which I cannot independently confirm from the diff alone.

Behavioral note (not a spec-compliance failure, since the brief's signature for `EarliestChange` doesn't mandate ordering, but worth flagging): the doc comment says `EarliestChange` returns "the first resolved change (**sorted by merge time**)" (`model.go:139`), but `WithResolved` performs no sort — it simply copies whatever slice the caller passes (`model.go:154-157`), and `EarliestChange` just returns `s.resolved[0]` (`model.go:143-144`). If a future caller (Task 12/13) appends resolved changes out of merge order, `EarliestChange` will silently return the wrong change. See Job B, Issue (Important).

No extra/undocumented public symbols beyond the brief's list were found; all unexported helpers (`touch`, `optional`, `deref`, `expire`, `recordFile`) are implementation detail, not part of the produced interface.

## Job B — Audit

### 1. Persistence fidelity

**Round-trip mechanism is structurally sound** — `ToRecord`/`FromRecord` cover every `Session` field including the embedded `diff.FileSummary`/`diff.Totals`, the full `ReviewError` (including `Diagnostics`), and `ResolvedChanges` with `LandingSHAs` (`record.go:62-97`, `98-138`).

**Test coverage is a spot-check, not an exhaustive round-trip, and misses the exact fields flagged as highest-risk by the task brief:**
- `record_test.go:36` (`TestRecordRoundTrip`) asserts only `ID, Status, BaseSHA, HeadSHA, len(ResolvedChanges), ResolvedChanges[0].Strategy(), Totals().Additions, len(Files), ExpiresAt`. It never asserts `ProviderID`, `Repository`, `BaseBranch`, `RequestedChanges`, `CreatedAt`, `UpdatedAt`, `Stage`, or (critically) any `ResolvedChange` field other than `Strategy` — `Number`, `Title`, `Author`, `WebURL`, `MergedAt`, `SourceSHA`, `LandingSHAs` are marshalled and unmarshalled but never compared post-round-trip.
- **`Diagnostics` is never round-tripped by any test in this diff.** Grepping the diff for `Diagnostics` finds only its type definition (`errors.go:10-15`) — no test in `model_test.go`, `record_test.go`, or `store_test.go` ever constructs a `ReviewError` with a non-nil `Diagnostics`, `AppliedChanges`, or `PossibleDependency`, let alone saves/reloads one. `TestLoadAllRecovery` (`store_test.go:108`) is the only test that produces a `ReviewError` via real code (the `CodeInterrupted` path in `LoadAll`, `store.go:192-197`) and it only asserts `.Error().Code`, not that the record round-trips losslessly through `Save`→disk→(simulated fresh process)→`Get`. This is exactly the failure mode Priority 1 warns about ("a field with a wrong or missing JSON tag is silently dropped on reload") for the one struct (`Diagnostics`) that is most likely to carry a mistagged or lossy field, and it ships with zero coverage.
- No test does a full-structure comparison (e.g. `reflect.DeepEqual` between the original `Session`/`Record` and the round-tripped one).

**Verdict: Important.** The wiring is correct by inspection, but the test suite would not catch a broken `Diagnostics` tag, a swapped `MergedAt`/`CreatedAt` field, or a lost `AppliedChanges`/`PossibleDependency` value — the exact class of defect this package exists to prevent.

### 2. Atomic writes

`Save` (`store.go:49-82`) is genuinely atomic:
- Temp file created via `os.CreateTemp(dir, recordFile+".*.tmp")` **in the same directory** as the final `session.json` (`store.go:58`) — same filesystem, so `rename(2)` is atomic.
- Cleanup on any failure path: `defer func() { _ = os.Remove(tmpName) }()` (`store.go:63`) runs regardless of outcome; after a successful rename the temp path no longer exists, so the deferred remove is a harmless no-op.
- Write → `Sync()` → `Close()` → `Rename()` in that order, with `Rename` genuinely last (`store.go:64-75`).
- `TestSaveGetListAtomic` (`store_test.go:47`) asserts no `.tmp` file is left behind after a successful save.

**Verdict: Pass.** No partial-write outcome is reachable; a reader always sees either the old complete file or the new complete file.

### 3. Error handling / innocuous zero values

`LoadAll` (`store.go:157-204`) treats "unreadable file", "invalid JSON", and "fails `FromRecord` validation" identically as "cannot be trusted" (`readErr` accumulates all three, `store.go:168-176`), and does **not** silently drop the case: it logs a `Warn` (`store.go:178`), then either removes the directory if older than TTL or leaves it untouched if younger, with the reasoning documented in the function's doc comment (`store.go:130-155`). This matches the brief's "decide-and-document is acceptable" bar — cite as Pass.

`Get` (`store.go:84-90`) only consults the in-memory index and explicitly documents that it does not distinguish "never existed" from "existed but removed/corrupt" (`store.go:84-87` doc comment). Practically: by the time a session is in the index it already passed `FromRecord` (via `Save` or `LoadAll`), so during steady-state operation this is fine. But for a directory that was corrupt-but-young at `LoadAll` time (left un-indexed, `store.go:178-190`), every subsequent `Get` for that ID returns `false` identically to an ID that never existed, for the remaining lifetime of the process — the one-time `Warn` log at startup is the *only* record that this session was known-corrupt rather than unknown. There is no re-check, no marker, nothing surfaced through `Get`/`List` distinguishing the two. Given this exact "missing vs. corrupt conflated" pattern is called out as the specific defect shape from three earlier tasks, this is worth flagging even though it is a narrower window (limited to sessions that are simultaneously corrupt and younger than TTL at boot) than the Task 7/8/10 defects.

**Verdict: Important** (narrower than Tasks 7/8/10's defects, but the same shape and explicitly named as the thing to check).

### 4. Concurrency

- `Store.index` is guarded by `sync.RWMutex` (`store.go:27`); every read/write path (`Save` 79-81, `Get` 88-89, `List` 96-97/104-107, `Finish` 118-119, `expire` 129-131, `LoadAll` 199-201) takes the lock.
- No pointer/slice into locked state escapes: `Get`/`List` return `Session` by value; `Session`'s own accessors (`RequestedChanges`, `ResolvedChanges`, `Files`, `Totals`, `Error`) all defensively copy on every call (`model.go:106-131`), so even though the `Session` struct copied out from the map still shares its slice-backing-arrays and `*ReviewError`/`*diff.Totals` pointers with the map's copy, there is no code path — inside or outside the package — that mutates those shared backing structures in place (`Conflicted`/`Failed`/`FromRecord` always assign a fresh `.clone()` rather than mutating an existing `*ReviewError`, `model.go:184-196`, `record.go:124`).
- **No test exercises concurrent access.** `store_test.go` never starts a goroutine against the `Store` — every test (`TestSaveGetListAtomic` 47, `TestFinishIsIdempotentAndCleans` 78, `TestLoadAllRecovery` 108, `TestSweepExpires` 150) calls `Save`/`Get`/`Finish`/`Sweep`/`LoadAll` sequentially in the calling goroutine. `-race` passing (per the implementer's report) proves nothing here, since no test actually races `Sweep`/`RunSweeper` against `Save`/`Get`/`List`/`Finish` the way the brief warns Task 5 got wrong. `RunSweeper` itself (`sweep.go:29-38`) has zero references in any `_test.go` file in this diff.

**Verdict: Important.** Locking discipline looks correct by inspection, but the brief explicitly calls out that a clean `-race` run proves nothing without a concurrency-driving test, and this package ships none — for the one component (`Store`) that will be shared between HTTP handlers and a sweeper goroutine per the task's own framing.

### 5. Immutability

- `RequestedChanges()`, `ResolvedChanges()`, `Files()` all return `append([]T(nil), s.x...)` copies — `model.go:108, 110-112, 113`.
- `Totals()` copies the pointed-to struct and returns a new pointer — `model.go:117-122`.
- `Error()` calls `s.err.clone()` — `model.go:106`, and `clone()` deep-copies `ConflictingFiles`, `AppliedChanges`, and `Diagnostics` — `errors.go:18-32`.
- Ingress: `WithResolved` copies the incoming slice rather than aliasing it (`model.go:154-157`); `Ready` copies `files` (`model.go:179`); `Conflicted`/`Failed` clone the incoming `*ReviewError` rather than storing the caller's pointer (`model.go:186, 194`).
- `TestBuilderAndAccessors` (`model_test.go:31-35`) and `TestTransitionsAreImmutable` (`model_test.go:90-93`) both directly assert mutation of a returned slice/struct does not leak back into the session — real tests, not just inspection.

**Verdict: Pass.**

### 6. Transition validity

**None of the eight transitions check the session's current `Status` before applying.** Concretely:
- `Ready` (`model.go:167-182`) only checks `s.baseSHA == ""`; it does not check `s.status`. Calling `.Ready(...)` on a `FINISHED` or `EXPIRED` session succeeds and silently resurrects it to `READY`.
- `Finished` (`model.go:198-202`), `Expired` (`model.go:204-208`), `Conflicted` (`model.go:184-189`), `Failed` (`model.go:191-196`) each unconditionally overwrite `s.status` with no guard on the prior value — e.g. calling `.Conflicted(...)` on an already-`FINISHED` session transitions it back to `CONFLICTED`; calling `.Finished(...)` twice is harmless (idempotent by accident, not by design), but calling `.Finished(...)` on an `EXPIRED` session "un-expires" it.
- `WithBase` (`model.go:159-165`) similarly allows rewriting `baseSHA` regardless of status.

The model **trusts the caller entirely** to drive transitions in valid order; it validates only structural preconditions (`baseSHA` presence, SHA syntax) rather than state-machine preconditions. Nothing in the brief mandates guards, and this may be an intentional "processor layer enforces order" design (consistent with "plain functions instead of lazy providers" from the deliberate-decisions list) — but that tradeoff isn't stated anywhere in the report's "Deviations" or "Concerns" sections, which claim "no other deviations" and "no other deviations. All exact strings ... and function signatures match the brief character-for-character" with no mention of this gap.

`IsActive`/`IsExpired` are internally consistent with the status set: `IsActive` excludes exactly `FINISHED`/`EXPIRED` (`model.go:137`), `IsExpired` is a pure time comparison independent of status (`model.go:134`) — no contradiction between the two, just no cross-check between "is this transition legal from this status."

**Verdict: Important, plan-adherence caveat.** Not a violation of the brief's literal signatures, but a real correctness gap in the domain's core responsibility (guarding state), undisclosed by the implementer, and exactly the kind of thing Task 12/13/19/20 (all consumers of these transitions) need to know they cannot rely on.

### 7. Security

- **Token leakage**: nothing in this diff constructs a `ReviewError.Message`, `Diagnostics.*`, or any other field from live data — every value in the tests is a literal string (`"boom"`, `"m"`, `"The review was interrupted..."`). This task itself introduces no leak. But note: `ReviewError.Message` and `Diagnostics.SourceSHA`/`Branch`/`WorkspacePath` are free-form strings with no sanitization or redaction logic anywhere in this package — nothing here would strip a credential embedded in a git/provider error string before it reaches `session.json`. This is a ⚠️ forward-looking note for whichever later task (git/provider error path, likely Task 12+) actually populates these fields; this task provides no safety net.
- **G304 suppression** (`store.go:168`, `os.ReadFile(filepath.Join(s.root, id, recordFile))` at `store.go:169`): `id` comes from `os.ReadDir(s.root)` (`store.go:161`, `e.Name()`), i.e. it enumerates literal child-directory names of `s.root`. Directory entries returned by `ReadDir` never contain `/` and never include `.`/`..` (those are implicit, not listed), so `filepath.Join(s.root, id, recordFile)` cannot escape `s.root` regardless of `id`'s content — the suppression's safety argument holds structurally.
  - One nuance versus the brief's expected shape: the brief frames the constraint as "built from a validated session id (`^[0-9a-f]{8}$` via `workspace.ValidateSessionID`)". In this code, `workspace.ValidateSessionID(id)` is called only *after* the read, inside the error-handling branch (`store.go:178`), to decide whether an unreadable directory is "ours" to log/clean — it is **not** a precondition gating the `os.ReadFile` call itself. The actual safety property comes from filesystem semantics (bare filenames can't contain path separators), not from pre-validating the regex as the brief's framing implies. Functionally safe, but the mechanism differs from what's described — worth a note for whoever reviews `.golangci.yml`/gosec baselines later.

**Verdict: Pass** on the G304 constraint (traversal is not reachable); ⚠️ note on future Message/Diagnostics sanitization, out of this task's scope but worth flagging forward.

### 8. Test depth

8 test functions for a ~17-accessor model, 8 transitions, a builder, an error type, ID generation, a record DTO, a 10-member atomic store, recovery, and a sweep loop.

Genuine coverage:
- `TestBuilderAndAccessors` includes a real table (`bad := []struct{...}`, `model_test.go:39-55`) for six distinct invalid-builder-input cases — table-driven, adequate for that slice.
- `TestTransitionsAreImmutable` is a single dense sequential test exercising `WithStage`, `WithResolved`, `EarliestChange`, `WithBase` (valid + invalid), `Ready` (valid + invalid), `Conflicted`, `Failed`, `Finished`, `Expired`, plus two `NewResolvedChange` validation cases — not table-driven, but touches most of the transition surface in one function, which is close to the brief's "table-driven tests covering many cases in few functions would be adequate" bar.

Named, concretely untested surfaces:
- **`RunSweeper` (`sweep.go:29`) has zero test coverage** — not referenced in any `_test.go` file in this diff. It's a named member of the brief's produced interface and is the actual production loop the sweeper goroutine (Task 20) will run.
- **No transition-precondition test exists** for the gap found in §6 (no state-machine guard) — not even a test documenting the current (unsafe) behavior is present.
- **No concurrent-access test** for `Store` (see §4) — `Sweep`/`Finish`/`Save`/`Get`/`List` racing against each other is entirely unexercised.
- **`Diagnostics`, `ReviewError.AppliedChanges`, `ReviewError.PossibleDependency`** are never constructed or asserted by any test (see §1).
- **`FromRecord`'s own validation error paths are untested**: no test sets `Record.SchemaVersion` to anything other than `1` and checks the `record.go:99` error path fires; the builder-validation-failure branch inside `FromRecord` (`record.go:...`, wraps `Build()`'s error) is likewise never triggered by a test.
- Individual `ResolvedChange` field content (`Number, Title, Author, WebURL, MergedAt, SourceSHA`) is never asserted post-round-trip (see §1).
- `Save`'s failure paths (`MkdirAll`/marshal/`CreateTemp`/`Write`/`Sync`/`Close`/`Rename` errors) are not exercised by any test — lower priority since these require fault injection, but worth naming.

**Verdict: Important.** Not "8 functions each testing one happy path" (the two model tests are genuinely dense), but several whole surfaces named in the brief's own produced interface (`RunSweeper`) or flagged as highest-risk by the brief itself (`Diagnostics` persistence, concurrent `Store` access) ship with literally zero test coverage.

## Strengths

- Every symbol and signature in the brief's Produces list is present and correctly typed; nothing renamed, nothing missing from the interface later tasks depend on.
- `Save`'s atomic-write implementation is textbook-correct: same-directory temp file, `Sync` before `Close` before `Rename`, cleanup on every failure path, verified by a real test that checks no `.tmp` survives.
- Defensive copying is consistently applied on both ingress and egress for every slice/pointer field, and is verified by tests that actually attempt the mutation rather than just inspecting the code.
- `LoadAll`'s three-way classification (valid / corrupt-and-old / corrupt-and-young / not-ours) is well-reasoned and documented in-line, and does not silently drop the corrupt case — it logs, which is the bar the brief sets.
- The G304 suppression is narrowly scoped, commented with rationale, and the rationale is actually correct (even if not via the exact mechanism the brief's framing suggested).
- The implementer's report is detailed and mostly accurate about what was done — the three lint-driven deviations are correctly and honestly described.

## Issues

#### Critical (Must Fix)

None.

#### Important (Should Fix)

- No transition guards against invalid state-machine order (`Ready`/`Conflicted`/`Failed`/`Finished`/`Expired`/`WithBase` all apply unconditionally regardless of current `Status`) — `apps/backend/internal/session/model.go:167-208`. Undisclosed in the implementer's report, which claims no deviations from spec beyond three named lint fixes.
- `Diagnostics`, `ReviewError.AppliedChanges`, `ReviewError.PossibleDependency` are never constructed, saved, or round-tripped by any test — `apps/backend/internal/session/record_test.go` (whole file, no `Diagnostics` reference); this is exactly the field the brief names as highest persistence risk.
- `RunSweeper` has zero test coverage — `apps/backend/internal/session/sweep.go:29`.
- No test drives concurrent access to `Store` (`Sweep`/`Finish`/`Save`/`Get`/`List` racing) — `apps/backend/internal/session/store_test.go` (whole file, sequential only). A clean `-race` run does not cover this.
- `Get` cannot distinguish "session ID never existed" from "session directory existed but was corrupt/unreadable at the last `LoadAll`" for the remainder of the process lifetime — `apps/backend/internal/session/store.go:84-90`.
- `EarliestChange`'s doc comment promises "sorted by merge time" but neither it nor `WithResolved` performs any sort — `apps/backend/internal/session/model.go:139-157`.

#### Minor (Nice to Have)

- `record_test.go`'s round-trip assertion only spot-checks a subset of fields (misses `ProviderID`, `Repository`, `BaseBranch`, `RequestedChanges`, `CreatedAt`, `UpdatedAt`, `Stage`, and per-field `ResolvedChange` content beyond `Strategy`) rather than a full structural comparison — `apps/backend/internal/session/record_test.go:36`.
- `Record.Files` has `omitempty` while sibling slice fields (`RequestedChanges`, `ResolvedChanges`) do not — `apps/backend/internal/session/record.go:33` vs `24, 26` — inconsistent, harmless.
- `ErrNotFound` is declared but never referenced anywhere in this package — `apps/backend/internal/session/errors.go:5` — presumably for a later task's consumption; flag for Task 19's reviewer to confirm it's actually wired up.
- `FromRecord`'s `SchemaVersion` mismatch and internal `Build()`-failure error paths are untested — `apps/backend/internal/session/record.go:98-99`.
- No sanitization exists for `ReviewError.Message`/`Diagnostics.*` against credential leakage — not a defect in this diff (nothing here populates them with live data), but no safety net exists for the task that will.

## Assessment

**Task quality:** Needs fixes

**Reasoning:** The produced interface is spec-complete and the atomic-write/immutability mechanics are solid and well-tested, but the domain's core state-machine responsibility (guarding transitions) is unguarded and undisclosed, and test coverage has real gaps in exactly the areas the brief calls highest-risk — `Diagnostics` persistence and concurrent `Store` access — plus a fully untested `RunSweeper`. None of these are build/test-breaking, but they are exactly the class of silent, later-surfacing defect this review process exists to catch before three more tasks build on top of this package.

---

## Fix round 1 re-review (diff `e799b94..407f6ff`)

Build: PASS (`go build ./...`, `go vet ./...` clean). Full `internal/session` suite: PASS on a single `-race -count=1` run. Repeated `-race` runs of the two new concurrency tests surfaced a genuine, reproducible flake — see Finding 4.

### Finding 1 — Terminal-state protection: ADDRESSED

- `isTerminal()` added, `apps/backend/internal/session/model.go:162-164`, checks exactly `StatusFinished || StatusExpired` — no broader state machine.
- Six no-error transitions each start with `if s.isTerminal() { return s }` and return the **unchanged receiver**: `WithStage` (`model.go:174-176`), `WithResolved` (`model.go:184-186`), `Conflicted` (`model.go:231-233`), `Failed` (`model.go:243-245`), `Finished` (`model.go:257-259`), `Expired` (`model.go:268-270`).
- The two error-returning transitions reject with an error on a terminal receiver: `WithBase` (`model.go:195-197`, `fmt.Errorf("session: cannot set base on a terminal session...")`), `Ready` (`model.go:210-212`, `fmt.Errorf("session: cannot become ready from a terminal session...")`).
- No over-reach confirmed: `Ready` still only guards on `s.baseSHA == ""` (`model.go:213-215`), not on prior status; no other status-based guard was added anywhere in the diff. `Conflicted`/`Failed`/`Finished` still unconditionally overwrite `status` for any *non-terminal* receiver, exactly as before the fix.
- Control test `TestNonTerminalTransitionsStillWork` (`model_test.go:260-297`) drives all eight transitions through ordinary non-terminal states and asserts pre-fix behavior is preserved (`WithStage`→stage set, `WithBase`→SHA set, `Ready`→status READY, `Conflicted`/`Failed`→status set, `Finished`/`Expired`→status set). This would fail if the guard had been made status-order-sensitive instead of terminal-only.
- `TestTerminalTransitionsAreNoOps` (`model_test.go:208-255`) asserts, per transition, that `Status()` and `UpdatedAt()` are byte-identical to the pre-call receiver (not just "still terminal") — this would catch a regression that changed the terminal status itself (e.g. FINISHED silently becoming EXPIRED), not just a resurrection.

### Finding 2 — `EarliestChange` sort: ADDRESSED

- Real linear-scan sort by `mergedAt`, ties broken by ascending `number` — `model.go:143-155`. No `sort.Slice` call anywhere in the diff; the method iterates `s.resolved[1:]` by value (`rc` is a copy — `ResolvedChange` has no pointer/slice-of-slices fields that would alias on copy per `model.go:25-34`) and only ever reassigns the local `earliest` variable — `s.resolved` itself, the receiver's own slice, is never written to. Confirmed non-mutating by inspection: no assignment target has `s.resolved[...]=` anywhere in the function.
- `WithResolved` (`model.go:183-189`) is unchanged in this regard — still just copies whatever order the caller supplied; sorting was deliberately kept out of ingress, matching the report's stated rationale.
- Test `TestEarliestChangeSortsByMergeTime` (`model_test.go:302-337`) supplies changes out of merge-time order and asserts the earliest-numbered/earliest-merged one is returned, plus a same-timestamp case asserting the `Number` tie-break — genuinely exercises both branches of the `||` condition on `model.go:149-150`; would fail if either branch were removed or the tie-break order flipped.

### Finding 3 — Full round-trip coverage + `FromRecord` SHA-validation bug: ADDRESSED

- `TestRecordRoundTripFullStructure` (`record_test.go:454-599`) populates every field named in the finding: `ReviewError.Diagnostics` (`record_test.go:500-505`), `ConflictingFiles`/`AppliedChanges`/`PossibleDependency` (`record_test.go:499`), resolved changes with `LandingSHAs` (two entries, one with and one without `SourceSHA` to hit `omitempty` — `record_test.go:460-476`), `Files` (added/renamed-with-`PreviousPath`/binary — `record_test.go:484-488`), `Totals`, and all three timestamps.
- The comparison is genuinely whole-structure: `reflect.DeepEqual(orig, roundTripped)` on the full `Record` value (`record_test.go:533-536`), not a longer spot-check list — a wrong/missing json tag on any field, including nested `Diagnostics`, would break `DeepEqual` here because the JSON round trip (marshal→unmarshal, `record_test.go:510-524`) happens before the comparison. The subsequent field-by-field assertions (`record_test.go:541-586`) are explicitly framed as belt-and-braces on top of, not instead of, the `DeepEqual`.
- `FromRecord` bug fix verified correct: `record.go:391-404` now calls `gitx.ValidateSHA` on non-empty `BaseSHA`/`HeadSHA` and **returns an error** (`fmt.Errorf("session %s: base sha: %w", ...)`) rather than zeroing — confirmed `gitx.ValidateSHA` (`internal/gitx/validate.go:23-28`) is a pure validator that returns an error on mismatch, never mutates or substitutes a zero value. `TestFromRecordErrorPaths/malformed_base_sha` (`record_test.go:638-645`) sets a literal `"not-a-sha"` and asserts `FromRecord` errors — this is a real regression guard, not vacuous (verified it fails pre-fix per the report, and independently: the old code path was a bare `deref()` assignment with no validation call, so this exact input previously passed silently).

### Finding 4 — Concurrency and `RunSweeper` coverage: NOT ADDRESSED

- `TestStoreConcurrentAccess` (`store_test.go:865-925`) is genuine, not theatre: 8 goroutines really run concurrently (`go func(worker int)`, `store_test.go:884`), each doing `Save`/`Get`/`List`/`Corrupted` on 200 distinct ids while a second goroutine calls `Sweep` in a tight 100-iteration loop with no synchronization gate between them (`store_test.go:908-913`) — the absence of a barrier is what forces real overlap rather than sequencing. Ran this test alone under `-race -count=20` three times (60 total runs) with zero failures — this test is solid.
- `TestRunSweeperSweepsOnIntervalAndStopsOnCancel` (`store_test.go:243-...`) is **empirically flaky**, contradicting the report's claim that it is "deterministic rather than timing-flaky." Reproduced a real failure at `go test -race -count=5 ./internal/session/...` (approx. 1 failure in ~150 runs of the pair of tests): 

  ```
  store_test.go:275: session not expired after sweep: {... status:CREATING ...} ok=true
  ```

  Root cause: `Store.expire` (`store.go:143-150`, pre-existing, not touched by this fix diff) calls `s.cleaner.Cleanup(ctx, sess)` **before** taking the lock and writing `s.index[sess.ID()] = sess.Expired(...)`. The new test's `signalingCleaner.Cleanup` (`store_test.go:980-985`) sends on the `swept` channel from *inside* `Cleanup`, i.e. before the index is actually updated to EXPIRED. The test then immediately does `st.Get("0123abcd")` and asserts `Status() == StatusExpired` (`store_test.go:962-964` / diff line 962-963) on the assumption that receiving the channel signal implies the store mutation has already happened — that assumption is false, and there is a real TOCTOU window between the two. This is precisely the "timing-flaky, and that's its own defect" failure mode the audit brief called out by name for this finding.
- Because the `RunSweeper` test — the one specifically required to prove the interval/cancellation behavior — is flaky, Finding 4 is not fully discharged. The concurrency-contention half is solid; the `RunSweeper` half needs the assertion moved past a synchronization point that actually waits for the index write (e.g. poll `Get` with a timeout, or have `signalingCleaner` signal *after* an injected post-write hook / just poll-wait on `Get` returning EXPIRED with a bounded retry loop) rather than trusting the `Cleanup` callback's timing.

### Finding 5 — `Corrupted(id)`: ADDRESSED

- `corrupt map[string]error` field added to `Store` (`store.go:34`), initialized in `NewStore` (`store.go:42`).
- Populated by `LoadAll` for every id that fails to load and passes `workspace.ValidateSessionID` (`store.go:199-206`) — i.e., for every id LoadAll treats as "ours but corrupt," not just the ones old enough to be removed by the cleaner (the write happens before the age check at `store.go:207-213`).
- Cleared on every successful `Save` (`store.go:80`, `delete(s.corrupt, sess.ID())`), including `LoadAll`'s own rewrite of a recovered CREATING→FAILED session (`store.go:216-220` calls `s.Save`, which clears it).
- Safe for concurrent use: `Corrupted` (`store.go:106-111`) and the `corrupt` map writes in `Save` (`store.go:78-81`) and `LoadAll` (`store.go:204-206`) all take the same `s.mu` (`RWMutex`) already guarding `index` — no separate/unsynchronized lock, confirmed by reading each access site.
- Usefully consultable: `Get` returning `false` plus `Corrupted(id)` returning `true` genuinely distinguishes "never existed" from "was corrupt," verified by `TestGetVsCorrupted` (`store_test.go:996-1045`), which checks both the negative case (never-seen id → `Corrupted` false) and the positive case, then confirms `Corrupted` clears after a healthy `Save` for the same id — this is a real behavioral test, not just existence-of-method.

### Contract check

- No exported signature changed: confirmed by re-reading `model.go`/`store.go`/`record.go` in full — the only new symbols are the unexported `isTerminal()` (`model.go:162`) and the exported additive `Corrupted(id string) bool` (`store.go:106`). Every other function signature (`WithStage`, `WithResolved`, `WithBase`, `Ready`, `Conflicted`, `Failed`, `Finished`, `Expired`, `Get`, `Save`, `List`, `LoadAll`) is textually unchanged from `e799b94`.
- No JSON tag changed: the diff hunk for `record.go` touches only the `FromRecord` function body and adds one import (`gitx`) — no `json:"..."` tag lines appear anywhere in the diff. `codes.go` (status/stage/strategy/code string constants) is not present in the diff's file list at all, i.e. untouched.
- `.golangci.yml`: unchanged (only pre-existing `G204` exclusion present; no `G304` module-wide exclusion added). `grep -rn "nolint" internal/session/` → 0 matches. The single `// #nosec G304` annotation at `store.go:189` is the pre-existing one, unaltered in scope or wording.
- Nothing newly persisted into `session.json` could carry a token: the fix diff adds no new `Record`/`ResolvedChangeRecord` fields; the only new persisted-adjacent state (`Store.corrupt`) is in-memory only (`map[string]error`, never marshalled).

### Test quality

- `TestTerminalTransitionsAreNoOps` / `TestNonTerminalTransitionsStillWork`: would fail on regression — asserts exact `Status()`/`UpdatedAt()` equality per transition, both for the terminal no-op case and the working case. Not vacuous.
- `TestEarliestChangeSortsByMergeTime`: would fail on regression — asserts a specific `Number()` after an out-of-order and a tied-timestamp case; both branches of the sort comparator are exercised.
- `TestRecordRoundTripFullStructure`: would fail on regression — `reflect.DeepEqual` over the full `Record`, plus targeted field assertions; not spot-check-only.
- `TestStoreConcurrentAccess`: would fail on a real regression in `Store`'s locking (under `-race`) or in `Sweep`'s TTL logic (`len(st.List()) != 0` after the final sweep). Genuine contention, verified by repeated runs.
- `TestRunSweeperSweepsOnIntervalAndStopsOnCancel`: **flaky, not vacuous** — when it fails, it fails for a real reason (see Finding 4), which is arguably worse than a vacuous assertion: it will produce confusing, hard-to-reproduce CI failures rather than silently passing regardless of behavior.
- `TestGetVsCorrupted`: would fail on regression — checks both the "not distinguishable via `Get` alone" property and the "distinguishable via `Corrupted`" property, plus the clear-on-heal path.
- No new vacuous/always-true assertions found in the fix diff.

### New breakage in the fix diff

None found at Critical/Important severity beyond the flakiness in Finding 4 (already called out there — categorized as Important given the brief's explicit "flaky test in CI is its own defect" instruction, not a build/test break).

### Deferred minors

- `Store.expire` (`store.go:143-150`, pre-existing) calls `Cleanup` before updating the index; this ordering is fine for production (the index update is what matters, `Cleanup` failure is only logged) but makes any test that treats "`Cleanup` was called" as a proxy for "index was updated" fragile — worth a comment or a structural fix (update index first, or have `RunSweeper`'s test poll `Get` instead of relying on the `Cleanup` signal) the next time this file is touched.

### Assessment

**Fix round verdict:** Findings still open

**Reasoning:** Findings 1, 2, 3, and 5 are correctly and completely addressed with real, non-vacuous regression tests. Finding 4 is only half-discharged: `TestStoreConcurrentAccess` is genuine concurrent-contention coverage, but `TestRunSweeperSweepsOnIntervalAndStopsOnCancel` is empirically flaky (reproduced directly, ~1 failure in ~150 runs) due to a TOCTOU race between the `signalingCleaner`'s channel signal and `Store.expire`'s later index write — exactly the "flaky test is its own defect" failure mode the original finding warned against.

---

## Fix round 2 re-review (`407f6ff..fbe29c5`)

### Finding verdicts

1. **Reorder correct and complete: ADDRESSED.**
   - `Finish` (`store.go:136-149`): computes `finished := sess.Finished(s.now())` (line 141), takes the lock, writes `s.index[id] = finished` (line 143), releases the lock (line 144), **then** calls `s.cleaner.Cleanup(ctx, sess)` (line 145) outside the lock. Status write precedes `Cleanup`; lock is not held across it.
   - `expire` (`store.go:156-163`): takes the lock, writes `s.index[sess.ID()] = sess.Expired(s.now())` (line 158), releases (line 159), **then** calls `s.cleaner.Cleanup(ctx, sess)` (line 160) outside the lock. Same shape.
   - No other method retains the old cleanup-before-status shape: `grep -n "func (s \*Store)" store.go` → `Root, Dir, Save, Get, Corrupted, List, Finish, expire, LoadAll`. Only `Finish` and `expire` call `cleaner.Cleanup`; `LoadAll`'s corrupt/stale-directory path calls `s.cleaner.RemoveDir` (store.go:223) only after already recording the corruption in `s.corrupt` under lock (store.go:217-219) — status-equivalent state precedes the filesystem call there too. `Save` (store.go:49-83) writes disk and index together at the end, not cleanup-adjacent.
   - `Finish` idempotency: the top-of-function guard (`store.go:137-139`, `sess.Status() == StatusFinished` → no-op) reads via `Get`, which reflects the index write from line 143 immediately, so a second call after the first's index write (even mid-`Cleanup`) is a true no-op and does not re-invoke `Cleanup`. Backed by `Session.Finished`/`Session.Expired` themselves being terminal-guarded no-ops (`model.go:162` `isTerminal()`, checked at `model.go:257` and `model.go:268`), so the transition can't un-terminal or double-fire regardless of caller. Idempotent.

2. **Race window closed: ADDRESSED.**
   - The index write (`store.go:143` / `store.go:158`) happens-before the `Cleanup` call in program order on the same goroutine, and the write is protected by `s.mu.Lock()`/`Unlock()` (store.go:142,144 / 157,159), which establishes a happens-before edge with any concurrent `Get`/`List` taking `s.mu.RLock()`. A concurrent reader therefore either observes the pre-transition session (before the writer's critical section) or the fully-terminal session (after it) — there is no interleaving in which a reader can see the session as active once `Cleanup` has begun, because the write is fully committed before `Cleanup` is even called.
   - Durability caveat (see orphan analysis below): the write is durable **in the in-memory index only**. Neither `Finish` nor `expire` calls `s.Save` (confirmed: the only `s.Save` call in the file is `store.go:231`, inside `LoadAll`'s CREATING→FAILED recovery, unrelated to these two paths). So "durable" here means durable for the life of the current process's index, not durable to `session.json` on disk. The race the finding was about (a concurrent reader observing an active session mid-delete) is fully closed at the in-memory level, which is the only level `Get`/`List` ever read from (`store.go:92-97`, `114-125`) — so the specific race is closed. But this is the same gap that produces the orphan-retry answer below.

### Orphaned-workspace analysis

**Direct answer: yes, for a live process nothing retries a failed `Cleanup`, for either `Finish` or `expire`. Across a restart, `expire`-triggered failures are deterministically retried; `Finish`-triggered failures are not retried until the session's original TTL naturally elapses (which may be much later, or may already have a large window before it fires).**

Evidence:

- The only place `Cleanup`/`RemoveDir` are invoked is `Finish` (store.go:145), `expire` (store.go:160), and `LoadAll`'s corrupt-directory path (store.go:223, a different, unrelated failure mode). `Sweep` (sweep.go:10-24) is the only periodic driver, and its selection filter is `if sess.IsActive() && sess.IsExpired(now)` (sweep.go:15). `IsActive()` is `s.status != StatusFinished && s.status != StatusExpired` (model.go:136). Because the reorder writes the terminal status into the index **before** `Cleanup` runs, by the time `Cleanup` could fail, the session is already `IsActive() == false` in the index. Every subsequent `Sweep` tick (`RunSweeper`, sweep.go:27-38) will skip it forever, for the remaining life of the process. There is no other timer, retry queue, or backoff in this package. **Within a running process, a failed `Finish` or `expire` cleanup is never retried.**
- Neither `Finish` nor `expire` persists the terminal status to `session.json` — confirmed by the single `s.Save` call site (store.go:231) being unrelated to these two methods, and by `expire`'s own doc comment: "in memory only; the directory is gone" (store.go:151-152, present before and after this diff — this is not new). This means FR-8.7's "disk is the source of truth and is rewritten atomically ... on every state change" (prd.md:332-333) is violated by both `Finish` and `expire`, a **pre-existing** gap not introduced or touched by this fix diff.
- Consequence for `expire` on restart: `LoadAll` re-reads the still-unmodified on-disk record (store.go:203-210), indexes it with its original pre-expiry status (store.go:235-237), then unconditionally calls `s.Sweep(ctx)` (store.go:239). Since `expire` is only ever invoked once `sess.IsExpired(now)` was already true (sweep.go:15) and TTL expiry is monotonic, that same record is still expired at restart, so `Sweep` will re-select it and retry `Cleanup`. **`expire` failures are retried, but only via a full process restart — not automatically, not while the process stays up.**
- Consequence for `Finish` on restart: same mechanism, but `Finish` is normally invoked before TTL (a client action, not a TTL-driven one). On restart, `LoadAll` reloads the pre-`Finish` on-disk status (e.g. `READY`) and re-indexes the session as active (store.go:235-237) — the fact that a client already finished the review, and cleanup for it failed, is completely lost. The session now silently reappears as active in `Get`/`List` output, and its cleanup will only be retried once the *original* `CreatedAt + TTL` naturally elapses (whatever that window was set to) and `Sweep` picks it up as expired. **Until then there is no way to retry the cleanup at all** — not automatically, and not by the client either: re-invoking `Finish` after this restart just re-runs the same sequence and can fail again the same way, and re-invoking it *before* a restart is now a guaranteed no-op (finding 1's idempotency guard), so the one retry path that existed before this round (a client re-clicking "finish" while cleanup was still failing, since status stayed non-terminal) is now gone.
- `Manager.Cleanup` is confirmed idempotent (manager.go:119-125, `TestCreateCleanupRealGit` double-cleanup per round-1 audit) and its individual git steps are best-effort/continue-on-failure, but only `os.RemoveAll` of the session directory (manager.go:172-176) is a hard failure that would leave the directory on disk — that is exactly the step whose retry path is now gone within a live process.

**Severity: Important.** This is a genuine regression for the `Finish` path specifically (the pre-round-2 code allowed retry via re-invocation because status stayed non-terminal on cleanup failure; that path is now closed by the idempotency guard), and it is an unchanged pre-existing gap for the `expire` path (which never had in-process retry either, before or after this diff — its behavior on `Cleanup` failure was "log a warning and move on" in both versions). Not Critical: FR-9.1's continue-on-failure/log requirement is still met (store.go:146 returns the error; expire logs it at store.go:161), `Cleanup`'s destructive step is `os.RemoveAll` which is a rare failure mode (permissions, disk I/O), and the ruling's trade-off is directionally correct — a visibly-wrong active session serving deleted files is worse than an invisible disk leak. But "nothing retries in-process, and the disk-fill risk is silent" is real and should be tracked: a follow-up (e.g., have `Sweep` also re-attempt `Cleanup` for terminal-but-not-yet-cleaned sessions, or persist terminal status + a `cleanupFailed` flag via `Save` so `LoadAll`/`Sweep` can find and retry it without waiting for TTL) is warranted rather than accepted silently.

### Flakiness verification

Ran `go test -race -run TestRunSweeperSweepsOnIntervalAndStopsOnCancel -count=200 ./internal/session/...` myself (not the reported 1000): **`ok`, 0 failures, 2.633s.** Consistent with the implementer's reported 300-run and 1000-run clean results. Given the previously-reproduced ~1/150 failure rate, 200 consecutive clean `-race` runs is consistent with the race being closed (roughly 74% expected-fail probability if the old bug were still present at that rate, so a clean run is meaningful, if not exhaustive, confirmation).

### Contract check

- No exported signature changed: the diff (`store.go` only, 21 insertions / 8 deletions per `git diff --stat`) touches only the bodies and doc comments of `Finish` and `expire`; both keep their existing signatures (`func (s *Store) Finish(ctx context.Context, id string) error`, `func (s *Store) expire(ctx context.Context, sess Session)`).
- No JSON tag changed: diff file list is `store.go` only; `record.go`/`codes.go` (where JSON tags and status/stage/strategy/error-code strings live) are untouched.
- No module-wide gosec exclusion added: `.golangci.yml` gosec `excludes` block still contains only the pre-existing `G204` entry (subprocess launch), confirmed by direct read — no `.golangci.yml` changes appear in this diff (file list is `store.go` only).
- Nothing newly persisted into `session.json` could carry a token: `grep -n "token\|Token" internal/session/store.go` → no matches; the diff adds no new fields to any persisted `Record` type, only reorders existing in-memory writes.

### New breakage in the fix diff

None found. `go build ./...` clean; `go test -race -count=1 ./...` and the targeted `-count=200` re-run both pass; the reorder itself is self-contained to `Finish`/`expire` and does not touch any other method's control flow.

### Deferred minors

- The prior round's "Deferred minors" note about `expire` calling `Cleanup` before the index update is now resolved by this fix (that was the actual bug); no longer applicable.
- FR-8.7 ("disk is the source of truth ... rewritten atomically on every state change") is not honored by either `Finish` or `expire` — both are in-memory-index-only transitions. This is pre-existing (not introduced by this diff) but is the root cause of the restart-recovery gap described above. Worth a follow-up ticket rather than blocking this round, since fixing it properly (persisting terminal status via `Save` before `Cleanup`, then handling the `Cleanup`-failed case explicitly) is a larger change than this round's scope.
- Consider having `Sweep` also reconsider sessions that are terminal in the index but whose directory still exists on disk (i.e. a cleanup-failed retry list), rather than relying on either "the process never restarts" (no retry, ever) or "the process restarts after the original TTL has elapsed" (retry, but only for `expire`-originated failures, and only after a possibly long, unbounded wait for `Finish`-originated ones).

### Assessment

**Fix round verdict:** All findings addressed

**Reasoning:** The reorder is correct, complete, and closes the specific concurrent-read race (finding 4) with 200 clean `-race` runs corroborating the implementer's 1000-run claim; no contract or breakage issues found. The orphaned-workspace trade-off the ruling explicitly asked about is real — nothing retries a failed cleanup within a live process for either `Finish` or `expire`, and `Finish`'s previous implicit retry-by-recall is now gone — but it is rated Important, not blocking, since it's a bounded, logged, non-security operational gap that the ruling's own stated priority (don't serve deleted files as active) correctly outweighs; it should be tracked as a follow-up rather than reopening this round.

## Fix round 3 re-review (`fbe29c5..144d237`)

### Finding verdicts

1. **Persist-on-failure, success path untouched: ADDRESSED.**
   - `Finish` (`store.go:155-170`): on `Cleanup` failure calls `s.persistCleanupFailure(id, finished)` (`store.go:166`), where `finished` is the terminal `Session` computed at line 160 — the failure-only branch, inside `if err := s.cleaner.Cleanup(...); err != nil`.
   - `expire` (`store.go:181-190`): same shape, `s.persistCleanupFailure(sess.ID(), expired)` at `store.go:188`, also inside the `err != nil` branch.
   - Success path unmodified: no `Save` call anywhere in the success branch of either function; `Finish` returns `nil` immediately after a successful `Cleanup` (`store.go:169`), `expire` falls straight past the `if` block (`store.go:189-190`) with nothing added.
   - `persistCleanupFailure` (`store.go:199-208`) calls `s.Save(terminal)` where `terminal` is the already-terminal `Session` passed in by the caller — confirmed the record written carries `StatusFinished`/`StatusExpired`, not an intermediate status.
   - `Save` failure handling: `store.go:200-202` — `if err := s.Save(terminal); err != nil { s.log.Warn(...) }`, no `return`, no panic; the in-memory index write happened earlier under lock in `Finish`/`expire` (`store.go:162`, `store.go:184`) before `Cleanup` was even called, so a `Save` failure here cannot roll back or lose the in-memory terminal status — it was already durable in the index regardless.
   - Verified by test: `TestFinishCleanupFailurePersistsTerminalStatus` (`store_test.go:387-405`) and `TestExpireCleanupFailurePersistsTerminalStatus` (`store_test.go:409-427`) both read `session.json` off disk via `os.ReadFile`/`json.Unmarshal` after a forced `Cleanup` failure and assert `rec.Status == StatusFinished`/`StatusExpired`. Both pass (see Test quality below).

2. **Bounded retry, including map cleanup: ADDRESSED.**
   - Bound enforced in `retryCleanup` (`store.go:217-237`): increments `s.pendingCleanup[id]` (`store.go:221`), and once `attempts >= maxCleanupRetries` (`store.go:224`, constant = 5 at `store.go:49`), logs at `Error` level (`store.go:225`) and deletes the map entry (`store.go:226-228`) before returning — no further `Cleanup` call for that id from that state.
   - Cleanup on success too: `store.go:234-236` deletes the entry once `Cleanup` succeeds — no leaked entries on the happy retry path either.
   - Give-up is observable: `s.log.Error("giving up on session cleanup after repeated failures", ...)` (`store.go:225`) — not silent, and distinguishable from the `Warn`-level per-attempt log (`store.go:231`) by severity.
   - `TestSweepRetryCleanupIsBounded` (`store_test.go:490-527`) directly proves the bound is real, not just documented: it drives 20 `Sweep` calls against a `Cleanup` that always fails, then asserts `calls == wantCalls` where `wantCalls := 1 + maxCleanupRetries` (`store_test.go:508-512`) — this would fail immediately if the constant were removed or increased without the test being updated in lockstep — and separately asserts `pendingSize == 0` (`store_test.go:520-522`), which would fail if the give-up path stopped deleting the key. Ran this test directly; passes (see Test quality).

3. **No resurrection: ADDRESSED.**
   - `retryCleanup` (`store.go:217-237`) never writes to `s.index` and never calls any `Session` transition method (`Ready`/`Finished`/`Expired`/etc.) — it only calls `s.cleaner.Cleanup(ctx, sess)` and mutates `s.pendingCleanup`. The `sess` passed in is already terminal by construction: `Sweep` only adds an id to the `retry` slice if it is a key of `s.pendingCleanup` (`sweep.go:23-28`), and every path that adds a key to `pendingCleanup` (`persistCleanupFailure` at `store.go:203-206`, `LoadAll` at `store.go:311-320`) only does so for sessions whose index status is already terminal (`persistCleanupFailure` is only called from the `Cleanup`-failed branch of `Finish`/`expire`, both of which write terminal status to the index *before* calling `Cleanup`; `LoadAll` gates on `!sess.IsActive()` at `store.go:311`).
   - `TestSweepRetriesFailedCleanupUntilSuccess` (`store_test.go:432-484`) asserts `len(st.List()) != 0` fails both immediately after the failed `Finish` (`store_test.go:449-451`) and again after the retry succeeds (`store_test.go:475-477`) — i.e. explicitly checks the session never becomes visible as active across the retry. Passes.

4. **Concurrency of `pendingCleanup`: ADDRESSED.**
   - Every access to `s.pendingCleanup` is under `s.mu`: reads in `Sweep` (`sweep.go:16,24-27`, under `RLock`), writes in `persistCleanupFailure` (`store.go:203-206`, `Lock`), `retryCleanup` (`store.go:220-223`, `226-228`, `234-236`, `Lock`), and `LoadAll` (`store.go:309-321`, `Lock`, same critical section as the `s.index[id] = sess` write).
   - No map or pointer escapes the lock while `Cleanup` runs: `Sweep` builds `retry []Session` — a slice of `Session` value copies, not map entries or pointers into `pendingCleanup` — while holding `RLock` (`sweep.go:23-28`), releases the lock (`sweep.go:29`), and only then iterates the *slice* (not the map) calling `s.retryCleanup(ctx, sess)` (`sweep.go:34-36`). This mirrors the round-2-verified pattern for `due`/`expire` (`sweep.go:17-22`, `30-33`) exactly — same shape, same lock discipline. `retryCleanup` itself calls `s.cleaner.Cleanup(ctx, sess)` fully outside any lock (`store.go:219`, no `s.mu` held at that line), so `Cleanup` — the filesystem/git-work call that round 2 explicitly kept out from under the mutex — is still never invoked while holding it.
   - `go test -race -count=1 ./internal/session/...` (this checkout, ran myself) passed clean: `ok github.com/jtumidanski/converge/internal/session 1.163s` — the race detector saw no unsynchronized access to `pendingCleanup` across `Finish`/`expire`/`Sweep`/`LoadAll`/`retryCleanup`/`persistCleanupFailure` under the concurrency exercised by the existing test suite (including `TestRunSweeperSweepsOnIntervalAndStopsOnCancel`, which drives `Sweep` concurrently with reads).

5. **Restart seeding: ADDRESSED.**
   - `LoadAll` (`store.go:309-320`): after indexing the loaded record, `if !sess.IsActive() { if _, ok := s.pendingCleanup[id]; !ok { s.pendingCleanup[id] = 0 } }`. `IsActive()` (`model.go:136`) is `status != StatusFinished && status != StatusExpired` — so only FINISHED/EXPIRED records get seeded.
   - Correctness of the inference: `LoadAll` only visits directories `os.ReadDir(s.root)` actually returns (`store.go:266,271`). A session directory only survives to be found here if its prior `Cleanup` either was never invoked (impossible for a terminal record — `Session.Finished`/`Expired` are only ever produced by `Finish`/`expire`, both of which call `Cleanup` unconditionally right after the transition) or was invoked and failed (a successful `Cleanup` deletes the directory outright — confirmed by round-1 audit of `manager.go`'s `os.RemoveAll`, unchanged by this diff). So a terminal record found on disk after a restart necessarily means a prior `Cleanup` failed — the seeding cannot be wrong for a session that went through `Finish`/`expire` normally.
   - Cannot mistakenly seed a healthy (active) session: the `!sess.IsActive()` guard excludes every non-terminal status outright, and an active session's directory existing is the normal, expected case (not a signal of anything).
   - **Gap not claimed to be closed, and not newly introduced by this round:** a crash *during* `Cleanup` itself (after the in-memory terminal write but before `persistCleanupFailure` runs, i.e. before the process ever learns `Cleanup` failed) leaves the on-disk record at its pre-transition status (e.g. `READY`), not terminal — `LoadAll` would then correctly-per-its-own-logic treat it as active and not seed `pendingCleanup`. This is strictly narrower than the round-2 gap (crash window is now only "mid-`Cleanup`," not "any time after cleanup fails"), is outside what this round's ask (`persist on cleanup *failure*`) covers, and does not regress anything — noting for completeness only.
   - **No dedicated test exercises the `LoadAll` seeding path itself** (see Test quality) — the restart-seeding claim in the implementer report is verified here by code reading only, not by a test that writes a terminal `session.json`, calls `LoadAll` on a fresh `Store`, and asserts `pendingCleanup` was seeded and a subsequent `Sweep` retries it. This is a test-coverage gap, not a correctness gap — the logic is correct.

### Test quality

- Ran the full package suite and the two named tests myself:
  - `go test -race -count=1 ./internal/session/...` → `ok  github.com/jtumidanski/converge/internal/session  1.163s`.
- `TestSweepRetryCleanupIsBounded` (`store_test.go:490-527`): confirmed it asserts `calls == wantCalls` where `wantCalls = 1 + maxCleanupRetries = 6` (`store_test.go:508-512`) across 20 `Sweep` calls (`store_test.go:502-504`), and separately asserts `pendingSize == 0` (`store_test.go:517-522`). Both assertions are exact-equality, not upper bounds, so the test would fail if the retry bound regressed in either direction (too many or too few calls) or if the give-up path stopped clearing the map. This matches what the implementer report claims.
- `TestFinishCleanupFailurePersistsTerminalStatus` (`store_test.go:387-405`) and `TestExpireCleanupFailurePersistsTerminalStatus` (`store_test.go:409-427`): both read `session.json` directly off disk via `os.ReadFile` + `json.Unmarshal` into a `Record` (`store_test.go:394-399`, `416-421`) and assert the on-disk `Status` field, not `st.Get`/`st.index` — genuinely proves disk persistence, not just in-memory index state.
- **Gap:** no test drives the `LoadAll`-seeds-`pendingCleanup`-on-restart path end-to-end (write a terminal record with a surviving directory → fresh `Store` → `LoadAll` → assert `pendingCleanup` seeded and/or the id gets retried by the `Sweep` that `LoadAll` triggers internally at `store.go:323`). The four new tests all exercise the pending-cleanup lifecycle via `Finish` within a single `Store` instance, never via a simulated restart. This is the one part of the round-3 ask ("verify it identifies them correctly") that rests on code reading alone rather than a passing test — worth closing in a future round but not blocking, since the logic itself is straightforward and independently verifiable (see Finding 5).

### Flakiness verification

Ran myself: `go test -race -run TestRunSweeperSweepsOnIntervalAndStopsOnCancel -count=200 ./internal/session/...` → `ok  github.com/jtumidanski/converge/internal/session  2.636s`, 0 failures. Consistent with round 2's clean 200-run result; this round's changes to `sweep.go` (adding the `retry` slice/loop) don't touch the `due`/`expire` code path this test exercises, and the race detector found nothing across the combined suite either.

### Judgment call: is `maxCleanupRetries = 5` right?

Reasonable, not obviously wrong, but not free of a real trade-off either. With the confirmed default `CLEANUP_INTERVAL_MINUTES = 30` (`config.go:97`), 5 retries gives a failing cleanup ~2.5 hours (5 × 30 min, plus the initial attempt) to self-resolve before the id is dropped from `pendingCleanup` and no further automatic retry happens for the rest of that process's life (only a restart resets the counter, per Finding 5). For a transient cause (momentarily-locked file, brief NFS hiccup) 2.5 hours is generous. For a permanent cause (bad permissions, disk full, a bug in `Cleanup` itself) 2.5 hours of periodic retry work is cheap (six total attempts, not six-per-sweep), so the cost of "too many" retries is low. The bigger question is the *give-up* behavior: it degrades to a single `Error`-level log line (`store.go:225`) with no counter, metric, or other durable signal that an operator can alert on — the implementer's own report flags this as a deliberate scope cut ("outside this round's scope, no new exported API was requested"). Given this is infra that runs unattended, a log line alone is a weak backstop for "we now have a permanently orphaned directory on disk" — I'd rate this a legitimate follow-up (metric/alert on the give-up path), not a blocking gap for this round, since nothing in the round-3 ask requested observability beyond logging and FR-9.1's "log it and don't abort" bar is met.

### Contract check

- No exported signature changed: `git diff --stat` for this range shows only `store.go`, `store_test.go`, `sweep.go`; `Finish(ctx, id) error`, `Sweep(ctx)`, `RunSweeper(ctx, interval)`, `LoadAll(ctx) error` all keep their existing signatures — confirmed by reading each definition (`store.go:155`, `sweep.go:14`, `sweep.go:40`, `store.go:265`).
- No JSON tag changed: `record.go`/`codes.go` (owners of JSON tags and status/stage/strategy/error-code strings) do not appear in the file list for this diff at all.
- New retry state (`pendingCleanup map[string]int`, `maxCleanupRetries`) is unexported — lowercase identifiers, `store.go:42`, `store.go:49`.
- No module-wide gosec exclusion added: `.golangci.yml` gosec `excludes` still contains only the pre-existing `G204` entry (`.golangci.yml:23`), confirmed by direct read; the diff doesn't touch `.golangci.yml`.
- Nothing newly persisted into `session.json` could carry a token: `grep -n "token\|Token" apps/backend/internal/session/*.go` (excluding tests) → no matches; `persistCleanupFailure` persists the same `Session`/`Record` shape `Save` always has (`ToRecord`, unchanged by this diff).
- Round 1 and 2 fixes undisturbed: `Finish`/`expire` still write terminal status to the index before calling `Cleanup` (`store.go:161-164`, `183-186`) and still call `Cleanup` outside the lock (`store.go:164`, `186` are not inside any `s.mu` critical section); `Finish`'s terminal-status early-return guard is unchanged in shape (`store.go:156-158`); `TestRunSweeperSweepsOnIntervalAndStopsOnCancel` remains deterministic (200/200 clean, above).

### New breakage in the fix diff

None found. `go build ./...` clean (no output, exit 0). `go test -race -count=1 ./internal/session/...` clean. `go test -race -run TestRunSweeperSweepsOnIntervalAndStopsOnCancel -count=200` clean. The new code is additive (`persistCleanupFailure`, `retryCleanup`, the `pendingCleanup` field, and the `retry` loop in `Sweep`) and does not alter the control flow of any pre-existing exported method beyond adding the failure-branch calls identified in Finding 1.

### Deferred minors

- Commit message uses `fix(session): retry and persist status on failed session cleanup` where the plan's stated convention is `fix(task-001): ...`. Flagging for the record per the ask; not worth a fix round.
- Give-up on a permanently failing cleanup degrades to a single `Error`-level log line with no metric/alert/counter (see Judgment call above) — legitimate follow-up, not blocking.
- No test drives `LoadAll`'s `pendingCleanup` seeding path directly (see Test quality) — the logic is verified correct by reading, but untested end-to-end via a simulated restart. Worth adding in a future round.
- The narrower residual gap noted in Finding 5 (crash exactly during `Cleanup`, before `persistCleanupFailure` ever runs, still leaves a non-terminal on-disk record and thus no `pendingCleanup` seeding on restart) is out of scope for this round's ask and not a regression — noted for completeness only.

### Assessment

**Fix round verdict:** All findings addressed

**Reasoning:** All five findings have direct file:line evidence of correct implementation, corroborated by tests I ran myself (`-race` full suite, `-count=200` flakiness re-run, and reading the two disk-persistence tests to confirm they read from disk rather than memory); the two gaps worth naming — no restart-seeding test, and a log-only give-up signal — are genuine but are follow-up-grade, not reopeners, since neither was part of what round 3 was asked to close and neither breaks the guarantees that were asked for.
