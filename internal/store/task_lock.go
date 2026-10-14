package store

// taskLockShards serialize filesystem reads/ingestion across owning lanes.
const taskLockShards = 64

// WithTaskLock serializes one task's file effects without blocking other tasks.
func (s *Store) WithTaskLock(taskID string, fn func() error) error {
	var hash uint32 = 2166136261
	for i := 0; i < len(taskID); i++ {
		hash ^= uint32(taskID[i])
		hash *= 16777619
	}
	mu := &s.taskLocks[hash%taskLockShards]
	mu.Lock()
	defer mu.Unlock()
	return fn()
}
