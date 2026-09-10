# Risks — task-001-combined-review-mvp

| Risk | Impact | Mitigation |
|---|---|---|
| Rebase/fast-forward merges rewrite commit SHAs, so provider commit lists do not match what landed on the target branch. | Wrong landing commits, wrong base, or missing-commit failures. | Resolve landing commits by walking first-parent history back from the provider-reported head SHA by commit count and verifying `git patch-id` equality; fail with `BASE_UNDETERMINED` when verification fails. Cover with fixture tests. |
| GitHub merged-PR listing has no server-side `merged` filter on the pulls endpoint; the Search API has low rate limits. | Slow or incomplete PR lists on busy repositories. | Decide in design after checking current docs; page through closed PRs filtered by `merged_at` with a bounded page count, and expose `search` for number lookups that hit `GET /pulls/{n}` directly. |
| Cherry-picking a merge commit with `-m 1` on a base that is not the merge's first parent can produce spurious conflicts when the PR branch was long-lived. | False `CONFLICTED` sessions. | Accept for MVP; the applicator abstraction allows a later tree/patch-based strategy. Document in Diagnostics. |
| Large repositories make first-use mirror clones slow. | Long `CREATING` stage, poor first impression. | Persist the mirror volume; show stage progress; clone/fetch timeout configurable. |
| Credentials leaking through git configuration or logs. | Token exposure. | Per-invocation credential injection, tests asserting remote URL and logs contain no token, log redaction of `Authorization`/`PRIVATE-TOKEN` values. |
| Scope: one branch delivers backend, frontend, Docker, and two CI systems. | Very large PR, slow review. | Plan phase orders work CLI-first so reconstruction is proven before UI; reviewer agents run per area. |
| CI publishing to GHCR and GitLab registry cannot be fully verified locally. | Mainline publish job breaks after merge. | Keep registry logic in `make docker-push` driven by env vars and dry-run it locally against a local registry container in `make test-integration` or a documented manual step. |
