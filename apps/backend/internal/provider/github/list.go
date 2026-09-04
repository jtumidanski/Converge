package github

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

// scanEntry caches merged PRs collected from the closed-PR listing.
type scanEntry struct {
	items     []provider.ChangeRequest
	nextPage  int // 0 when the provider has no more pages
	capped    bool
	fetchedAt time.Time
}

func (c *Client) scanKey(repo provider.Repository, target string) string {
	return repo.FullName() + "\x00" + target
}

// scanSnapshot is a point-in-time copy of a scanEntry's fields, safe to read
// without holding c.mu. ensureScanned must never return the live *scanEntry:
// the cache is keyed by (repo, target) and a concurrent call for the same key
// can keep mutating entry.items/nextPage/capped after the lock is released.
type scanSnapshot struct {
	items    []provider.ChangeRequest
	nextPage int
	capped   bool
}

// ensureScanned returns a snapshot of merged PRs for (repo, target), fetching
// provider pages until at least `need` items are collected, the provider runs
// out, or MaxScanPages is hit. need < 0 means "scan until exhausted or capped".
func (c *Client) ensureScanned(ctx context.Context, repo provider.Repository, target string, need int) (scanSnapshot, error) {
	key := c.scanKey(repo, target)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.scans[key]
	if entry == nil || c.now().Sub(entry.fetchedAt) > ScanCacheTTL {
		entry = &scanEntry{nextPage: 1, fetchedAt: c.now()}
		c.scans[key] = entry
	}
	for entry.nextPage != 0 && (need < 0 || len(entry.items) < need) {
		if entry.nextPage > MaxScanPages {
			entry.capped = true
			break
		}
		q := url.Values{
			"state":     {"closed"},
			"base":      {target},
			"sort":      {"updated"},
			"direction": {"desc"},
			"per_page":  {strconv.Itoa(providerPage)},
			"page":      {strconv.Itoa(entry.nextPage)},
		}
		var raw []pullJSON
		hasNext, err := c.get(ctx, "/repos/"+repo.FullName()+"/pulls", q, &raw)
		if err != nil {
			return scanSnapshot{}, err
		}
		for _, p := range raw {
			if p.MergedAt == nil || p.Base.Ref != target {
				continue
			}
			cr, err := p.toModel(c.id, repo)
			if err != nil {
				return scanSnapshot{}, err
			}
			entry.items = append(entry.items, cr)
		}
		if hasNext && len(raw) > 0 {
			entry.nextPage++
		} else {
			entry.nextPage = 0
		}
	}
	items := make([]provider.ChangeRequest, len(entry.items))
	copy(items, entry.items)
	return scanSnapshot{items: items, nextPage: entry.nextPage, capped: entry.capped}, nil
}

// ListMergedChanges lists merged pull requests targeting target, optionally
// filtered by a title/author substring or a "#123"/"123" number search.
func (c *Client) ListMergedChanges(ctx context.Context, repo provider.Repository, target, search string, page provider.Page) (provider.Slice[provider.ChangeRequest], error) {
	page = page.Normalize()
	if err := gitx.ValidateBranchSyntax(target); err != nil {
		return provider.Slice[provider.ChangeRequest]{}, err
	}
	empty := provider.Slice[provider.ChangeRequest]{Items: []provider.ChangeRequest{}}
	if n, ok := provider.ParseSearchNumber(search); ok {
		cr, err := c.GetChange(ctx, repo, n)
		if errors.Is(err, provider.ErrNotFound) {
			return empty, nil
		}
		if err != nil {
			return empty, err
		}
		if cr.State() != provider.StateMerged || cr.TargetBranch() != target {
			return empty, nil
		}
		return provider.Slice[provider.ChangeRequest]{Items: []provider.ChangeRequest{cr}}, nil
	}
	need := page.Number*page.Size + 1
	if search != "" {
		need = -1 // filtering shrinks the set; scan to the cap
	}
	entry, err := c.ensureScanned(ctx, repo, target, need)
	if err != nil {
		return empty, err
	}
	filtered := filterChanges(entry.items, search)
	sort.SliceStable(filtered, func(i, j int) bool {
		if !filtered[i].MergedAt().Equal(filtered[j].MergedAt()) {
			return filtered[i].MergedAt().After(filtered[j].MergedAt())
		}
		return filtered[i].Number() > filtered[j].Number()
	})
	start := (page.Number - 1) * page.Size
	if start >= len(filtered) {
		return provider.Slice[provider.ChangeRequest]{Items: []provider.ChangeRequest{}, HasNext: false}, nil
	}
	end := start + page.Size
	if end > len(filtered) {
		end = len(filtered)
	}
	hasNext := end < len(filtered) || (entry.nextPage != 0 && !entry.capped)
	return provider.Slice[provider.ChangeRequest]{Items: filtered[start:end], HasNext: hasNext}, nil
}

func filterChanges(items []provider.ChangeRequest, search string) []provider.ChangeRequest {
	out := make([]provider.ChangeRequest, 0, len(items))
	s := strings.ToLower(strings.TrimSpace(search))
	for _, cr := range items {
		if s == "" || strings.Contains(strings.ToLower(cr.Title()), s) || strings.Contains(strings.ToLower(cr.Author()), s) {
			out = append(out, cr)
		}
	}
	return out
}
