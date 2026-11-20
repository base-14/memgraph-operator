// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"errors"
	"testing"
	"time"

	"github.com/base14/memgraph-operator/internal/memgraph"
)

type fakeCounter struct {
	counts map[string]graphCounts
	errs   map[string]error
	calls  map[string]int
}

func newFakeCounter(counts map[string]graphCounts) *fakeCounter {
	return &fakeCounter{counts: counts, errs: map[string]error{}, calls: map[string]int{}}
}

func (f *fakeCounter) count(pod string) (graphCounts, error) {
	f.calls[pod]++
	if err := f.errs[pod]; err != nil {
		return graphCounts{}, err
	}
	return f.counts[pod], nil
}

func (f *fakeCounter) total() int {
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

func infosWithoutCounts(pods ...string) map[string]*memgraph.StorageInfo {
	infos := map[string]*memgraph.StorageInfo{}
	for _, p := range pods {
		infos[p] = &memgraph.StorageInfo{MemoryRes: 1}
	}
	return infos
}

func TestGraphCountCacheLeavesReportedCountsAlone(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	cache := newGraphCountCache(func() time.Time { return now })
	counter := newFakeCounter(map[string]graphCounts{"g-0": {vertices: 9, edges: 9}})
	infos := map[string]*memgraph.StorageInfo{"g-0": {VertexCount: 100, EdgeCount: 200, HasCounts: true}}

	cache.fill("ns/c", infos, counter.count)

	if counter.total() != 0 {
		t.Fatalf("count called %d times, want 0 when SHOW STORAGE INFO already reports counts", counter.total())
	}
	if infos["g-0"].VertexCount != 100 || infos["g-0"].EdgeCount != 200 {
		t.Errorf("counts = %d/%d, want 100/200 unchanged", infos["g-0"].VertexCount, infos["g-0"].EdgeCount)
	}
}

func TestGraphCountCacheCountsAllPodsTogetherAndReusesWithinInterval(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	cache := newGraphCountCache(func() time.Time { return now })
	counter := newFakeCounter(map[string]graphCounts{
		"g-0": {vertices: 604, edges: 742251},
		"g-1": {vertices: 604, edges: 742250},
	})

	first := infosWithoutCounts("g-0", "g-1")
	cache.fill("ns/c", first, counter.count)
	if counter.calls["g-0"] != 1 || counter.calls["g-1"] != 1 {
		t.Fatalf("calls = %v, want each pod counted once on first fill", counter.calls)
	}
	for pod, want := range counter.counts {
		got := first[pod]
		if !got.HasCounts || got.VertexCount != want.vertices || got.EdgeCount != want.edges {
			t.Errorf("%s = %+v, want counts %+v and HasCounts", pod, got, want)
		}
	}

	now = now.Add(4 * time.Minute)
	counter.counts["g-0"] = graphCounts{vertices: 1, edges: 1}
	second := infosWithoutCounts("g-0", "g-1")
	cache.fill("ns/c", second, counter.count)
	if counter.total() != 2 {
		t.Fatalf("count called %d times in total, want 2: within the interval the cache must be reused", counter.total())
	}
	if second["g-0"].EdgeCount != 742251 || !second["g-0"].HasCounts {
		t.Errorf("g-0 = %+v, want cached 742251 edges", second["g-0"])
	}

	now = now.Add(time.Minute)
	third := infosWithoutCounts("g-0", "g-1")
	cache.fill("ns/c", third, counter.count)
	if counter.calls["g-0"] != 2 || counter.calls["g-1"] != 2 {
		t.Fatalf("calls = %v, want every pod recounted once the interval has passed", counter.calls)
	}
	if third["g-0"].EdgeCount != 1 {
		t.Errorf("g-0 edges = %d, want refreshed value 1", third["g-0"].EdgeCount)
	}
}

func TestGraphCountCacheFailedCount(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	cache := newGraphCountCache(func() time.Time { return now })
	counter := newFakeCounter(map[string]graphCounts{
		"g-0": {vertices: 10, edges: 20},
		"g-1": {vertices: 10, edges: 20},
	})
	counter.errs["g-1"] = errors.New("exec failed")

	first := infosWithoutCounts("g-0", "g-1")
	cache.fill("ns/c", first, counter.count)
	if first["g-1"].HasCounts {
		t.Errorf("g-1 HasCounts = true after a failed count with nothing cached, want false so drift is not computed from 0")
	}
	if !first["g-0"].HasCounts || first["g-0"].EdgeCount != 20 {
		t.Errorf("g-0 = %+v, want counted despite g-1 failing", first["g-0"])
	}

	delete(counter.errs, "g-1")
	now = now.Add(5 * time.Minute)
	second := infosWithoutCounts("g-0", "g-1")
	cache.fill("ns/c", second, counter.count)

	counter.errs["g-1"] = errors.New("exec failed again")
	counter.counts["g-1"] = graphCounts{vertices: 99, edges: 99}
	now = now.Add(5 * time.Minute)
	third := infosWithoutCounts("g-0", "g-1")
	cache.fill("ns/c", third, counter.count)
	if !third["g-1"].HasCounts || third["g-1"].EdgeCount != 20 {
		t.Errorf("g-1 = %+v, want the last good count (20 edges) kept when a refresh fails", third["g-1"])
	}
}

func TestGraphCountCacheClustersAreIndependent(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	cache := newGraphCountCache(func() time.Time { return now })
	counter := newFakeCounter(map[string]graphCounts{"g-0": {vertices: 1, edges: 2}})

	cache.fill("a/c", infosWithoutCounts("g-0"), counter.count)
	cache.fill("b/c", infosWithoutCounts("g-0"), counter.count)

	if counter.calls["g-0"] != 2 {
		t.Errorf("calls = %d, want 2: a fresh cluster must not reuse another cluster's counts", counter.calls["g-0"])
	}
}
