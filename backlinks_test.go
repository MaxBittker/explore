package main

import "testing"

func pageIds(images []imageMeta) []int {
	var out []int
	for _, img := range images {
		out = append(out, img.Id)
	}
	return out
}

func TestWithBacklink(t *testing.T) {
	page := func(ids ...int) []imageMeta {
		var out []imageMeta
		for _, id := range ids {
			out = append(out, imageMeta{Id: id})
		}
		return out
	}
	eq := func(got []imageMeta, want ...int) {
		t.Helper()
		g := pageIds(got)
		if len(g) != len(want) {
			t.Fatalf("got %v, want %v", g, want)
		}
		for i := range g {
			if g[i] != want[i] {
				t.Fatalf("got %v, want %v", g, want)
			}
		}
	}
	// A full page gives up its last image.
	eq(withBacklink(page(1, 2, 3), imageMeta{Id: 9}, 3), 1, 2, 9)
	// A short page (after dedupe) just grows.
	eq(withBacklink(page(1, 2), imageMeta{Id: 9}, 3), 1, 2, 9)
	// Already on the page: unchanged.
	eq(withBacklink(page(1, 9, 3), imageMeta{Id: 9}, 3), 1, 9, 3)
	// The anchor is never replaced.
	eq(withBacklink(page(1), imageMeta{Id: 9}, 1), 1, 9)
}
