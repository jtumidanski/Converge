package session

import (
	"errors"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/workspace"
)

// Builder constructs a new CREATING session.
type Builder struct {
	s       Session
	changes []int
	ttl     time.Duration
}

func NewBuilder() *Builder { return &Builder{} }

func (b *Builder) SetID(v string) *Builder              { b.s.id = v; return b }
func (b *Builder) SetProviderID(v string) *Builder      { b.s.providerID = v; return b }
func (b *Builder) SetRepository(v string) *Builder      { b.s.repository = v; return b }
func (b *Builder) SetBaseBranch(v string) *Builder      { b.s.baseBranch = v; return b }
func (b *Builder) SetRequestedChanges(v []int) *Builder { b.changes = v; return b }
func (b *Builder) SetCreatedAt(v time.Time) *Builder    { b.s.createdAt = v; return b }
func (b *Builder) SetTTL(v time.Duration) *Builder      { b.ttl = v; return b }
func (b *Builder) SetOwner(v string) *Builder           { b.s.owner = v; return b }

// Build validates every invariant.
func (b *Builder) Build() (Session, error) {
	s := b.s
	if err := workspace.ValidateSessionID(s.id); err != nil {
		return Session{}, err
	}
	if s.providerID == "" {
		return Session{}, errors.New("session: provider id is required")
	}
	if err := gitx.ValidateRepoFullName(s.repository); err != nil {
		return Session{}, err
	}
	if err := gitx.ValidateBranchSyntax(s.baseBranch); err != nil {
		return Session{}, err
	}
	changes, err := gitx.ValidateChangeNumbers(b.changes)
	if err != nil {
		return Session{}, err
	}
	if b.ttl <= 0 {
		return Session{}, errors.New("session: ttl must be positive")
	}
	if s.createdAt.IsZero() {
		return Session{}, errors.New("session: created at is required")
	}
	s.requestedChanges = changes
	s.status = StatusCreating
	s.updatedAt = s.createdAt
	s.expiresAt = s.createdAt.Add(b.ttl)
	return s, nil
}
