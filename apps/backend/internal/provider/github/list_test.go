package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/provider"
)

// newScanRaceServer serves an unbounded stream of synthetic merged-PR pages
// for atlas/race, 10 items per page, so ensureScanned needs several page
// fetches per scan. That keeps the shared scan cache entry growing over many
// separate lock acquisitions while other goroutines are concurrently reading
// whatever ensureScanned last handed them for the same (repo, target) key.
func newScanRaceServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/atlas/race/pulls" {
			w.WriteHeader(404)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		w.Header().Set("Link", fmt.Sprintf(`<http://x/repos/atlas/race/pulls?page=%d>; rel="next"`, page+1))
		const perPage = 10
		base := (page - 1) * perPage
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[`)
		for i := 0; i < perPage; i++ {
			if i > 0 {
				fmt.Fprint(w, `,`)
			}
			n := base + i + 1
			fmt.Fprintf(w, `{"number":%d,"title":"pr %d","state":"closed","html_url":"https://github.com/atlas/race/pull/%d",`+
				`"user":{"login":"jsmith"},"created_at":"2026-08-20T09:00:00Z","merged_at":"2026-08-21T14:02:11Z",`+
				`"head":{"ref":"feat/%d","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"base":{"ref":"main"}}`, n, n, n, n)
		}
		fmt.Fprint(w, `]`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestListMergedChangesConcurrentScanIsRaceFree exercises ListMergedChanges
// from many goroutines against the same (repo, target) cache key at once.
// ensureScanned must not hand a caller a live *scanEntry: the cache entry is
// shared and mutated by every concurrent scan for that key, so reading its
// fields after the lock is released is a data race. Run with -race.
func TestListMergedChangesConcurrentScanIsRaceFree(t *testing.T) {
	srv := newScanRaceServer(t)
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	c := New("gh", "GitHub", srv.URL, config.NewSecret("ghp_test"), srv.Client(), func() time.Time { return now })
	repo, err := provider.NewRepositoryBuilder().
		SetProviderID("gh").
		SetFullName("atlas/race").
		SetCloneURL("https://github.com/atlas/race.git").
		Build()
	if err != nil {
		t.Fatalf("repo setup: %v", err)
	}

	const goroutines = 500
	const rounds = 8
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*rounds)
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			for r := 0; r < rounds; r++ {
				pageNum := ((i+r)%20 + 1)
				_, err := c.ListMergedChanges(context.Background(), repo, "main", "", provider.Page{Number: pageNum, Size: 3})
				if err != nil {
					errs <- err
				}
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("ListMergedChanges: %v", err)
	}
}

// newEmptyScanServer answers every merged-PR listing with an empty page and no
// Link header, so one scan costs exactly one request and terminates.
func newEmptyScanServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func boundedCacheRepo(t *testing.T) provider.Repository {
	t.Helper()
	repo, err := provider.NewRepositoryBuilder().
		SetProviderID("gh").
		SetFullName("atlas/race").
		SetCloneURL("https://github.com/atlas/race.git").
		Build()
	if err != nil {
		t.Fatalf("repo setup: %v", err)
	}
	return repo
}

// The scan cache is keyed on the caller-supplied ?target= branch, so a client
// that varies it must not be able to grow the map without limit.
func TestScanCacheEvictsOldestWhenFull(t *testing.T) {
	srv := newEmptyScanServer(t)
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	c := New("gh", "GitHub", srv.URL, config.NewSecret("ghp_test"), srv.Client(), func() time.Time { return now })
	repo := boundedCacheRepo(t)

	for i := 0; i < MaxScanCacheEntries*3; i++ {
		if _, err := c.ListMergedChanges(context.Background(), repo, fmt.Sprintf("feature/b%d", i), "", provider.Page{Number: 1, Size: 30}); err != nil {
			t.Fatalf("target %d: %v", i, err)
		}
	}
	c.mu.Lock()
	n := len(c.scans)
	c.mu.Unlock()
	if n != MaxScanCacheEntries {
		t.Errorf("scan cache holds %d entries, want it bounded at %d", n, MaxScanCacheEntries)
	}
}

// Entries past their TTL are dropped rather than counted against the cap.
func TestScanCacheDropsExpiredEntries(t *testing.T) {
	srv := newEmptyScanServer(t)
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	c := New("gh", "GitHub", srv.URL, config.NewSecret("ghp_test"), srv.Client(), func() time.Time { return now })
	repo := boundedCacheRepo(t)

	for i := 0; i < 10; i++ {
		if _, err := c.ListMergedChanges(context.Background(), repo, fmt.Sprintf("feature/b%d", i), "", provider.Page{Number: 1, Size: 30}); err != nil {
			t.Fatalf("target %d: %v", i, err)
		}
		now = now.Add(2 * ScanCacheTTL)
	}
	c.mu.Lock()
	n := len(c.scans)
	c.mu.Unlock()
	if n != 1 {
		t.Errorf("scan cache holds %d expired entries, want only the newest", n)
	}
}
