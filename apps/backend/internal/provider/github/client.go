// Package github implements provider.GitProvider against the GitHub REST API.
package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

const (
	// APIVersion is sent on every request via the X-GitHub-Api-Version header.
	APIVersion = "2022-11-28"
	// MaxScanPages bounds how many provider pages ensureScanned will fetch
	// while building the merged-PR cache for one (repo, target) pair.
	MaxScanPages = 10
	// ScanCacheTTL is how long a scanned page set is reused before refetching.
	ScanCacheTTL = 60 * time.Second
	// MaxScanCacheEntries bounds how many (repo, target) page sets are kept.
	// The cache key includes the caller-supplied ?target= branch, so without a
	// cap a client could grow it without limit by varying that parameter.
	MaxScanCacheEntries = 64
	// MaxPRCommits bounds how many commits GetChangeCommits will collect.
	MaxPRCommits = 250

	providerPage = 100
)

var linkNextRe = regexp.MustCompile(`<[^>]+>;\s*rel="next"`)

// Client talks to one GitHub instance.
type Client struct {
	id          string
	displayName string
	baseURL     string
	token       config.Secret
	http        *http.Client
	now         func() time.Time

	mu    sync.Mutex
	scans map[string]*scanEntry
}

// New builds a client; baseURL has no trailing slash.
func New(id, displayName, baseURL string, token config.Secret, client *http.Client, now func() time.Time) *Client {
	if now == nil {
		now = time.Now
	}
	return &Client{
		id:          id,
		displayName: displayName,
		baseURL:     baseURL,
		token:       token,
		http:        client,
		now:         now,
		scans:       map[string]*scanEntry{},
	}
}

func (c *Client) ID() string          { return c.id }
func (c *Client) Kind() provider.Kind { return provider.KindGitHub }
func (c *Client) DisplayName() string { return c.displayName }
func (c *Client) BaseURL() string     { return c.baseURL }

func (c *Client) newRequest(ctx context.Context, path string, query url.Values) (*http.Request, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token.Reveal())
	req.Header.Set("X-GitHub-Api-Version", APIVersion)
	return req, nil
}

// get performs a GET and reports whether a rel="next" link exists.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) (bool, error) {
	req, err := c.newRequest(ctx, path, query)
	if err != nil {
		return false, err
	}
	h, err := provider.DoJSON(ctx, c.http, req, out)
	if err != nil {
		return false, err
	}
	return linkNextRe.MatchString(h.Get("Link")), nil
}

// ListRepositories returns the caller's accessible repositories.
func (c *Client) ListRepositories(ctx context.Context, page provider.Page) (provider.Slice[provider.Repository], error) {
	page = page.Normalize()
	q := url.Values{
		"affiliation": {"owner,collaborator,organization_member"},
		"sort":        {"full_name"},
		"per_page":    {strconv.Itoa(page.Size)},
		"page":        {strconv.Itoa(page.Number)},
	}
	var raw []repoJSON
	hasNext, err := c.get(ctx, "/user/repos", q, &raw)
	if err != nil {
		return provider.Slice[provider.Repository]{}, err
	}
	items := make([]provider.Repository, 0, len(raw))
	for _, r := range raw {
		repo, err := r.toModel(c.id)
		if err != nil {
			return provider.Slice[provider.Repository]{}, err
		}
		items = append(items, repo)
	}
	return provider.Slice[provider.Repository]{Items: items, HasNext: hasNext}, nil
}

// GetRepository fetches a single repository by "owner/name".
func (c *Client) GetRepository(ctx context.Context, fullName string) (provider.Repository, error) {
	if err := gitx.ValidateRepoFullName(fullName); err != nil {
		return provider.Repository{}, err
	}
	var raw repoJSON
	if _, err := c.get(ctx, "/repos/"+fullName, nil, &raw); err != nil {
		return provider.Repository{}, err
	}
	return raw.toModel(c.id)
}

// GetChange fetches a single pull request.
func (c *Client) GetChange(ctx context.Context, repo provider.Repository, number int) (provider.ChangeRequest, error) {
	if err := gitx.ValidateChangeNumber(number); err != nil {
		return provider.ChangeRequest{}, err
	}
	var raw pullJSON
	if _, err := c.get(ctx, fmt.Sprintf("/repos/%s/pulls/%d", repo.FullName(), number), nil, &raw); err != nil {
		return provider.ChangeRequest{}, err
	}
	return raw.toModel(c.id, repo)
}

// GetChangeCommits fetches every commit on a pull request, bounded by MaxPRCommits.
func (c *Client) GetChangeCommits(ctx context.Context, repo provider.Repository, number int) ([]provider.Commit, error) {
	if err := gitx.ValidateChangeNumber(number); err != nil {
		return nil, err
	}
	var out []provider.Commit
	for page := 1; ; page++ {
		var raw []commitJSON
		q := url.Values{"per_page": {strconv.Itoa(providerPage)}, "page": {strconv.Itoa(page)}}
		hasNext, err := c.get(ctx, fmt.Sprintf("/repos/%s/pulls/%d/commits", repo.FullName(), number), q, &raw)
		if err != nil {
			return nil, err
		}
		for _, r := range raw {
			cm, err := r.toModel()
			if err != nil {
				return nil, err
			}
			out = append(out, cm)
		}
		if len(out) > MaxPRCommits {
			return nil, fmt.Errorf("pull %d: %w", number, provider.ErrTooManyCommits)
		}
		if !hasNext || len(raw) == 0 {
			return out, nil
		}
	}
}

// CloneURL returns the repository's HTTPS clone URL.
func (c *Client) CloneURL(repo provider.Repository) string { return repo.CloneURL() }

// AuthorizeGit injects the token through GIT_CONFIG_* env, scoped to the clone host.
func (c *Client) AuthorizeGit(repo provider.Repository, spec *gitx.Spec) error {
	env, err := gitx.CredentialEnv(repo.CloneURL(), provider.GitUser(provider.KindGitHub), c.token.Reveal())
	if err != nil {
		return err
	}
	spec.Env = append(spec.Env, env...)
	return nil
}

var _ provider.GitProvider = (*Client)(nil)
