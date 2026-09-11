package gitx

// ReviewBranchPrefix namespaces the branches Converge creates in a mirror for
// per-session worktrees. The workspace manager creates them and the mirror
// cache must keep them out of a pruning fetch; those packages are peers, so
// the name they share lives in the layer beneath both.
const ReviewBranchPrefix = "review/"

// MirrorRefspec is the refspec a `clone --mirror` configures for origin.
const MirrorRefspec = "+refs/*:refs/*"

// ExcludeReviewRefspec keeps ReviewBranchPrefix branches out of a fetch.
//
// A mirror fetches MirrorRefspec, so `--prune` would otherwise delete every
// refs/heads/review/<id> — those exist only locally and never on origin.
// Deleting one out from under a live session worktree leaves its HEAD a
// dangling symref, which makes the next cherry-pick in that worktree fail
// with exit 128. Negative refspecs require git >= 2.29, comfortably below
// app.MinGitVersion.
const ExcludeReviewRefspec = "^refs/heads/" + ReviewBranchPrefix + "*"
