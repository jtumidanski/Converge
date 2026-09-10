// Package session holds the review session model and its on-disk store.
package session

// Code is a review error code exposed by the API and CLI.
type Code string

const (
	CodeNotMerged             Code = "NOT_MERGED"
	CodeIncompatibleTargets   Code = "INCOMPATIBLE_TARGETS"
	CodeNotOnBaseBranch       Code = "NOT_ON_BASE_BRANCH"
	CodeMissingCommits        Code = "MISSING_COMMITS"
	CodeBaseUndetermined      Code = "BASE_UNDETERMINED"
	CodeConflict              Code = "CONFLICT"
	CodeProviderAuth          Code = "PROVIDER_AUTH"
	CodeProviderUnavailable   Code = "PROVIDER_UNAVAILABLE"
	CodeRepositoryUnavailable Code = "REPOSITORY_UNAVAILABLE"
	CodeGitFailure            Code = "GIT_FAILURE"
	CodeInterrupted           Code = "INTERRUPTED"
)

// Status is the session lifecycle state.
type Status string

const (
	StatusCreating   Status = "CREATING"
	StatusReady      Status = "READY"
	StatusConflicted Status = "CONFLICTED"
	StatusFailed     Status = "FAILED"
	StatusFinished   Status = "FINISHED"
	StatusExpired    Status = "EXPIRED"
)

// Strategy is how a change landed on the target branch.
type Strategy string

const (
	StrategyMerge  Strategy = "merge"
	StrategySquash Strategy = "squash"
	StrategyRebase Strategy = "rebase"
)

// Stage names.
const (
	StageResolving         = "resolving"
	StageUpdatingRepo      = "updating-repository"
	StageCreatingWorkspace = "creating-workspace"
	StageApplyingPrefix    = "applying:"
	StageDiffing           = "diffing"
)
