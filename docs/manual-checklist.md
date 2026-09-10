# Manual verification checklist

Run against real GitHub and GitLab repositories before a release. Automated
tests use local repositories only, so these scenarios cover the provider
contracts that fixtures cannot. This satisfies FR-12.4 (GitHub squash and
merge-commit PRs, GitLab squash and merge-commit MRs) and extends it with the
rebase, fast-forward, error, lifecycle, and token-leak scenarios exercised by
this codebase.

Record the result and the date for each row.

## Setup

- [ ] `.env` configures one GitHub provider and one GitLab provider with tokens
      that can read the target repositories.
- [ ] `docker compose up -d` serves the UI and `/healthz` reports the expected
      version.

## GitHub

- [ ] **Squash PR.** Select one squash-merged PR. The base is the commit before
      it landed, the diff matches the PR's own "Files changed" view.
- [ ] **Merge-commit PR.** Select one PR merged with a merge commit and more
      than one commit. The reconstruction contains the whole PR, not just the
      last commit.
- [ ] **Rebase PR.** Select one PR merged with "Rebase and merge". Diagnostics
      report strategy `rebase`, and every commit of the PR appears in the diff.
- [ ] **Several PRs with unrelated work between them.** Select two or three PRs
      that landed with other people's commits interleaved. No file touched only
      by the unrelated commits appears in the combined diff.
- [ ] **Missing commit.** Select a PR whose landing commit was removed from the
      branch (force-push or branch rewrite). The build fails with
      `MISSING_COMMITS` and names the SHA. GitHub's fetch-by-SHA behaviour is
      undocumented, so confirm the failure is clean rather than a hang.

## GitLab

- [ ] **Squash MR.** Same expectation as the GitHub squash case;
      `squash_commit_sha` is used as the landing commit.
- [ ] **Merge-commit MR.** A project with merge method "merge commit".
- [ ] **Fast-forward MR.** A project with merge method "fast-forward". Confirm
      the landing commit resolves from the candidate chain and the strategy is
      reported as `rebase` or `squash` depending on the commit count.
- [ ] **Self-hosted instance.** Repeat one scenario against a self-hosted GitLab
      to confirm `BASE_URL` handling and `/api/v4` appending.

## Errors and lifecycle

- [ ] **Not merged.** Selecting an open PR/MR fails with `NOT_MERGED` naming the
      number.
- [ ] **Incompatible targets.** Selecting changes that targeted different
      branches fails with `INCOMPATIBLE_TARGETS` listing each target.
- [ ] **Conflict.** Two selected changes that touch the same lines produce a
      `CONFLICTED` session listing the files, and the panel names the change.
- [ ] **Dependency.** A change that depends on an unselected change produces a
      conflict whose message mentions work outside the review, and the
      unselected change is absent from the applied list.
- [ ] **Finish Review.** After finishing, the session directory is gone, the
      mirror still works for a new review, and finishing twice is harmless.
- [ ] **Expiry.** With `SESSION_TTL_HOURS=1`, a session older than the TTL is
      expired by the sweep and its directory is removed.
- [ ] **Restart recovery.** Restarting the server while a build is running marks
      that session `FAILED` with code `INTERRUPTED`.

## Token-leak sweep

- [ ] `docker compose logs converge | grep -iE '<token prefix>|authorization|private-token'` finds
      no token value.
- [ ] Every API response body for a review contains no token and no
      `extraheader` string.
- [ ] Inside the container: `git -C /data/repositories/<provider>/<ns>/<name>.git remote get-url origin`
      prints a URL with no credentials.
- [ ] `cat /data/workspaces/<id>/session.json` contains no token.
