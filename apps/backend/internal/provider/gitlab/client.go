// Package gitlab implements provider.GitProvider against the GitLab REST API v4.
package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

const (
	providerPage = 100
	// MaxMRCommits bounds how many commits GetChangeCommits will collect.
	MaxMRCommits = 250
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Client talks to one GitLab instance.
type Client struct {
	id          string
	displayName string
	baseURL     string
	token       config.Secret
	http        *http.Client
}

// New builds a client; baseURL is the instance root without trailing slash.
func New(id, displayName, baseURL string, token config.Secret, client *http.Client) *Client {
	return &Client{id: id, displayName: displayName, baseURL: strings.TrimRight(baseURL, "/"), token: token, http: client}
}

func (c *Client) ID() string          { return c.id }
func (c *Client) Kind() provider.Kind { return provider.KindGitLab }
func (c *Client) DisplayName() string { return c.displayName }
func (c *Client) BaseURL() string     { return c.baseURL }

func projectPath(fullName string) string { return "/projects/" + url.PathEscape(fullName) }

// get performs a GET under /api/v4 and returns the X-Next-Page value.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) (string, error) {
	u := c.baseURL + "/api/v4" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("gitlab: build request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", c.token.Reveal())
	h, err := provider.DoJSON(ctx, c.http, req, out)
	if err != nil {
		return "", err
	}
	return h.Get("X-Next-Page"), nil
}

func (c *Client) ListRepositories(ctx context.Context, page provider.Page) (provider.Slice[provider.Repository], error) {
	page = page.Normalize()
	q := url.Values{"membership": {"true"}, "order_by": {"path"}, "sort": {"asc"}, "per_page": {strconv.Itoa(page.Size)}, "page": {strconv.Itoa(page.Number)}}
	var raw []projectJSON
	next, err := c.get(ctx, "/projects", q, &raw)
	if err != nil {
		return provider.Slice[provider.Repository]{}, err
	}
	items := make([]provider.Repository, 0, len(raw))
	for _, p := range raw {
		r, err := p.toModel(c.id)
		if err != nil {
			return provider.Slice[provider.Repository]{}, err
		}
		items = append(items, r)
	}
	return provider.Slice[provider.Repository]{Items: items, HasNext: next != ""}, nil
}

func (c *Client) GetRepository(ctx context.Context, fullName string) (provider.Repository, error) {
	if err := gitx.ValidateRepoFullName(fullName); err != nil {
		return provider.Repository{}, err
	}
	var raw projectJSON
	if _, err := c.get(ctx, projectPath(fullName), nil, &raw); err != nil {
		return provider.Repository{}, err
	}
	return raw.toModel(c.id)
}

func (c *Client) listMRs(ctx context.Context, repo provider.Repository, q url.Values) ([]provider.ChangeRequest, string, error) {
	var raw []mrJSON
	next, err := c.get(ctx, projectPath(repo.FullName())+"/merge_requests", q, &raw)
	if err != nil {
		return nil, "", err
	}
	items := make([]provider.ChangeRequest, 0, len(raw))
	for _, m := range raw {
		cr, err := m.toModel(c.id, repo)
		if err != nil {
			return nil, "", err
		}
		items = append(items, cr)
	}
	return items, next, nil
}

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
	base := url.Values{"state": {"merged"}, "target_branch": {target}, "sort": {"desc"}, "per_page": {strconv.Itoa(page.Size)}, "page": {strconv.Itoa(page.Number)}}
	search = strings.TrimSpace(search)
	if search == "" {
		items, next, err := c.listOrdered(ctx, repo, base)
		if err != nil {
			return empty, err
		}
		return provider.Slice[provider.ChangeRequest]{Items: items, HasNext: next != ""}, nil
	}

	// Server-side search: query by author_username first (when the search
	// term looks like a username) and by title last, so the title query -
	// the common case - is the most recent request observed by callers/tests
	// inspecting request order. Results from both are merged and re-sorted.
	var items []provider.ChangeRequest
	hasNext := false
	if usernameRe.MatchString(search) {
		authorQ := cloneValues(base)
		authorQ.Set("author_username", search)
		byAuthor, next, err := c.listOrdered(ctx, repo, authorQ)
		if err != nil {
			return empty, err
		}
		items = byAuthor
		hasNext = next != ""
	}
	titleQ := cloneValues(base)
	titleQ.Set("search", search)
	titleQ.Set("in", "title")
	byTitle, next, err := c.listOrdered(ctx, repo, titleQ)
	if err != nil {
		return empty, err
	}
	hasNext = hasNext || next != ""
	items = mergeUnique(items, byTitle)
	sortMergedDesc(items)
	// The author and title queries are each independently paginated against
	// GitLab, so their merged/re-sorted union can exceed page.Size. Truncate
	// back to the requested page size; this makes the merged page boundary
	// approximate rather than exact, which is accepted for this search
	// convenience path (see task-6 fix review, finding 2).
	if len(items) > page.Size {
		items = items[:page.Size]
		hasNext = true
	}
	return provider.Slice[provider.ChangeRequest]{Items: items, HasNext: hasNext}, nil
}

// listOrdered asks for order_by=merged_at (GitLab >= 17.2) and falls back to created_at on 400.
func (c *Client) listOrdered(ctx context.Context, repo provider.Repository, q url.Values) ([]provider.ChangeRequest, string, error) {
	q = cloneValues(q)
	q.Set("order_by", "merged_at")
	items, next, err := c.listMRs(ctx, repo, q)
	var se *provider.StatusError
	if err != nil && errors.As(err, &se) && se.Status == http.StatusBadRequest {
		q.Set("order_by", "created_at")
		items, next, err = c.listMRs(ctx, repo, q)
		if err == nil {
			sortMergedDesc(items)
		}
	}
	return items, next, err
}

func (c *Client) GetChange(ctx context.Context, repo provider.Repository, number int) (provider.ChangeRequest, error) {
	if err := gitx.ValidateChangeNumber(number); err != nil {
		return provider.ChangeRequest{}, err
	}
	var raw mrJSON
	if _, err := c.get(ctx, fmt.Sprintf("%s/merge_requests/%d", projectPath(repo.FullName()), number), nil, &raw); err != nil {
		return provider.ChangeRequest{}, err
	}
	return raw.toModel(c.id, repo)
}

func (c *Client) GetChangeCommits(ctx context.Context, repo provider.Repository, number int) ([]provider.Commit, error) {
	if err := gitx.ValidateChangeNumber(number); err != nil {
		return nil, err
	}
	var all []commitJSON
	for page := 1; ; page++ {
		var raw []commitJSON
		q := url.Values{"per_page": {strconv.Itoa(providerPage)}, "page": {strconv.Itoa(page)}}
		next, err := c.get(ctx, fmt.Sprintf("%s/merge_requests/%d/commits", projectPath(repo.FullName()), number), q, &raw)
		if err != nil {
			return nil, err
		}
		all = append(all, raw...)
		if len(all) > MaxMRCommits {
			return nil, fmt.Errorf("merge request %d: %w", number, provider.ErrTooManyCommits)
		}
		if next == "" || len(raw) == 0 {
			break
		}
	}
	return toCommits(all)
}

func (c *Client) CloneURL(repo provider.Repository) string { return repo.CloneURL() }

func (c *Client) AuthorizeGit(repo provider.Repository, spec *gitx.Spec) error {
	env, err := gitx.CredentialEnv(repo.CloneURL(), provider.GitUser(provider.KindGitLab), c.token.Reveal())
	if err != nil {
		return err
	}
	spec.Env = append(spec.Env, env...)
	return nil
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func mergeUnique(a, b []provider.ChangeRequest) []provider.ChangeRequest {
	seen := map[int]struct{}{}
	out := make([]provider.ChangeRequest, 0, len(a)+len(b))
	for _, list := range [][]provider.ChangeRequest{a, b} {
		for _, cr := range list {
			if _, dup := seen[cr.Number()]; dup {
				continue
			}
			seen[cr.Number()] = struct{}{}
			out = append(out, cr)
		}
	}
	return out
}

var _ provider.GitProvider = (*Client)(nil)
