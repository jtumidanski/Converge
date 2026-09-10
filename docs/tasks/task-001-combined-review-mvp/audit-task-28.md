# Task 28 audit — GitHub Actions and GitLab CI pipelines

## Verdict: PASS (with one Important finding)

Both pipelines match the brief's Interfaces section and FR-14.3. The R58 formatting fix is
verified byte-identical to a clean Prettier re-run and semantically inert. All local gates
(backend + frontend) are clean. One Important finding: the workflow-level `permissions:` block
grants `contents: write` + `packages: write` to the `validate` job, which needs neither — a real
least-privilege violation, not a theoretical one, since `validate` runs untrusted PR code with
that token in scope for non-fork PRs.

---

## Priority question — GHCR lowercasing interaction

**Answer: (c), with a caveat.** As written, `docker-build` and `docker-push` in the `publish` job
produce **matching** tags, because `IMAGE_REPOSITORY` is set explicitly and identically in both
the "Build artifacts and image" step (`.github/workflows/ci.yml:90-96`) and the "Push image" step
(`:98-102`):

```
.github/workflows/ci.yml:93:          IMAGE_REPOSITORY: ghcr.io/${{ github.repository }}
...
.github/workflows/ci.yml:101:          IMAGE_REPOSITORY: ghcr.io/${{ github.repository }}
```

Evidence from `Makefile:10`:

```
IMAGE     ?= $(if $(IMAGE_REPOSITORY),$(IMAGE_REPOSITORY),$(if $(CI_REGISTRY_IMAGE),$(CI_REGISTRY_IMAGE),converge))
```

`IMAGE_REPOSITORY` is checked first, before `CI_REGISTRY_IMAGE` or the `converge` fallback. Since
both the `docker-build` step and the `docker-push` step set the same `IMAGE_REPOSITORY` value,
`make docker-build` (`Makefile:38-46`) and `make docker-push` (`Makefile:48-49`, which invokes
`tools/docker-push.sh`) resolve `$(IMAGE)` / `$image` identically. `tools/docker-push.sh:10-11`
confirms `IMAGE_REPOSITORY`, when set, wins over its own `GITHUB_REPOSITORY` lowercasing branch
(`docker-push.sh:12-13`) — so setting it explicitly *bypasses* the script's lowercasing, but
because both steps bypass it the *same way*, build and push agree.

**Concrete tags produced in the publish job (repo `jtumidanski/converge`, `MAINLINE=1`):**

- `docker-build` tags (Makefile:42-44): `ghcr.io/jtumidanski/converge:$VERSION`,
  `ghcr.io/jtumidanski/converge:$GIT_SHA`, `ghcr.io/jtumidanski/converge:latest`
- `docker-push` pushes (docker-push.sh:21-24, same `$image`): the identical three tags.

No divergence for this repository. The residual risk is real but narrower than "build and push
disagree": it is that **any fork with an uppercase character anywhere in `owner/repo`** would have
`IMAGE_REPOSITORY: ghcr.io/${{ github.repository }}` produce an uppercase path segment (GitHub
preserves case in `github.repository`; GHCR rejects uppercase repository paths). In that case both
`docker-build` and `docker-push` would still *agree* with each other (both use the same
un-lowercased value) — `docker push` would fail, not silently push the wrong thing, since a local
`docker buildx build --load` with an uppercase tag succeeds (Docker's local tag rules are laxer
than GHCR's remote-path rules) but the subsequent `docker push` to GHCR would be rejected by the
registry. **This is graded Minor**, per the reviewer brief's own framing: it is a genuine but
fork-only, currently-inert defect, not an active build/push tag mismatch on this repository.

I did **not** re-derive option (a)'s trap independently as new information — the implementer's
report (task-28-report.md:27-40) already worked it out correctly and I verified the same
`Makefile:10` / `docker-push.sh:10-19` source lines support it: dropping `IMAGE_REPOSITORY` from
the publish job's `docker-build` step would leave `$(IMAGE)` falling through to the literal
`converge` (since `GITHUB_REPOSITORY` is not one of the Makefile's fallback variables — only
`docker-push.sh` reads it), while `docker-push` would resolve `ghcr.io/jtumidanski/converge` via
its own `GITHUB_REPOSITORY` branch. That would make `docker push ghcr.io/jtumidanski/converge:$VERSION`
fail with "an image does not exist locally with the tag" (no local image was ever built under that
name — `docker buildx build --load` only populated the local daemon under `converge:$VERSION`).
So (a) is wrong. (b) (compute the lowercased name once into `$GITHUB_ENV`) would be the more robust
fix and would also close the fork-owner edge case, but the as-written form is not a live bug for
this repository — hence PASS on this question, Minor finding recorded for the fork case.

---

## Findings

### Important — workflow-level `permissions:` over-grants to `validate`

`.github/workflows/ci.yml:9-11`:

```yaml
permissions:
  contents: write
  packages: write
```

This is a single, workflow-level block with no per-job override (confirmed: `grep -n
"permissions:" .github/workflows/ci.yml` returns only this one occurrence). GitHub Actions applies
workflow-level `permissions:` to **every** job unless a job declares its own `permissions:` block.
The `validate` job (`:18-58`) only checks out code and runs `make lint test test-integration build
docker-build` — it never touches `git push`, GitHub Releases, or GHCR. It needs `contents: read`
at most, yet it runs with `contents: write` + `packages: write` in its `GITHUB_TOKEN`.

This matters because `validate` runs on `pull_request` (`:4`) and executes attacker-influenced
build/test/lint tooling (arbitrary `Makefile`/npm/go toolchain invocations) from the PR head. For
same-repository PRs (not fork PRs — GitHub already forces fork-PR `pull_request` tokens to
read-only regardless of declared `permissions:`, so the fork case is not the exposure here), a
malicious internal branch could exploit build tooling to exfiltrate a token scoped `contents:write,
packages:write` and push a commit or a package under the repo's identity — permissions the
`validate` job never needed to have. `publish` (`:60-108`) is the only job that needs
`packages: write` (for `docker push`); `contents: write` is arguably only needed for
`release-github`'s `gh release create`, and even that runs in `publish`, never `validate`.

Fix would be to move `permissions:` to job-level (`validate: permissions: contents: read`,
`publish: permissions: contents: write / packages: write`), which the brief's own sample YAML
(task-28-brief.md:25-27) also specifies at the workflow level — so this is a **brief defect
inherited verbatim**, not an implementer deviation. Recorded as Important because it is a
findable, concrete over-permissioning, independent of whose artifact introduced it.

### Minor — GHCR lowercasing bypass is real but fork-only (see priority question above)

Graded Minor per the reasoning above: build and push tags agree for this repository; the failure
mode only exists for forks with an uppercase owner/repo, and fails loud (`docker push` rejected),
not silently wrong.

### Minor — pinned action versions are several majors behind current tags

Checked live against GitHub's release API (network available in this environment):

| Action | Pinned | Latest release (checked 2026-09-05) |
|---|---|---|
| `actions/checkout` | `@v4` | `v7.0.1` |
| `actions/setup-go` | `@v5` | `v7.0.0` |
| `actions/setup-node` | `@v4` | `v7.0.0` |
| `actions/upload-artifact` | `@v4` | `v7.0.1` |
| `docker/setup-buildx-action` | `@v3` | `v4.3.0` |
| `docker/setup-qemu-action` | `@v3` | `v4.3.0` |
| `docker/login-action` | `@v3` | `v4.6.0` |

All pinned major-version tags still resolve (verified each via
`GET /repos/<owner>/<repo>/git/ref/tags/<tag>` → `200`), so nothing is broken, but the gap (2-3
majors on most) is large enough that this is worth a note rather than silence. Not blocking.

---

## Other checks

### `.github/workflows/ci.yml` / `.gitlab-ci.yml` — YAML parse, actionlint (executed)

```
$ python3 -c "import yaml; [yaml.safe_load(open(p)) for p in ('.github/workflows/ci.yml','.gitlab-ci.yml')]; print('yaml ok')"
yaml ok

$ actionlint --version
1.7.7 (installed by downloading from release page, built with go1.23.4)

$ actionlint .github/workflows/ci.yml; echo "exit=$?"
exit=0
```

No GitLab-side static linter is installed in this environment (`gitlab-ci-local`, `yamllint` both
absent) — the `.gitlab-ci.yml` check is YAML-parse plus manual `rules:` reasoning only, same
limitation the implementer's report discloses.

### Docker base images exist (executed, network check)

```
$ curl -s .../repositories/library/golang/tags/1.27-bookworm/   → 200, image present
$ curl -s .../repositories/library/docker/tags/28-cli/          → 200, image present
$ curl -s .../repositories/library/docker/tags/28-dind/         → 200, image present
```

`go.mod:3` declares `go 1.26.0`; the GitLab toolchain pins `golang:1.27-bookworm` — a newer
toolchain than the module's `go` directive, which is compatible (newer Go toolchains build modules
declaring older `go` directives). Not a defect.

### Every make target the pipelines invoke exists (executed)

```
$ for target in lint test test-integration build docker-build docker-push release-github; do
    make -n "$target" >/dev/null 2>&1 && echo "target $target ok" || echo "target $target MISSING"
  done
target lint ok
target test ok
target test-integration ok
target build ok
target docker-build ok
target docker-push ok
target release-github ok
```

All 7 targets referenced across both files exist in the Task 27 `Makefile`. No nonexistent target.

### `continue-on-error` / `allow_failure` / `|| true` / `--exit-zero` (executed)

```
$ grep -n "continue-on-error\|allow_failure\|exit-zero" .github/workflows/ci.yml .gitlab-ci.yml
(no matches, grep exit=1)
$ grep -n '|| true' .github/workflows/ci.yml .gitlab-ci.yml
(no matches, grep exit=1)
```

No escape hatch that would let a validation step go green while failing. Confirmed independently,
not just trusted from the report.

### R58 formatting-only claim (executed, proven not eyeballed)

Extracted the base (`70b6859`) and committed (`70a7521`) versions of all three `.tsx` files into
`/tmp/task28-scratch` (outside the worktree), copied the repo's `.prettierrc.json`
(`semi: true, singleQuote: false, trailingComma: "all", printWidth: 100`), and ran the exact
pinned Prettier version from `package.json` (`"prettier": "^3.9.6"`) against the base files:

```
$ npx --yes prettier@3.9.6 --write ChangeTable.base.tsx SelectChangesPage.base.tsx SelectChangesPage.test.base.tsx
$ diff -q ChangeTable.base.tsx ChangeTable.committed.tsx           → MATCH (byte-identical)
$ diff -q SelectChangesPage.base.tsx SelectChangesPage.committed.tsx → MATCH (byte-identical)
$ diff -q SelectChangesPage.test.base.tsx SelectChangesPage.test.committed.tsx → MATCH (byte-identical)
```

Also ran `git diff 70b6859..70a7521 -w --ignore-blank-lines` on the three files: every surviving
hunk is a line-wrap/re-indent of an existing statement (import list, JSX attribute wrapping,
destructuring, object-literal wrapping) — no identifier, string literal, JSX attribute value, or
test assertion changed. Confirmed by inspection of the full whitespace-insensitive diff (reproduced
in this audit's working notes; every changed line pair is a reflow of the unchanged tokens on the
corresponding base line).

```
$ npm run format:check   → "All matched files use Prettier code style!" (exit 0)
$ npm test -- --run      → Test Files 19 passed (19), Tests 94 passed (94)
```

**No semantic change smuggled in. R58 claim fully verified, not Critical.**

### `format:check` clean (executed)

```
$ npm run format:check
> prettier --check .
Checking formatting...
All matched files use Prettier code style!
```

Exit 0. R58's purpose (task-27 audit's known-deferred failure on these 3 files) is now resolved.

---

## Trigger matrix

### GitHub Actions (`.github/workflows/ci.yml`) — reasoned (cannot execute a real workflow run)

| Event | `validate` | `publish` |
|---|---|---|
| Pull request (opened/sync/reopen) | runs (`on: pull_request`, no `if:` on the job) | does not run (`if: github.event_name == 'push'` false) |
| Push to `main` | runs | runs (`needs: validate`; `release-github` step skipped — ref is not `refs/tags/v*`) |
| Push of tag `v*` | runs | runs; `release-github` step also runs (`startsWith(github.ref, 'refs/tags/v')` true) |
| Push to any other branch | does not run (`branches: [main]` filter) | does not run |

`validate` has no job-level `if:`, so it cannot be silently skipped on a PR — matches the brief's
requirement. `publish`'s `needs: validate` (`:61`) means GitHub Actions will not start `publish` if
`validate` fails (default `needs` behavior requires success unless `if:` overrides with
`always()`/`failure()`, which is not used here) — confirmed by reading `:60-63`, no such override
present.

### GitLab CI (`.gitlab-ci.yml`) — reasoned (no GitLab runner available)

| Context | `validate` | `build` | `docker-build` | `publish` |
|---|---|---|---|---|
| Merge request pipeline | runs (`$CI_PIPELINE_SOURCE == "merge_request_event"`) | runs | runs | does not run (no MR rule in `publish:rules`) |
| Push to default branch (`main`) | runs (`$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH`) | runs | runs | runs |
| Tag push (`v*`, any tag) | runs (`$CI_COMMIT_TAG`) | runs | runs | runs |
| Push to non-default branch, no open MR | none of the 3 rules match → job **not created** for that pipeline | — | — | — |

Per GitLab semantics: a job whose `rules:` list evaluates false for every entry is not created in
the pipeline at all (distinct from "skipped"), so a plain branch push with no MR produces an empty
pipeline rather than a green-but-empty `validate`. `$CI_COMMIT_BRANCH` is indeed unset in MR
pipelines, but the MR case is caught by the separate `$CI_PIPELINE_SOURCE ==
"merge_request_event"` rule listed first, so this does not create a gap. `publish`'s `rules:`
(`:224-225`, default branch or tag) are a subset of `build`'s and `docker-build`'s rules
(`:181-184`, `:197-200`), so whenever `publish` runs, `build` is guaranteed to have already run in
the same pipeline, satisfying `dependencies: [build]` (`:216-217`) — stage ordering
(`build` before `publish`, `stages:` at `:1-4`) plus this rules subset relationship together
guarantee `publish` never starts before `build`'s artifacts exist.

**`validate` cannot be skipped on a PR/MR in either provider** — confirmed for both.

---

## Secrets / FR-14.3 (reasoned + one grep executed)

- No registry hostname is hard-coded as a literal secret; `ghcr.io` is a public, non-secret,
  fixed hostname (permitted — FR-14.3 is about credentials/registry *location as configuration*,
  not about never mentioning `ghcr.io` at all). GitLab side uses only `$CI_REGISTRY`,
  `$CI_REGISTRY_IMAGE`, `$CI_REGISTRY_USER`, `$CI_REGISTRY_PASSWORD` — all runtime CI variables.
- GHCR login (`ci.yml:83-88`): `docker/login-action@v3` with `password: ${{ secrets.GITHUB_TOKEN }}`.
  This action wraps `docker login --password-stdin` internally (its documented behavior); GitHub
  additionally masks any literal occurrence of `secrets.*` values in job logs. No token on a
  command line.
- GitLab login (`.gitlab-ci.yml:73`): `echo "$CI_REGISTRY_PASSWORD" | docker login -u
  "$CI_REGISTRY_USER" --password-stdin "$CI_REGISTRY"` — password piped via stdin, never an argv
  token, and no `set -x`/`-v` in `before_script` that would echo the pipe. Correct pattern.
- **Important finding above**: `permissions:` is not least-privilege — `validate` inherits
  `contents: write` + `packages: write` it does not use.

---

## Independent reproduction — claim by claim

| Claim (from task-28-report.md) | Reproduced | Evidence |
|---|---|---|
| Both YAML files parse | Yes | `yaml.safe_load` → `yaml ok` |
| `actionlint` clean, exit 0 | Yes | re-ran myself, `actionlint 1.7.7`, exit=0, no output |
| All 7 make targets exist | Yes | re-ran `make -n` loop myself, all `ok` |
| No `continue-on-error`/`allow_failure`/`|| true` | Yes | re-ran grep myself, zero matches |
| R58 is formatting-only | Yes, more rigorously | byte-identical Prettier re-run in an isolated scratch dir (report only diffed hunks by eye, did not re-run Prettier) |
| `format:check` passes | Yes | re-ran myself, "All matched files use Prettier code style!" |
| `npm test` → 19 files / 94 tests | Yes | re-ran myself, exact match |
| `npm run lint` clean | Yes | re-ran myself, exit 0, no output |
| Backend `go test -race -count=1 ./...` clean | Yes | re-ran myself, all `ok`, no fail |
| `go vet ./...` clean | Yes | re-ran myself, no output |
| `go tool golangci-lint run` → 0 issues | Yes | re-ran myself, "0 issues." |
| `CGO_ENABLED=0 go build ./...` clean | Yes | re-ran myself, no output |
| `make test-integration` exercises real tests (not a no-op) | Already pre-verified by controller — not re-derived, per instructions | N/A |
| IMAGE_REPOSITORY set in both publish steps makes build/push agree | Yes, independently re-derived from `Makefile:10` and `docker-push.sh:10-19`, not just trusted | see priority-question section above |
| `.gitkeep` untouched | Yes | `git status --porcelain` empty for that path throughout this audit |
| GHCR lowercase edge case is a "residual risk," not fixed | Yes, confirmed and graded (Minor, fork-only) | see priority-question section |
| `permissions:` is least-privilege | **No — report did not check this; I found it is not.** | See Important finding above. The implementer's report states "No `continue-on-error`/`allow_failure`/`\|\| true` anywhere" and a hostname/credential check, but never evaluates whether `permissions:` scope matches per-job need. |
| Action versions pinned to something real | Not claimed by report; independently checked here | Live GitHub API check, all tags resolve, versions are 2-3 majors behind current (Minor, informational) |
| Docker base images (`golang:1.27-bookworm`, `docker:28-cli`, `docker:28-dind`) exist | Not claimed by report; independently checked here | Docker Hub API, all present |

**Could not independently verify (no CI runner, consistent with report's own disclosure):**
- Whether the GitHub Actions workflow actually schedules/completes on GitHub's infrastructure.
- Whether GitLab's docker-in-docker service actually starts and reaches a real registry.
- Whether `docker buildx build --load` succeeds inside `docker:28-cli`/`dind` in a live GitLab
  runner (Task 27's audit did execute a real `docker buildx build` locally, outside CI, and it
  succeeded — that is the closest available evidence, not a live-CI reproduction).

---

## Spec-compliance verdict

**FR-14.1/14.2 (validate + publish pipelines, correct triggers):** PASS. Trigger matrix matches
the brief's Interfaces section exactly: `validate` on PR + push to `main`/`v*` tags; `publish`
needs `validate`, runs on push to `main` or a `v*` tag, logs into the correct registry, runs
`docker-build`/`docker-push`, uploads `dist/*.tar.gz`, runs `release-github` only on tags. GitLab
mirrors this with `stages`/`rules` correctly, and `publish`'s `dependencies: [build]` is safe under
the rules subset relationship.

**FR-14.3 (no committed registry hostname/credential):** PASS. Confirmed no literal credential or
non-public hostname; both providers resolve the image path through CI-provided variables.

**Standing constraint (tokens never reach a log):** PASS. Both login steps use documented
stdin/action patterns, no token on a command line.

**Least-privilege permissions:** **FAIL** as a specific sub-check — `validate` is granted
`contents: write` + `packages: write` it never uses (Important finding above). This does not
invalidate the overall PASS verdict for the task (the brief's own sample specified this exact
top-level `permissions:` block, so it is an inherited spec defect, not a new implementer bug), but
it is a real, actionable finding that should be fixed before this ships to a fork or an
externally-contributed-PR workflow.

**Overall: PASS**, carrying one Important finding (permissions scope) into any follow-up, and two
Minor findings (GHCR lowercase fork-edge-case, stale action version pins) that do not block.
