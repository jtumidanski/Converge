package review

import (
	"fmt"
	"strings"
)

// TargetMismatch is one change whose target branch differs from the requested base.
type TargetMismatch struct {
	Number int
	Target string
}

func joinNumbers(numbers []int) string {
	parts := make([]string, len(numbers))
	for i, n := range numbers {
		parts[i] = fmt.Sprintf("#%d", n)
	}
	return strings.Join(parts, ", ")
}

// MsgNotMerged reports unmerged selections.
func MsgNotMerged(numbers []int) string {
	return fmt.Sprintf("%s is not merged yet. Converge can only reconstruct merged PRs/MRs.", joinNumbers(numbers))
}

// MsgIncompatibleTargets reports selections that targeted a different branch.
func MsgIncompatibleTargets(pairs []TargetMismatch) string {
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = fmt.Sprintf("#%d targets %s", p.Number, p.Target)
	}
	return "All selected PRs/MRs must target the same base branch: " + strings.Join(parts, ", ") + "."
}

// MsgNotOnBaseBranch reports a change whose landing commit is not on the base branch.
func MsgNotOnBaseBranch(number int, branch string) string {
	return fmt.Sprintf("#%d does not appear on %s. It may have been reverted or the branch was rewritten.", number, branch)
}

// MsgMissingCommits reports commits absent from the repository.
func MsgMissingCommits(shas []string) string {
	short := make([]string, len(shas))
	for i, s := range shas {
		if len(s) > 7 {
			s = s[:7]
		}
		short[i] = s
	}
	return "Some commits are no longer available in the repository: " + strings.Join(short, ", ") + "."
}

// MsgBaseUndetermined reports that the base could not be computed.
func MsgBaseUndetermined(reason string) string {
	return "Converge could not determine a base for this selection: " + reason + "."
}

// MsgConflict reports a conflict, optionally flagging a likely dependency.
func MsgConflict(number int, dependency bool) string {
	msg := fmt.Sprintf("#%d conflicts while being applied.", number)
	if dependency {
		msg += " It may depend on work that is not part of this review."
	}
	return msg
}

// MsgProviderAuth reports rejected credentials.
func MsgProviderAuth(providerID string) string {
	return fmt.Sprintf("The token configured for provider %q was rejected.", providerID)
}

// MsgProviderUnavailable reports an unreachable provider.
func MsgProviderUnavailable(providerID string) string {
	return fmt.Sprintf("Provider %q is unavailable right now. Try again shortly.", providerID)
}

// MsgRepositoryUnavailable reports a repository that could not be read or cloned.
func MsgRepositoryUnavailable(repo string) string {
	return fmt.Sprintf("The repository %s could not be read.", repo)
}

// MsgGitFailure reports an unexpected git failure.
func MsgGitFailure() string {
	return "A git command failed while building the review. See Diagnostics for details."
}

// MsgInterrupted reports a build cut short by a restart.
func MsgInterrupted() string {
	return "The review was interrupted by a server restart before it finished building."
}
