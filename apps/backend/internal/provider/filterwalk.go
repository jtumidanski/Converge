package provider

import "context"

// FilterWalk paginates a provider listing that cannot filter server-side.
//
// fetch returns one upstream page (1-based) and whether the upstream has more
// pages. Items for which keep returns true are counted; the first
// (page.Number-1)*page.Size of them are skipped and the next page.Size are
// returned.
//
// HasNext is observational: it is true only when a kept item beyond the
// requested page was actually seen. A walk that stops because it reached
// maxPages reports HasNext=false, so a capped result reads as "that is all we
// can offer" rather than dangling a page the caller could never fill. The
// upstream error is returned unwrapped so the provider sentinel mapping in
// api.classify still matches.
func FilterWalk[T any](
	ctx context.Context,
	page Page,
	maxPages int,
	fetch func(ctx context.Context, upstreamPage int) ([]T, bool, error),
	keep func(T) bool,
) (Slice[T], error) {
	page = page.Normalize()
	skip := (page.Number - 1) * page.Size
	out := make([]T, 0, page.Size)
	for p := 1; p <= maxPages; p++ {
		items, hasNext, err := fetch(ctx, p)
		if err != nil {
			return Slice[T]{}, err
		}
		for _, item := range items {
			if !keep(item) {
				continue
			}
			if skip > 0 {
				skip--
				continue
			}
			if len(out) == page.Size {
				return Slice[T]{Items: out, HasNext: true}, nil
			}
			out = append(out, item)
		}
		if !hasNext {
			return Slice[T]{Items: out, HasNext: false}, nil
		}
	}
	return Slice[T]{Items: out, HasNext: false}, nil
}
