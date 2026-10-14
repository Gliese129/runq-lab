package backend

import "github.com/gliese129/runq-lab/internal/store"

func (m *MultiBackend) ViewStore() *store.Store { return m.store }
func (b *storeQueries) ViewStore() *store.Store { return b.store }
