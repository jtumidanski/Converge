# Task 10 Audit — internal/diff

- **Scope:** `apps/backend/internal/diff/{diff.go,parse.go,diff_test.go,parse_test.go}` (commit `4c53549`, diffed against `87cee9f`)
- **Date:** 2026-09-04
- **Build/Test:** Not re-run (implementer's TDD evidence in task-10-report.md accepted per dispatch instructions — RED/GREEN transitions shown for all four test functions, plus `go vet`, `golangci-lint run ./...` (0 issues), `go test -race -count=1 ./...` (all packages ok), `CGO_ENABLED=0 go build ./...`).

## Parser correctness

- **Modify/Add/Delete (single-path record)** — PASS. `parseRaw` default branch, `parse.go:395-400`: consumes header token + 1 path token (`i += 2`), errors if `i+1 >= len(toks)` (`parse.go:396-398`). Verified by hand-trace against `parse_test.go:9` fixture (A/M/D all single `\x00`-terminated path).
- **Rename (two-path record)** — PASS. `parse.go:389-394`: on `code == 'R'`, consumes header + old-path token (`toks[i+1]`) + new-path token (`toks[i+2]`), `i += 3`, guarded by `i+2 >= len(toks)` (`parse.go:390-392`). Field mapping is correct — `Path: toks[i+2]` (new), `PreviousPath: toks[i+1]` (old) — matches git's raw record order (old path emitted before new path). Verified against `parse_test.go:9` `R095` case and real-git `README.md`→`docs/README.md` in `diff_test.go:242-244`.
- **Copy (two-path record, shares rename branch)** — case `'C'` folds into the same 3-token branch as `'R'` (`parse.go:389`), structurally correct by the same field-count reasoning as rename, since git's raw grammar gives copy records the identical old-path/new-path shape. **However this branch is entirely untested** — no fixture in `parse_test.go`, no real-git scenario in `diff_test.go`, and it is currently unreachable in production because none of `WriteCombined`/`Summarize`/`FileContent` pass `-C`/`--find-copies` (only `--find-renames` at `diff.go:82,110,114,152`). See Job B Important-1.
- **Binary files in `--numstat`** — PASS, not counted as `0`. `parse.go:428-429`: `if parts[0] == "-" || parts[1] == "-" { e.Binary = true }`, skipping the `strconv.Atoi` path entirely so `Additions`/`Deletions` stay at their zero value *and* `Binary` is explicitly set to distinguish this from a real 0/0 change. Verified by `parse_test.go:492` (`img.png` → `{Path:"img.png", Additions:0, Deletions:0, Binary:true}`) and real-git `bin.dat` in `diff_test.go:245-247,267-269`.
- **Paths with spaces/odd bytes** — PASS. All parsing operates on `-z` NUL-delimited tokens (`splitNul`, `parse.go:365-371`); the only intra-record splitting is `strings.Fields` on the *raw header* (`parse.go:382`, fixed 5 whitespace-separated fields that never contain the path) and `strings.SplitN(toks[i], "\t", 3)` on the numstat *count* line (`parse.go:423`, which stops after 2 tabs and takes the remainder verbatim as path or leaves it empty for rename form). No code re-splits a path token on whitespace. Verified real-git via `has space.txt` in `diff_test.go:299,318-320,327-329` — both `Summarize` accounting and `FileContent`'s pathspec argument.
- **Empty diff** — code-verified only, not real-git-tested. `splitNul` returns `nil` for empty input (`parse.go:367-369`), so `parseRaw`/`parseNumstat` return `(nil, nil)` and `Summarize` returns `([]FileSummary{}, Totals{}, nil)` with no error. No test in `diff_test.go` exercises `base == head` / a no-op range end to end. See Job B Important-3.
- **`--raw`/`--numstat` correlation** — joined **by path**, not positionally. `Summarize` builds `counts := map[string]numstatEntry` keyed by `numstatEntry.Path` (`diff.go:126-129`), then iterates `raws` and looks up `counts[e.Path]` (`diff.go:132-133`). Both parsers assign `Path` as the *new* path for renames (`parse.go:393` / `parse.go:443`), so the join key is consistent across streams — this avoids the positional-zip failure mode the review explicitly warns about. **Gap:** a map miss silently yields a zero-value `numstatEntry{}` (`Additions:0, Deletions:0, Binary:false`) rather than an error — see Job B Important-2.

## Job A — Spec compliance

✅ Spec compliant. All eight declared interfaces match the brief exactly:

- `type FileStatus string` + 4 constants — `diff.go:34-41`, verbatim values `"added"/"modified"/"deleted"/"renamed"`.
- `FileSummary` — `diff.go:47-54`, field types and order match.
- `Totals` — `diff.go:57-61`, matches.
- `FileDiff` (embeds `FileSummary`, `Truncated bool`, `Diff string`) — `diff.go:64-68`, matches. Per controller note, no JSON tags required on `Truncated`/`Diff` since neither is marshalled directly (Task 19 owns the wire DTO) — not flagged.
- `const MaxFileDiffBytes = 1 << 20` — `diff.go:44`, matches.
- `WriteCombined(ctx, r gitx.Runner, repoDir, base, head, outPath string) error` — `diff.go:78`, matches; writes via `--find-renames base head` per brief comment, atomic temp-file + rename (`diff.go:86-102`).
- `Summarize(ctx, r gitx.Runner, repoDir, base, head string) ([]FileSummary, Totals, error)` — `diff.go:106`, matches; explicit `sort.Slice` by `Path` at `diff.go:140`.
- `FileContent(ctx, r gitx.Runner, repoDir, base, head string, f FileSummary) (FileDiff, error)` — `diff.go:145`, matches.
- `parseRaw(b []byte) ([]rawEntry, error)` / `parseNumstat(b []byte) ([]numstatEntry, error)` — `parse.go:374`, `parse.go:419`, matches, both unexported per brief.

**JSON tag check, character by character:**

| Field | Required | Actual | Verdict |
|---|---|---|---|
| `FileSummary.Path` | `path` | `` `json:"path"` `` (`diff.go:48`) | ✅ |
| `FileSummary.PreviousPath` | `previousPath` | `` `json:"previousPath,omitempty"` `` (`diff.go:49`) | ✅ — `,omitempty` is present verbatim in the brief's own Step 4 illustrative code, not an implementer addition; tag name matches exactly |
| `FileSummary.Status` | `status` | `` `json:"status"` `` (`diff.go:50`) | ✅ |
| `FileSummary.Additions` | `additions` | `` `json:"additions"` `` (`diff.go:51`) | ✅ |
| `FileSummary.Deletions` | `deletions` | `` `json:"deletions"` `` (`diff.go:52`) | ✅ |
| `FileSummary.Binary` | `binary` | `` `json:"binary"` `` (`diff.go:53`) | ✅ |
| `Totals.Files` | `files` | `` `json:"files"` `` (`diff.go:58`) | ✅ |
| `Totals.Additions` | `additions` | `` `json:"additions"` `` (`diff.go:59`) | ✅ |
| `Totals.Deletions` | `deletions` | `` `json:"deletions"` `` (`diff.go:60`) | ✅ |

⚠️ Cannot verify from this diff alone: whether Task 19's HTTP DTO actually maps `FileDiff.Truncated`/`.Diff` to `truncated`/`diff` on the wire (out of this task's scope per controller note — flagged there as Task 19's responsibility, not re-flagged here).

Files created match the brief's `Files:` list exactly: `diff.go`, `parse.go`, `parse_test.go`, `diff_test.go` — no extra or missing files (`TestSummarizeSpacesAndDeletion` is an added *test function* inside the specified `diff_test.go`, not an extra file).

## Job B — Audit

Package classification: **Support package** — no `model.go`, no `resource.go`; plain functional parsing/orchestration library. DOM-*/SUB-* checklists N/A. Not an auth service; SEC-* checklist N/A beyond argument-injection safety, covered under priority 5 below.

**Priority 2 — errors vs. zero values:** `WriteCombined`, `Summarize`, `FileContent` all propagate git run errors via `fmt.Errorf(...: %w, err)` (`diff.go:84,112,116,165`) rather than swallowing them — no repeat of Task 7/8's defect for the git-call layer itself. The one remaining silent-zero risk is the raw/numstat correlation gap (Important-2 below), which is a join-time gap, not a git-error swallow.

**Priority 3 — bounds/truncation:** `MaxFileDiffBytes` honoured at `diff.go:168-171`; cut is `text[:MaxFileDiffBytes]` triggered only when `len(text) > MaxFileDiffBytes` (strict `>`), so `Truncated` is `false` when the diff is exactly at the boundary and the returned `Diff` is exactly `MaxFileDiffBytes` bytes when it is truncated — matches `TestFileContentTruncates`'s assertion (`diff_test.go:284`, `len(fd.Diff) != MaxFileDiffBytes`). Whether the underlying `gitx.Runner.Run` streams/caps stdout before this point, or fully buffers an arbitrarily large diff before `FileContent` gets to truncate it, is outside this diff (`gitx` internals not touched here) — flagged as Minor-3 below since it's the same *shape* of bug the brief calls out (Task 6's unbounded pagination) but not confirmable from these four files alone.

**Priority 4 — determinism:** Sort is explicit and total: `sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })` (`diff.go:140`) — no map-iteration leakage (the `counts` map is only used for lookups, never ranged into the output order). Test coverage is weak: `diff_test.go:248` only asserts `files[0].Path > files[1].Path`, i.e., the first two of four produced entries, not a full-slice non-decreasing check across all `N`. See Minor-1.

**Priority 5 — argument safety:** `base`/`head` validated via `gitx.ValidateSHA` in `validateRange` (`diff.go:70-75`), called at the top of all three exported functions (`diff.go:79,107,146`). `FileContent`'s user-influenced `f.Path`/`f.PreviousPath` validated via `gitx.ValidatePathArg` (`diff.go:149,154`) *and* placed after a `--` separator in the git args (`diff.go:152,157`) — belt-and-suspenders, both layers present. Verified end to end by `diff_test.go:271-273` (`FileSummary{Path: "-bad"}` rejected before any git call).

**Priority 6 — test quality:** Confirmed genuinely real-git-driven for integration behavior — `diff_test.go` uses `testutil.NewRepo`/`src.Commit`/`src.Git` throughout (`diff_test.go:11 (import), 197,277,296`), no hand-written fixture strings pretending to be git output at that layer. Hand-written byte fixtures are confined to `parse_test.go`, which is appropriate for unit-level malformed/truncated-input testing that can't be produced by real git. Record-form coverage: add/modify/delete/rename/binary/spaces/deletion are each real-git-exercised at least once; **copy (`C`) is not**, per the parser-correctness section above.

## Strengths

- Path-keyed (not positional) join between `--raw` and `--numstat` streams (`diff.go:126-133`) avoids the highest-severity failure mode the dispatch calls out.
- Binary detection is explicit (`Binary` field), not inferred from a `0/0` count — closes the exact silent-corruption gap the task brief warns about.
- `-z` used consistently everywhere paths could contain problematic bytes; no whitespace re-splitting on path tokens.
- Truncated/malformed raw and numstat records return errors rather than dropping entries or entries with wrong field counts (`parse.go:380,384,391,397,425,441`).
- Argument safety is double-layered (`ValidatePathArg` + `--` separator) for the one function taking client-traceable path input.
- Honest self-reporting: implementer flagged the untested `C` branch and the `FileDiff` JSON-tag ambiguity itself rather than claiming full coverage.

## Issues

#### Critical (Must Fix)

None found.

#### Important (Should Fix)

1. **Copy (`C`) raw record parsing is unexercised** — `parse.go:389` (`case 'R', 'C':`) claims to handle copy records but has zero test coverage (no fixture in `parse_test.go`, no real-git scenario in `diff_test.go`) and is currently unreachable in production, since no exported function passes `-C`/`--find-copies` (`diff.go:82,110,114,152` all use `--find-renames` only). This is plan-mandated code (matches the brief's Step 2 code verbatim, including the shared-branch design and the 4-status model with no `StatusCopied`) — not a defect to remove, but an unexercised record form the parser claims to handle, which the calibration guidance rates Important. Recommend: add a hand-crafted `C`-record fixture to `parse_test.go` mirroring the existing `R095` case (`parse_test.go:9`) to lock in the shared-branch behavior at the unit level, since a real-git copy-detection test isn't reachable without wiring `-C` into an exported function.
2. **Raw/numstat correlation has no cross-stream invariant check** — `diff.go:133` (`n := counts[e.Path]`) silently returns a zero-value `numstatEntry{}` on a map miss, producing `Additions:0, Deletions:0, Binary:false` — indistinguishable from a legitimate empty change. If the two independent git invocations (`diff.go:82` vs `diff.go:110/114`) ever diverge on file set (git version quirk, rename-threshold edge case, future flag change), a real change is silently zeroed rather than erroring. No test exercises this path. Recommend asserting `len(raws) == len(nums)` (or that every raw entry's path has a numstat hit) in `Summarize` and returning an error on mismatch.
3. **Empty-diff path (`base == head`) is not exercised by any real-git test** — reasoned about only via `splitNul`'s empty-input handling (`parse.go:367-369`); no test in `diff_test.go` calls `Summarize`/`WriteCombined` with a no-op range. This is exactly the class of "verify behavior, don't infer it" gap the dispatch calls out (three earlier tasks in this plan hit ordering/edge-case flakes). Recommend a `TestSummarizeEmptyDiff` against `testutil.NewRepo` with `base == head`.

#### Minor (Nice to Have)

1. Sort-order assertion (`diff_test.go:248`) checks only `files[0]` vs `files[1]`, not the full 4-element ordering — a sort regression affecting only entries beyond index 1 would pass this test.
2. `FileContent`'s truncation (`diff.go:168-171`) cuts on raw byte offset without regard to UTF-8 rune boundaries; a multi-byte character straddling the 1 MiB cut could be corrupted in the returned string. Not required by the brief.
3. Whether `gitx.Runner.Run` bounds stdout capture before `FileContent` gets to truncate (`diff.go:163-172`) can't be confirmed from this diff — worth a direct check against `gitx`'s implementation given this is the same unbounded-then-truncate shape flagged for Task 6.

## Assessment

**Task quality:** Approved

**Reasoning:** The core parsing grammar (raw and numstat, including the rename/copy 2-path form and the binary `-`/`-` form) is implemented correctly and the highest-severity risk named in the dispatch — positional stream misalignment — is avoided by a path-keyed join. The three Important findings are real robustness/coverage gaps (untested copy branch, un-guarded correlation miss, untested empty-diff path) but none currently produce observably wrong output against any test or real-git scenario exercised; they should be fixed as fast-follow hardening rather than blocking this task, especially since Tasks 11/15/19/22 depend on the interface shape (which is fully spec-compliant), not on these edge cases.

## Fix round 1 re-review

- **Diff reviewed:** `review-4c53549..b05f695.diff` (commit `b05f695`), files touched: `apps/backend/internal/diff/diff.go`, `apps/backend/internal/diff/diff_test.go`, `apps/backend/internal/diff/parse_test.go`.
- **Tests run:** `go test -race -count=1 ./internal/diff/... -v` (foreground, cwd `apps/backend`) — all 10 tests PASS, matching the report verbatim.

### Finding verdicts

1. **Copy-record fixture: ADDRESSED.** `apps/backend/internal/diff/parse_test.go:33-56` adds `TestParseRawCopy` with a hand-written `C090` record (`:100644 100644 <sha> <sha> C090\x00src/orig.go\x00src/copy.go\x00`) followed by a plain `M` record. Grammar check: git's raw `-z` copy-record shape is header + old-path + new-path, identical to rename — the fixture matches this (score line, then `src/orig.go`, then `src/copy.go`), and asserts `Path="src/copy.go"` (dest), `PreviousPath="src/orig.go"` (source), plus that the following `M` record still parses correctly, proving the 3-token consumption didn't desync the stream. Production `case 'R', 'C':` branch is untouched (`apps/backend/internal/diff/parse.go:52`), confirmed via `grep -n "case 'R'"`. Confirmed no `-C`/`--find-copies` was added to any exported function — `grep -n "find-copies\|\"-C\""` across `diff.go`/`parse.go` returns only a comment string in the new test (`parse_test.go:29`), zero occurrences in production code.

2. **Raw/numstat mismatch errors (both directions): ADDRESSED.** `apps/backend/internal/diff/diff.go:150-153` errors on a `--raw`-only path (`counts[e.Path]` miss) with `fmt.Errorf("diff: raw/numstat mismatch: %s present in --raw but missing from --numstat", e.Path)`; `diff.go:168-174` adds a second pass over `nums` checking each was `seen` during the raw loop, erroring with the converse message naming the `--numstat`-only path. Both messages interpolate only the bare path string — no `res.Stdout`/`res.Stderr`/command args are included, so no data leak. `TestSummarizeRawNumstatMismatch` (`diff_test.go:262-281`) and `TestSummarizeNumstatRawMismatch` (`diff_test.go:287-306`) both drive deliberately inconsistent `--raw`/`--numstat` streams through `gitx.FakeRunner{Handler: ...}` (dispatching on `s.Args` containing `"--raw"` vs `"--numstat"`), not a hand-built error value — each asserts `err != nil` and `strings.Contains(err.Error(), "<mismatched-path>")`, which would fail if the join logic silently zero-filled instead of erroring.

3. **Empty-diff real-git test: ADDRESSED.** `TestSummarizeEmptyDiff` (`diff_test.go:311-344`) uses `testutil.NewRepo(t)` + `gitx.NewExecRunner` (real git, not `FakeRunner`), calls `Summarize(ctx, runner, src.Work, head, head)`, asserts `len(files) == 0`, `totals == Totals{}`, `err == nil`, then calls `WriteCombined` on the same empty range and asserts the output file exists with zero bytes. This is real git, matching the requirement.

4. **Full sort-order assertion: ADDRESSED, but the assertion is not discriminating — flagging as a residual test-quality gap, not re-opening the finding.** `diff_test.go:150-165` replaced the 2-of-4 check with a full `wantOrder := []string{"bin.dat", "docs/README.md", "mod.txt", "new.txt"}` comparison against all four entries — this genuinely addresses "checked only the first two of four" as literally stated. However, tracing the data path: `Summarize` builds `files` by iterating `raws` in the order `parseRaw` returns them (`diff.go:112-122`, a plain `for _, e := range raws` append, no map involved before the final `sort.Slice` at `diff.go:176`), and `raws` preserves the order tokens appear in `git diff --raw -z` stdout. Git's raw-diff output is tree order, which for this fixture's four top-level paths (`bin.dat`, `docs/README.md`, `mod.txt`, `new.txt`) is already byte-lexicographic (`b` < `d` < `m` < `n`) — i.e., identical to the `sort.Slice(... Path <)` result the code applies afterward. That means this specific fixture's raw git output already arrives in the exact order the test expects, independent of whether `diff.go:176`'s `sort.Slice` call executes at all. The reviewer's stated bar — "the chosen paths would actually fail if the sort were wrong" — is not met: no map is involved (so "map-iteration order leaking through" isn't the risk here), but a broken/removed `sort.Slice` would not be caught by this fixture either, because git's own tree-order emission coincides with the expected lexical order for these four names. This is a real gap in test discrimination, not a fabricated one — confirmed by reading `parseRaw`'s token-order-preserving loop (`parse.go` main loop, `i` walks `toks` in stream order, `out = append(out, e)`) and `Summarize`'s non-reordering pre-sort loop.

5. **Rune-boundary truncation: ADDRESSED.** `truncateToRuneBoundary` (`diff.go:174-181` in the new code / actual location `diff.go:176-183`) starts `cut := max` and decrements while `!utf8.RuneStart(b[cut])`, returning `b[:cut]`. Three required properties hold: (a) never exceeds `max` — `cut` only ever decreases from `max`, and the loop guard `cut > 0` prevents underflow, so `len(b[:cut]) <= max` always; (b) output is valid UTF-8 — backing off to the nearest preceding rune-start byte and excluding it via `b[:cut]` (exclusive) means the returned prefix never contains a partial trailing rune; (c) `Truncated` remains accurate — `FileContent` still sets `out.Truncated = true` inside the same `if len(text) > MaxFileDiffBytes` branch that calls the new helper (`diff.go:166-169`), unchanged trigger condition. `TestFileContentTruncatesOnRuneBoundary` (`diff_test.go:205-230`) uses 700,000 repetitions of the 2-byte rune `"é"`, which reliably forces a mid-rune cut at `MaxFileDiffBytes` (an even boundary would land exactly between runes only if `MaxFileDiffBytes` happens to be even relative to the rune-repeat offset — the file's unified-diff header prefix of unpredictable length makes the parity of the cut point effectively arbitrary, so this is a real regression-catching fixture, not a coincidentally-passing one), and asserts `len(fd.Diff) <= MaxFileDiffBytes`, `utf8.ValidString(fd.Diff)`, and `Truncated`. Per the report's explicit acknowledgment, the existing `TestFileContentTruncates` boundary assertion was loosened from `!=` to `<=` for the *cap* check (`diff_test.go:193`) — as instructed, the implementer said so rather than quietly changing it — while retaining a separate exact `== MaxFileDiffBytes` check (`diff_test.go:196-198`) for that test's pure-ASCII fixture, where every byte is a rune boundary and the cut is guaranteed to land exactly on the cap. Both properties are genuinely tested by different fixtures.

### Contract check

JSON tags and public signatures unchanged: **confirmed.** `diff.go`'s only changes are: one new import (`unicode/utf8`, `diff.go:10`), two new error-return branches plus a new `seen` map and post-loop check inside `Summarize` (`diff.go:112-134`, none touching struct definitions), the `text[:MaxFileDiffBytes]` → `truncateToRuneBoundary(text, MaxFileDiffBytes)` call-site swap in `FileContent` (`diff.go:166-168`), and the new unexported `truncateToRuneBoundary` function. `FileSummary` (`diff.go:29-36`), `Totals` (`diff.go:39-43`), and `FileDiff` (`diff.go:46-50`) struct bodies are byte-identical to the pre-fix version per direct read — no tag or field changed. `parse.go` was not touched at all in this diff (only `parse_test.go` gained a new test function).

### Test quality

- `TestParseRawCopy` — would fail on regression: if the `case 'R', 'C':` branch were ever changed to consume a different token count or swap `Path`/`PreviousPath`, both this test and the following `M` record's assertions would fail (desync detection built in). Not vacuous.
- `TestSummarizeRawNumstatMismatch` / `TestSummarizeNumstatRawMismatch` — would fail on regression: if `Summarize` reverted to zero-filling on a map miss, `err` would be `nil` and `t.Fatal("expected error...")` would fire. Error-message assertion (`strings.Contains`) would also fail if the message dropped the path. Not vacuous.
- `TestSummarizeEmptyDiff` — would fail on regression: if `Summarize` ever returned a non-nil error or non-zero totals for `base == head`, or if `WriteCombined` wrote a non-empty file for an empty diff, the corresponding assertions fire. Not vacuous.
- `TestSummarizeWriteAndFileContent`'s strengthened order check — **would NOT reliably fail on a `sort.Slice` regression**, per Finding 4 above: the fixture's natural raw-git emission order already coincides with the expected sorted order, so removing or breaking the sort would not be caught by this specific fixture. This is the one vacuous-under-regression assertion found in this fix round.
- `TestFileContentTruncatesOnRuneBoundary` — would fail on regression: reverting to a plain `text[:MaxFileDiffBytes]` byte slice against this multi-byte-rune fixture would produce invalid UTF-8 with high probability (a truncation exactly mid-`"é"` codepoint), which `utf8.ValidString` would catch; also would fail the `Truncated`/cap checks if those regressed. Not vacuous.

### New breakage in the fix diff

None found at Critical/Important severity. `Summarize`'s new converse-direction loop (`diff.go:168-174`) is a second full pass over `nums` after the first pass over `raws` — O(n) extra work, not a correctness or security issue. No new `os/exec`, no new `//nolint`, no `.golangci.yml` changes (confirmed via `git diff 4c53549..b05f695 -- .golangci.yml` — no such path touched by this commit; last `.golangci.yml` change is an unrelated prior commit `dae9a4e`). `gitx.ValidatePathArg` call site in `FileContent` is untouched by this diff.

### Deferred minors

- Finding 4's residual gap (sort-order test not actually regression-discriminating for this fixture, since git tree order coincides with lexical order at the top level) — worth a follow-up test that forces a case where raw-stream order and sorted order diverge (e.g., nested paths where git's tree-order comparison and pure byte-lexicographic `Path <` comparison would disagree, such as a file named `mod-a.txt` vs a directory `mod/`), but does not block this fix round since the literal finding text ("checked only the first two of four") was addressed.

### Assessment

**Fix round verdict:** All findings addressed
**Reasoning:** All five findings are structurally fixed with passing, non-fake-abusing tests and no scope creep into out-of-scope areas (`-C` flag, JSON tags, `.golangci.yml`, public signatures); Finding 4's fix satisfies the literal ask but leaves a non-discriminating test as a deferred minor rather than a blocking gap, since the finding's literal defect (2-of-4 coverage) is resolved.
