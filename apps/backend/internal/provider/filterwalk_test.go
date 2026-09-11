package provider

import (
	"context"
	"errors"
	"testing"
)

// pages builds a fetch func over fixed upstream pages of ints.
func pages(src [][]int, calls *int) func(context.Context, int) ([]int, bool, error) {
	return func(_ context.Context, n int) ([]int, bool, error) {
		*calls++
		if n < 1 || n > len(src) {
			return nil, false, nil
		}
		return src[n-1], n < len(src), nil
	}
}

func even(n int) bool { return n%2 == 0 }

func TestFilterWalk(t *testing.T) {
	src := [][]int{{1, 2, 3, 4}, {5, 6, 7, 8}, {9, 10, 11, 12}}
	cases := []struct {
		name        string
		page        Page
		maxPages    int
		wantItems   []int
		wantHasNext bool
	}{
		{"first page, more to come", Page{Number: 1, Size: 2}, 10, []int{2, 4}, true},
		{"second page", Page{Number: 2, Size: 2}, 10, []int{6, 8}, true},
		{"last page exhausts upstream", Page{Number: 3, Size: 2}, 10, []int{10, 12}, false},
		{"page past the end", Page{Number: 4, Size: 2}, 10, []int{}, false},
		{"page larger than all matches", Page{Number: 1, Size: 50}, 10, []int{2, 4, 6, 8, 10, 12}, false},
		{"cap hides remaining matches", Page{Number: 1, Size: 2}, 1, []int{2, 4}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got, err := FilterWalk(context.Background(), tc.page, tc.maxPages, pages(src, &calls), even)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Items) != len(tc.wantItems) {
				t.Fatalf("items = %v, want %v", got.Items, tc.wantItems)
			}
			for i := range tc.wantItems {
				if got.Items[i] != tc.wantItems[i] {
					t.Fatalf("items = %v, want %v", got.Items, tc.wantItems)
				}
			}
			if got.HasNext != tc.wantHasNext {
				t.Errorf("hasNext = %v, want %v", got.HasNext, tc.wantHasNext)
			}
		})
	}
}

func TestFilterWalkStopsAtCap(t *testing.T) {
	src := [][]int{{1}, {2}, {3}, {4}, {5}}
	calls := 0
	if _, err := FilterWalk(context.Background(), Page{Number: 1, Size: 10}, 2, pages(src, &calls), even); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("upstream calls = %d, want 2", calls)
	}
}

func TestFilterWalkPropagatesError(t *testing.T) {
	boom := errors.New("boom")
	fetch := func(context.Context, int) ([]int, bool, error) { return nil, false, boom }
	_, err := FilterWalk(context.Background(), Page{Number: 1, Size: 2}, 10, fetch, even)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom unwrapped", err)
	}
}
