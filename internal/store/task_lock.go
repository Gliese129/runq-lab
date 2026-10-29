package store

import "sort"

// taskLockShards serialize filesystem reads/ingestion across owning lanes.
const taskLockShards = 64

func taskLockShard(taskID string) uint32 {
	var hash uint32 = 2166136261
	for i := 0; i < len(taskID); i++ {
		hash ^= uint32(taskID[i])
		hash *= 16777619
	}
	return hash % taskLockShards
}

// WithTaskLock serializes one task's file effects without blocking other tasks.
func (s *Store) WithTaskLock(taskID string, fn func() error) error {
	mu := &s.taskLocks[taskLockShard(taskID)]
	mu.Lock()
	defer mu.Unlock()
	return fn()
}

// WithTaskLocks holds the task lock of every id at once (one batched file
// effect over many tasks). Shards are taken in ascending order, so it never
// deadlocks against another WithTaskLocks or a single WithTaskLock. fn must
// not take a task lock itself: the locks are not reentrant.
func (s *Store) WithTaskLocks(taskIDs []string, fn func() error) error {
	seen := map[uint32]bool{}
	shards := make([]int, 0, len(taskIDs))
	for _, id := range taskIDs {
		if sh := taskLockShard(id); !seen[sh] {
			seen[sh] = true
			shards = append(shards, int(sh))
		}
	}
	sort.Ints(shards)
	for _, sh := range shards {
		s.taskLocks[sh].Lock()
	}
	defer func() {
		for i := len(shards) - 1; i >= 0; i-- {
			s.taskLocks[shards[i]].Unlock()
		}
	}()
	return fn()
}
