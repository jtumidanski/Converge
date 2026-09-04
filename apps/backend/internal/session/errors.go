package session

import "errors"

// ErrNotFound is returned for unknown session IDs.
var ErrNotFound = errors.New("session: not found")

// Diagnostics carries operator-facing details; the UI shows them only under "Diagnostics".
type Diagnostics struct {
	WorkspacePath string `json:"workspacePath,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Strategy      string `json:"strategy,omitempty"`
	SourceSHA     string `json:"sourceSha,omitempty"`
}

// ReviewError is both the domain error and the API "error" attribute.
type ReviewError struct {
	Code               Code         `json:"code"`
	Message            string       `json:"message"`
	Change             int          `json:"change,omitempty"`
	Commit             string       `json:"commit,omitempty"`
	ConflictingFiles   []string     `json:"conflictingFiles,omitempty"`
	AppliedChanges     []int        `json:"appliedChanges,omitempty"`
	PossibleDependency bool         `json:"possibleDependency,omitempty"`
	Diagnostics        *Diagnostics `json:"diagnostics,omitempty"`
}

func (e *ReviewError) Error() string { return string(e.Code) + ": " + e.Message }

// clone deep-copies so callers cannot mutate a Session's error.
func (e *ReviewError) clone() *ReviewError {
	if e == nil {
		return nil
	}
	c := *e
	c.ConflictingFiles = append([]string(nil), e.ConflictingFiles...)
	c.AppliedChanges = append([]int(nil), e.AppliedChanges...)
	if e.Diagnostics != nil {
		d := *e.Diagnostics
		c.Diagnostics = &d
	}
	return &c
}
