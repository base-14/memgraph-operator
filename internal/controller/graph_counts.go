// Copyright 2025 Base14. See LICENSE file for details.

package controller

import (
	"sync"
	"time"

	"github.com/base14/memgraph-operator/internal/memgraph"
)

const graphCountRefreshInterval = 5 * time.Minute

type graphCounts struct {
	vertices int64
	edges    int64
}

type clusterGraphCounts struct {
	refreshedAt time.Time
	pods        map[string]graphCounts
}

type graphCountCache struct {
	mu       sync.Mutex
	now      func() time.Time
	clusters map[string]*clusterGraphCounts
}

func newGraphCountCache(now func() time.Time) *graphCountCache {
	return &graphCountCache{now: now, clusters: map[string]*clusterGraphCounts{}}
}

func (c *graphCountCache) fill(cluster string, infos map[string]*memgraph.StorageInfo, count func(pod string) (graphCounts, error)) {
	var missing []string
	for pod, info := range infos {
		if !info.HasCounts {
			missing = append(missing, pod)
		}
	}
	if len(missing) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	entry := c.clusters[cluster]
	if entry == nil {
		entry = &clusterGraphCounts{pods: map[string]graphCounts{}}
		c.clusters[cluster] = entry
	}

	now := c.now()
	if entry.refreshedAt.IsZero() || now.Sub(entry.refreshedAt) >= graphCountRefreshInterval {
		for _, pod := range missing {
			if counts, err := count(pod); err == nil {
				entry.pods[pod] = counts
			}
		}
		entry.refreshedAt = now
	}

	for _, pod := range missing {
		if counts, ok := entry.pods[pod]; ok {
			infos[pod].VertexCount = counts.vertices
			infos[pod].EdgeCount = counts.edges
			infos[pod].HasCounts = true
		}
	}
}
