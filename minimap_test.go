package main

import "testing"

func TestMinimapPlace(t *testing.T) {
	m := &minimap{
		dim:       2,
		xy:        [][2]float32{{0.1, 0.1}, {0.9, 0.1}, {0.5, 0.9}},
		centroids: [][]float32{{1, 0}, {0, 1}, {-1, 0}},
	}
	// On a centroid (at any length): that cell first, and its position.
	p := m.place([]float32{3, 0})
	if len(p.Near) != 3 || p.Near[0] != 0 || p.Near[2] != 2 || p.X < 0.099 || p.X > 0.101 {
		t.Errorf("on centroid 0: got %+v", p)
	}
	// Equally similar to cells 0 and 1: halfway between them.
	p = m.place([]float32{0.7071, 0.7071})
	if p.X < 0.49 || p.X > 0.51 || p.Y < 0.099 || p.Y > 0.101 {
		t.Errorf("between 0 and 1: got %+v", p)
	}
}
