package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/logfile"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/workspace"
)

// MultiBackend routes operations to per-target backends. ListJobs aggregates
// across all targets; Submit/Kill/Get look up the target from the DB and
// delegate. Project operations are global (routed to the default target).
//
// The target map is MUTABLE at runtime (RQ-75 hot reload): the config
// reconciler adds/replaces/removes lanes as config.yaml changes. mu guards
// targets and defaultTarget; every reader goes through get/snapshot/
// defaultName so a rebuild never races an in-flight request.
type MultiBackend struct {
	// routingMu is the generation-transition barrier. User operations hold a
	// read lease from resolution through the backend effect; the config
	// reconciler and retirement sweep hold the write lease while changing
	// scopes/registry membership. This makes each operation linearizable to a
	// single lane generation without holding the registry map mutex over SSH.
	routingMu sync.RWMutex
	// controlMu serializes user control transitions that can move ownership or
	// terminalize work (retry/kill/pause/resume) across lane generations.
	controlMu     sync.Mutex
	mu            sync.RWMutex
	targets       map[string]Backend
	defaultTarget string
	// retiring holds superseded lane generations still tracking their
	// in-flight tasks (RQ-75): target name → generation → lane. Task-scoped
	// ops (kill/logs/refresh) route here when the task's stamped generation
	// has a live retiring lane — the OLD endpoint/templates are the only
	// ones that can correctly act on those tasks.
	retiring map[string]map[string]Backend
	// historical holds quiesced, generation-scoped lanes for terminal
	// artifact reads. They do not participate in refresh or control fanout,
	// but preserve the original host/workspace after retirement completes.
	historical map[string]map[string]Backend

	store *store.Store
	// activityCache: job id → cachedActivity of a finished job.
	activityCache  sync.Map
	activityCacheN atomic.Int64
	// registry is the ONE routed project registry shared by every lane
	// (RQ-65); kept here so lanes added at runtime get the same wiring
	// assembly-time lanes got.
	registry *project.Registry
	// projectSyncMu serializes project-tree mirrors. A submit must never read
	// a destination while another fan-out is halfway through replacing it.
	projectSyncMu sync.Mutex
}

// BeginRoutingUpdate excludes routed operations while the reconciler changes
// lane scopes and registry membership. The returned release function must be
// deferred by the caller. Cold lane construction should happen before taking
// this lease when practical; correctness takes precedence when Start itself
// participates in the ownership handoff.
func (m *MultiBackend) BeginRoutingUpdate() func() {
	m.routingMu.Lock()
	return m.routingMu.Unlock
}

// get returns the named backend under the read lock.
func (m *MultiBackend) get(name string) (Backend, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	be, ok := m.targets[name]
	return be, ok
}

// snapshot copies the current target map — iteration must never hold the
// lock across backend calls (a slow SSH lane would serialize the world).
func (m *MultiBackend) snapshot() map[string]Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]Backend, len(m.targets))
	for k, v := range m.targets {
		out[k] = v
	}
	return out
}

// liveLanes snapshots active and retiring lanes, optionally for one target.
// Backend calls must happen after this returns; m.mu only protects registry
// membership and must never be held across SSH work.
func (m *MultiBackend) liveLanes(target string) []Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.targets)+len(m.retiring))
	seenNames := make(map[string]struct{}, len(m.targets)+len(m.retiring))
	for name := range m.targets {
		if target == "" || name == target {
			seenNames[name] = struct{}{}
		}
	}
	for name := range m.retiring {
		if target == "" || name == target {
			seenNames[name] = struct{}{}
		}
	}
	for name := range seenNames {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []Backend
	for _, name := range names {
		if be := m.targets[name]; be != nil {
			out = append(out, be)
		}
		generations := make([]string, 0, len(m.retiring[name]))
		for generation := range m.retiring[name] {
			generations = append(generations, generation)
		}
		sort.Strings(generations)
		for _, generation := range generations {
			be := m.retiring[name][generation]
			if be != nil && (len(out) == 0 || be != out[len(out)-1]) {
				out = append(out, be)
			}
		}
	}
	return out
}

// defaultName returns the routing default under the read lock.
func (m *MultiBackend) defaultName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaultTarget
}

// SetRetiringLane registers a superseded lane generation (RQ-75).
func (m *MultiBackend) SetRetiringLane(name, generation string, be Backend) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.retiring == nil {
		m.retiring = map[string]map[string]Backend{}
	}
	if m.retiring[name] == nil {
		m.retiring[name] = map[string]Backend{}
	}
	m.retiring[name][generation] = be
}

// RotateLane atomically installs the replacement as active AND registers
// the superseded lane as retiring (round 8 #2): a concurrent resolveTask
// can never observe a state where the old generation is neither active
// nor retiring.
func (m *MultiBackend) RotateLane(name string, newBe Backend, oldGen string, oldBe Backend) {
	if sq, ok := newBe.(interface{ setProjectRegistry(*project.Registry) }); ok {
		sq.setProjectRegistry(m.registry)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.targets[name] = newBe
	if oldGen != "" && oldBe != nil {
		if m.retiring == nil {
			m.retiring = map[string]map[string]Backend{}
		}
		if m.retiring[name] == nil {
			m.retiring[name] = map[string]Backend{}
		}
		m.retiring[name][oldGen] = oldBe
	}
}

// PromoteLane atomically moves a retiring lane back to active AND, when a
// lane is being superseded by the promotion (A→B→A: B steps down as A
// steps up), registers it as retiring in the SAME transaction (round 9
// #2) — a concurrent reader never sees either generation unrouted.
func (m *MultiBackend) PromoteLane(name, gen string, be Backend, supersededGen string, supersededBe Backend) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g := m.retiring[name]; g != nil {
		delete(g, gen)
		if len(g) == 0 {
			delete(m.retiring, name)
		}
	}
	m.targets[name] = be
	if supersededGen != "" && supersededBe != nil {
		if m.retiring == nil {
			m.retiring = map[string]map[string]Backend{}
		}
		if m.retiring[name] == nil {
			m.retiring[name] = map[string]Backend{}
		}
		m.retiring[name][supersededGen] = supersededBe
	}
}

// RetireTarget atomically unroutes a REMOVED target's active lane and
// registers it as retiring (round 9 #1) — task routing by generation
// keeps working through the whole transition, no not-found window.
func (m *MultiBackend) RetireTarget(name, gen string, be Backend) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.targets, name)
	if gen != "" && be != nil {
		if m.retiring == nil {
			m.retiring = map[string]map[string]Backend{}
		}
		if m.retiring[name] == nil {
			m.retiring[name] = map[string]Backend{}
		}
		m.retiring[name][gen] = be
	}
}

// RemoveRetiringLane unregisters a retired lane (its count hit zero).
func (m *MultiBackend) RemoveRetiringLane(name, generation string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g := m.retiring[name]; g != nil {
		delete(g, generation)
		if len(g) == 0 {
			delete(m.retiring, name)
		}
	}
}

// CompleteRetiringLane atomically moves a settled generation out of live
// routing and into artifact-only routing.
func (m *MultiBackend) CompleteRetiringLane(name, generation string, be Backend) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lanes := m.retiring[name]; lanes != nil {
		delete(lanes, generation)
		if len(lanes) == 0 {
			delete(m.retiring, name)
		}
	}
	if m.historical == nil {
		m.historical = map[string]map[string]Backend{}
	}
	if m.historical[name] == nil {
		m.historical[name] = map[string]Backend{}
	}
	m.historical[name][generation] = be
}

// SetHistoricalLane restores an artifact-only lane from a persisted target
// generation snapshot during daemon startup.
func (m *MultiBackend) SetHistoricalLane(name, generation string, be Backend) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.historical == nil {
		m.historical = map[string]map[string]Backend{}
	}
	if m.historical[name] == nil {
		m.historical[name] = map[string]Backend{}
	}
	m.historical[name][generation] = be
}

// RemoveHistoricalLane forgets an artifact-only lane, returning it so the
// daemon can release its filesystem. Used when that exact generation becomes
// active again and the active lane is once more the correct endpoint.
func (m *MultiBackend) RemoveHistoricalLane(name, generation string) Backend {
	m.mu.Lock()
	defer m.mu.Unlock()
	lanes := m.historical[name]
	be := lanes[generation]
	delete(lanes, generation)
	if len(lanes) == 0 {
		delete(m.historical, name)
	}
	return be
}

// retiringLane looks up a retiring lane for (target, generation).
func (m *MultiBackend) retiringLane(name, generation string) (Backend, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	be, ok := m.retiring[name][generation]
	return be, ok
}

func (m *MultiBackend) historicalLane(name, generation string) (Backend, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	be, ok := m.historical[name][generation]
	return be, ok
}

// TargetGenerations returns the recorded generations (retiring first,
// then recently done) with live unfinished counts — the archive view.
func (m *MultiBackend) TargetGenerations(ctx context.Context) ([]TargetGenerationView, error) {
	rows, err := m.store.ListAllGenerations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TargetGenerationView, 0, len(rows))
	for _, g := range rows {
		v := TargetGenerationView{
			Target: g.Target, Generation: g.Generation, Reason: g.Reason,
			RetiredAt: g.RetiredAt, DoneAt: g.DoneAt,
		}
		if g.DoneAt == nil {
			if n, cerr := m.store.CountUnfinishedGenerationTasks(ctx, g.Target, g.Generation); cerr == nil {
				v.Unfinished = n
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// ActiveLane exposes the CURRENT active lane of a target (RQ-75
// forwarding): retiring lanes resolve their successor per handoff, so
// chained rotations always reach the newest generation.
func (m *MultiBackend) ActiveLane(name string) (Backend, bool) {
	return m.get(name)
}

// SetTarget adds or replaces a lane at runtime (RQ-75), wiring the shared
// project registry exactly as assembly does. The caller owns the OLD
// backend's shutdown (replace first, then close — no routing gap).
func (m *MultiBackend) SetTarget(name string, be Backend) {
	if sq, ok := be.(interface{ setProjectRegistry(*project.Registry) }); ok {
		sq.setProjectRegistry(m.registry)
	}
	m.mu.Lock()
	m.targets[name] = be
	m.mu.Unlock()
}

// RemoveTarget drops a lane from routing. The caller closes the backend
// AFTER removal so no new request can reach a closing lane.
func (m *MultiBackend) RemoveTarget(name string) {
	m.mu.Lock()
	delete(m.targets, name)
	m.mu.Unlock()
}

// SetDefaultTarget changes the routing default at runtime. Unknown names
// are rejected — a default that routes nowhere is worse than a stale one.
func (m *MultiBackend) SetDefaultTarget(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name == "" {
		if len(m.targets) != 0 {
			return fmt.Errorf("cannot clear default target while %d target lane(s) are active", len(m.targets))
		}
		m.defaultTarget = ""
		return nil
	}
	if _, ok := m.targets[name]; !ok {
		return fmt.Errorf("default target %q not in targets map", name)
	}
	m.defaultTarget = name
	return nil
}

// NewMultiBackend creates a routing backend. The exact (empty targets,
// empty default) pair is the supported unconfigured state; otherwise targets
// must contain the defaultTarget key.
func NewMultiBackend(targets map[string]Backend, st *store.Store, defaultTarget string) (*MultiBackend, error) {
	if len(targets) == 0 && defaultTarget != "" {
		return nil, fmt.Errorf("default target %q set but no targets are configured", defaultTarget)
	}
	if len(targets) > 0 {
		if _, ok := targets[defaultTarget]; !ok {
			return nil, fmt.Errorf("default target %q not in targets map", defaultTarget)
		}
	}
	m := &MultiBackend{
		targets:       targets,
		retiring:      map[string]map[string]Backend{},
		historical:    map[string]map[string]Backend{},
		store:         st,
		defaultTarget: defaultTarget,
	}
	// ONE routed project registry shared by every lane (RQ-65): a
	// project's yaml lives on its home target's filesystem, and only the
	// multi-backend can route across targets. Lanes embed storeQueries;
	// swapping their reg pointer gives every project code path — CRUD,
	// self-healing sync, rename — the same router.
	reg := project.NewRegistry(st.DB()).WithFSRouter(func(target string) rfs.FS {
		if target == "" {
			return nil // local machine
		}
		fsys, err := m.TargetFS(target)
		if err != nil {
			return nil // unknown target: fall back to local (fault-tolerant read path)
		}
		return fsys
	})
	m.registry = reg
	for _, be := range m.targets {
		if sq, ok := be.(interface{ setProjectRegistry(*project.Registry) }); ok {
			sq.setProjectRegistry(reg)
		}
	}
	return m, nil
}

// ── Routing helpers ────────────────────────────────────────────────────────

// resolve returns the backend for the named target. Empty falls back to default.
func (m *MultiBackend) resolve(target string) (Backend, error) {
	m.mu.RLock()
	if target == "" {
		target = m.defaultTarget
	}
	if target == "" && len(m.targets) == 0 {
		m.mu.RUnlock()
		return nil, noTargetConfiguredError()
	}
	be, ok := m.targets[target]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown target %q", target)
	}
	return be, nil
}

// resolveTask looks up the task's target column and returns the OWNING
// backend (RQ-75): a task stamped with a generation that has a live
// retiring lane routes there — only the old endpoint/templates can
// correctly kill/probe/read it. Settled generations use a quiesced historical
// lane, never the current endpoint. Legacy rows use the active lane.
func (m *MultiBackend) resolveTask(ctx context.Context, taskID string) (Backend, error) {
	t, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("task %q: %w", taskID, ErrNotFound)
	}
	return m.resolveTaskRow(t)
}

func (m *MultiBackend) resolveTaskRow(t *store.TaskRow) (Backend, error) {
	if t.TargetGeneration != "" {
		if be, ok := m.retiringLane(t.Target, t.TargetGeneration); ok {
			return be, nil
		}
		if active, ok := m.get(t.Target); ok {
			if generation, hasGeneration := active.(interface{ Generation() string }); !hasGeneration || generation.Generation() == t.TargetGeneration {
				return active, nil
			}
		}
		if be, ok := m.historicalLane(t.Target, t.TargetGeneration); ok {
			return be, nil
		}
		return nil, fmt.Errorf("task %q belongs to unavailable target generation %q", t.ID, t.TargetGeneration)
	}
	return m.resolve(t.Target)
}

type artifactTaskGroup struct {
	fs    rfs.FS
	tasks []store.TaskRow
}

// jobArtifactGroups partitions a job by its durable target generation and
// resolves each partition to that generation's exact filesystem. Group order
// follows the first task occurrence so responses stay deterministic.
func (m *MultiBackend) jobArtifactGroups(ctx context.Context, jobID string) (*store.JobRow, []store.TaskRow, []artifactTaskGroup, error) {
	j, err := m.store.GetJob(ctx, jobID)
	if err != nil {
		return nil, nil, nil, err
	}
	if j == nil {
		return nil, nil, nil, fmt.Errorf("job %q: %w", jobID, ErrNotFound)
	}
	tasks, err := m.store.ListTasks(ctx, store.TaskFilter{JobID: jobID})
	if err != nil {
		return nil, nil, nil, err
	}
	groupIndex := make(map[string]int)
	groups := make([]artifactTaskGroup, 0)
	for i := range tasks {
		be, err := m.resolveTaskRow(&tasks[i])
		if err != nil {
			return nil, nil, nil, err
		}
		fsys := rfs.FS(rfs.NewLocalFS())
		if fsp, ok := be.(interface{ FS() rfs.FS }); ok {
			fsys = fsp.FS()
		}
		if active, ok := be.(interface{ touchActivity() }); ok {
			active.touchActivity()
		}
		key := tasks[i].Target + "\x00" + tasks[i].TargetGeneration
		idx, ok := groupIndex[key]
		if !ok {
			idx = len(groups)
			groupIndex[key] = idx
			groups = append(groups, artifactTaskGroup{fs: fsys})
		}
		groups[idx].tasks = append(groups[idx].tasks, tasks[i])
	}
	return j, tasks, groups, nil
}

// storedJob reads the local snapshot even when its execution lane is unavailable.
func (m *MultiBackend) storedJob(ctx context.Context, jobID string) (*store.JobRow, error) {
	j, err := m.store.GetJob(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if j == nil {
		return nil, fmt.Errorf("job %q: %w", jobID, ErrNotFound)
	}

	return j, nil
}

// jobOwningLanes resolves the live lane generations that actually own tasks
// for a job. Generation stamps are authoritative: a live retiring generation
// owns its exact rows; active owns its own, legacy, and orphan-generation rows.
// primary is the active lane when present (the renderer for whole-job views),
// otherwise the first live owner of a removed target.
func (m *MultiBackend) jobOwningLanes(ctx context.Context, jobID string) (primary Backend, owners []Backend, err error) {
	j, err := m.store.GetJob(ctx, jobID)
	if err != nil {
		return nil, nil, err
	}
	if j == nil {
		return nil, nil, fmt.Errorf("job %q: %w", jobID, ErrNotFound)
	}
	tasks, err := m.store.ListTasks(ctx, store.TaskFilter{JobID: jobID})
	if err != nil {
		return nil, nil, fmt.Errorf("list job %s generations: %w", jobID, err)
	}

	// An experiment's submissions may run on different targets: every
	// target its tasks name owns them (the job's first target always).
	targets := []string{j.Target}
	seen := map[string]bool{j.Target: true}
	for _, t := range tasks {
		if t.Target != "" && !seen[t.Target] {
			seen[t.Target] = true
			targets = append(targets, t.Target)
		}
	}
	for _, target := range targets {
		var mine []store.TaskRow
		for _, t := range tasks {
			if t.Target == target || t.Target == "" && target == j.Target {
				mine = append(mine, t)
			}
		}
		lanePrimary, laneOwners := m.targetOwners(target, mine)
		if primary == nil {
			primary = lanePrimary
		}
		owners = append(owners, laneOwners...)
	}
	if primary == nil && len(owners) > 0 {
		primary = owners[0]
	}
	if primary == nil {
		return nil, nil, fmt.Errorf("unknown target %q", j.Target)
	}
	return primary, owners, nil
}

// targetOwners: the lanes of one target that own some of tasks — the active
// lane, and any retiring generation still tracking one.
func (m *MultiBackend) targetOwners(target string, tasks []store.TaskRow) (primary Backend, owners []Backend) {
	m.mu.RLock()
	active := m.targets[target]
	retiring := make(map[string]Backend, len(m.retiring[target]))
	for generation, be := range m.retiring[target] {
		retiring[generation] = be
	}
	m.mu.RUnlock()

	activeOwns := false
	ownedRetiring := make(map[string]struct{})
	for i := range tasks {
		if _, ok := retiring[tasks[i].TargetGeneration]; ok && tasks[i].TargetGeneration != "" {
			ownedRetiring[tasks[i].TargetGeneration] = struct{}{}
			continue
		}
		if active != nil {
			activeOwns = true
		}
	}
	if activeOwns {
		owners = append(owners, active)
	}
	generations := make([]string, 0, len(ownedRetiring))
	for generation := range ownedRetiring {
		generations = append(generations, generation)
	}
	sort.Strings(generations)
	for _, generation := range generations {
		be := retiring[generation]
		if be != nil && be != active {
			owners = append(owners, be)
		}
	}
	if len(owners) == 0 && active != nil {
		// Preserve the zero-task job behavior without letting an empty active
		// scope mask a real retiring owner (which would already be in owners).
		owners = append(owners, active)
	}
	primary = active
	if primary == nil && len(owners) > 0 {
		primary = owners[0]
	}
	return primary, owners
}

// refreshJobGenerations reconciles every live lane generation that owns at
// least one task for the job. Errors are joined so an empty/successful active
// scope can never hide a retiring generation's failure.
func (m *MultiBackend) refreshJobGenerations(ctx context.Context, jobID string) (Backend, error) {
	primary, owners, err := m.jobOwningLanes(ctx, jobID)
	if err != nil {
		return nil, err
	}
	var refreshErrs []error
	for _, be := range owners {
		if err := be.RefreshJob(ctx, jobID); err != nil {
			refreshErrs = append(refreshErrs, err)
		}
	}
	return primary, errors.Join(refreshErrs...)
}

// defaultBackend returns the default target's backend. During a config
// reconcile the default can be momentarily unrouted: the removed loop
// unroutes the old default BEFORE the added loop builds (and
// SetDefaultTarget installs) its replacement, and an SSH lane build
// keeps that window open for seconds. Readers must degrade, never
// panic (Codex RQ2-4 F3): fall back to any live lane (deterministic:
// smallest name); nil only when no lane exists at all — callers below
// turn that into an error or a zero value.
func (m *MultiBackend) defaultBackend() Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if be, ok := m.targets[m.defaultTarget]; ok {
		return be
	}
	fallback := ""
	for name := range m.targets {
		if fallback == "" || name < fallback {
			fallback = name
		}
	}
	if fallback == "" {
		return nil
	}
	return m.targets[fallback]
}

// defaultBackendE is defaultBackend for error-returning paths: the
// reconcile gap becomes a retryable error instead of a nil-call panic.
func (m *MultiBackend) defaultBackendE() (Backend, error) {
	if be := m.defaultBackend(); be != nil {
		return be, nil
	}
	m.mu.RLock()
	unconfigured := len(m.targets) == 0 && m.defaultTarget == ""
	m.mu.RUnlock()
	if unconfigured {
		return nil, noTargetConfiguredError()
	}
	return nil, fmt.Errorf("no target lane available (config reconcile in progress) — retry")
}

func noTargetConfiguredError() error {
	return fmt.Errorf("%w; add one with `runq target add <name> ...`", ErrNoTargetConfigured)
}

// ── Backend interface ──────────────────────────────────────────────────────

// Capabilities returns the default target's capabilities. Individual target
// capabilities can differ; consumers that care should query per-target.
// In the reconcile gap (no lane routable) it reports zero capabilities —
// a briefly degraded UI beats a 500 for a config that DID apply.
func (m *MultiBackend) Capabilities() Capabilities {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	if be := m.defaultBackend(); be != nil {
		return be.Capabilities()
	}
	return Capabilities{}
}

// PerTargetCapabilities exposes each target's own capability set — the
// dashboard gates per-job UI (retry, live log, poll cadence) by the job's
// target through this map.
func (m *MultiBackend) PerTargetCapabilities() map[string]Capabilities {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	targets := m.snapshot()
	out := make(map[string]Capabilities, len(targets))
	for name, be := range targets {
		out[name] = be.Capabilities()
	}
	return out
}

// DefaultTargetName exposes the routing default for config responses.
func (m *MultiBackend) DefaultTargetName() string { return m.defaultName() }

// ReconcileAll fans the dashboard's activity-gated background reconcile out
// to every target that supports it (remote lanes). Local targets are push
// model — nothing to reconcile. This is what keeps a WATCHED dashboard
// fresher (30s cadence) than the lanes' own 25min alignment loops.
func (m *MultiBackend) ReconcileAll(ctx context.Context) error {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	var firstErr error
	for _, be := range m.liveLanes("") {
		if r, ok := be.(interface{ ReconcileAll(context.Context) error }); ok {
			if err := r.ReconcileAll(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (m *MultiBackend) RefreshJob(ctx context.Context, jobID string) error {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	_, err := m.refreshJobGenerations(ctx, jobID)
	return err
}

func (m *MultiBackend) renderJobRows(ctx context.Context, jobs []store.JobRow) ([]JobSummary, error) {
	if len(jobs) == 0 {
		return []JobSummary{}, nil
	}
	jobIDs := make([]string, len(jobs))
	for i := range jobs {
		jobIDs[i] = jobs[i].ID
	}
	tasks, err := m.store.ListTasksForJobs(ctx, jobIDs)
	if err != nil {
		return nil, err
	}
	byJob := make(map[string][]store.TaskRow, len(jobs))
	for i := range tasks {
		byJob[tasks[i].JobID] = append(byJob[tasks[i].JobID], tasks[i])
	}
	out := make([]JobSummary, 0, len(jobs))
	for i := range jobs {
		summary := BuildJobSummary(jobs[i], byJob[jobs[i].ID])
		out = append(out, summary)
	}
	return out, nil
}

// ListJobs renders the durable store, not the current lane registry. A target
// may be removed and every one of its generations may be settled, but its job
// history remains user data and must stay visible. Observation belongs to background workers.
func (m *MultiBackend) ListJobs(ctx context.Context, projectScope string) ([]JobSummary, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	rows, err := m.store.ListJobsVisible(ctx, projectScope, "")
	if err != nil {
		return nil, err
	}
	jobs, err := m.renderJobRows(ctx, rows)
	if err != nil {
		return nil, err
	}

	return jobs, nil
}

// ListJobsForTarget has the same durable-history contract as ListJobs. An
// absent active lane is not an absent target history.
func (m *MultiBackend) ListJobsForTarget(ctx context.Context, target, projectScope string) ([]JobSummary, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	rows, err := m.store.ListJobsVisible(ctx, projectScope, target)
	if err != nil {
		return nil, err
	}
	jobs, err := m.renderJobRows(ctx, rows)
	if err != nil {
		return nil, err
	}

	return jobs, nil
}

// ListArchivedJobsForTarget also survives target removal.
func (m *MultiBackend) ListArchivedJobsForTarget(ctx context.Context, target string) ([]JobSummary, error) {
	rows, err := m.store.ListJobsArchived(ctx, "", target)
	if err != nil {
		return nil, err
	}
	return m.renderJobRows(ctx, rows)
}

func (m *MultiBackend) GetJob(ctx context.Context, jobID string) (*JobDetail, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	j, err := m.store.ResolveJobRef(ctx, jobID) // id or unique prefix
	if err != nil {
		var ambiguous *store.AmbiguousRefError
		if errors.As(err, &ambiguous) {
			return nil, err
		}
		return nil, fmt.Errorf("job %q: %w", jobID, ErrNotFound)
	}
	jobID = j.ID
	tasks, err := m.store.ListTasks(ctx, store.TaskFilter{JobID: jobID})
	if err != nil {
		return nil, err
	}
	keys, _ := m.store.MetricKeys(ctx, jobID)
	picks, _ := m.store.CellPicks(ctx, jobID)
	detail := BuildJobDetail(*j, tasks, keys, picks)
	for i, row := range tasks {
		version := detail.Tasks[i].Version
		detail.Tasks[i] = m.storedTaskView(ctx, row)
		detail.Tasks[i].Version = version
	}
	fillLegacyRunNames(j.ConfigJSON, detail.Tasks)
	attachLastOutput(ctx, m.store, detail.Tasks)
	if batches, err := m.store.ListJobBatches(ctx, jobID); err == nil {
		for _, b := range batches {
			detail.Batches = append(detail.Batches, JobBatchView{Seq: b.Seq, SubmittedAt: b.SubmittedAt.Unix(), Name: b.Name,
				Description: b.Description, Command: b.Command, Target: b.Target, TaskCount: b.TaskCount, DraftID: b.DraftID})
		}
	}
	if len(detail.Tasks) > 0 {
		detail.Capabilities = detail.Tasks[0].Capabilities
	}
	for _, view := range detail.Tasks {
		if view.Stale {
			detail.Job.Stale = true
		}
	}
	attachStoredMetrics(ctx, m.store, jobID, detail.Tasks)
	attachTaskLinks(ctx, m.store, detail.Tasks)
	if cfg, err := m.registry.GetCached(ctx, j.ProjectName); err == nil && cfg.Wandb != nil {
		detail.Wandb = &WandbInfo{
			Entity:  cfg.Wandb.Entity,
			Project: cfg.Wandb.Project,
			BaseURL: WandbBaseURL(cfg.Wandb.Entity, cfg.Wandb.Project),
		}
	}
	return &detail, nil
}

// attachLastOutput fills last_output_at from the ingest observations.
func attachLastOutput(ctx context.Context, st *store.Store, views []TaskView) {
	ids := make([]string, len(views))
	for i := range views {
		ids[i] = views[i].ID
	}
	at, err := st.LastOutputTimes(ctx, ids)
	if err != nil {
		return
	}
	for i := range views {
		if v, ok := at[views[i].ID]; ok {
			views[i].LastOutputAt = &v
		}
	}
}

func (m *MultiBackend) CompareMetrics(ctx context.Context, jobID, key string, desc bool) ([]CompareRow, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	if _, err := m.storedJob(ctx, jobID); err != nil {
		return nil, err
	}
	return compareRowsFromDB(ctx, m.store, jobID, key, desc)
}

// GPUStatus aggregates GPU visibility across ALL targets (local ∪ remote —
// remote targets report through their gpu_template, e.g. the runq preset's
// `runq gpu --json`; targets without one contribute nothing). Slots are
// stamped with their target name for panel grouping.
func (m *MultiBackend) GPUStatus(ctx context.Context) ([]GPUSlot, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	var all []GPUSlot
	for name, be := range m.snapshot() {
		slots, err := be.GPUStatus(ctx)
		if err != nil {
			continue
		}
		for i := range slots {
			if slots[i].Target == "" {
				slots[i].Target = name
			}
		}
		all = append(all, slots...)
	}
	return all, nil
}

func (m *MultiBackend) GetTask(ctx context.Context, taskID string) (*TaskView, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	if id, err := m.store.ResolveTaskRef(ctx, taskID); err == nil {
		taskID = id // id, unique prefix or <experiment>/<cell>[@vN]
	} else {
		var ambiguous *store.AmbiguousRefError
		if errors.As(err, &ambiguous) || strings.Contains(taskID, "/") {
			return nil, err
		}
	}
	row, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("task %q: %w", taskID, ErrNotFound)
	}
	view := m.storedTaskView(ctx, *row)
	views := []TaskView{view}
	attachStoredMetrics(ctx, m.store, row.JobID, views)
	attachTaskLinks(ctx, m.store, views)
	attachLastOutput(ctx, m.store, views)
	return &views[0], nil
}

func (m *MultiBackend) storedTaskView(ctx context.Context, row store.TaskRow) TaskView {
	view := buildTaskView(row, false)
	applyTaskDetail(&view, row)
	view.Target = row.Target
	if lane, err := m.resolveTask(ctx, row.ID); err == nil {
		caps := lane.Capabilities()
		view.Capabilities = &caps
		if sy, ok := lane.(interface {
			SyncInfo(context.Context) (int64, bool)
		}); ok {
			_, view.Stale = sy.SyncInfo(ctx)
		}
	}
	if j, err := m.store.GetJob(ctx, row.JobID); err == nil && j != nil {
		view.RefreshedAt = BuildJobSummary(*j, nil).RefreshedAt
		view.Stale = !store.IsTerminalJobStatus(j.Status) && (view.Stale || view.Capabilities == nil || view.RefreshedAt == nil || time.Now().Unix()-*view.RefreshedAt > 90)
	}
	return view
}

func (m *MultiBackend) TaskMetrics(ctx context.Context, taskID string, afterTS int64) ([]MetricPoint, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	be, err := m.resolveTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return be.TaskMetrics(ctx, taskID, afterTS)
}

func (m *MultiBackend) MetricKeys(ctx context.Context, jobID string) ([]string, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	j, err := m.store.GetJob(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if j == nil {
		return nil, fmt.Errorf("job %q: %w", jobID, ErrNotFound)
	}
	return m.store.MetricKeys(ctx, jobID)
}

func (m *MultiBackend) TaskMetricBuckets(ctx context.Context, taskID, key string, fromTS, toTS int64, maxBuckets int) ([]workspace.PyramidBucket, string, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	be, err := m.resolveTask(ctx, taskID)
	if err != nil {
		return nil, "", err
	}
	return be.TaskMetricBuckets(ctx, taskID, key, fromTS, toTS, maxBuckets)
}

// ── RQ-44: log access — pure ownership routing ────────────────────────────

func (m *MultiBackend) TaskLogRead(ctx context.Context, taskID string, offset int64, maxLines int) (*LogPage, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	be, err := m.resolveTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return be.TaskLogRead(ctx, taskID, offset, maxLines)
}

// TargetFS resolves the addressed target's filesystem (spec §5.2 fs 组,
// #44): the fs browser and python-envs operate on the TARGET's disk, not
// the daemon's. Lanes without an FS concept fall back to LocalFS.
func (m *MultiBackend) TargetFS(name string) (rfs.FS, error) {
	be, ok := m.get(name)
	if !ok {
		return nil, fmt.Errorf("target %q: %w", name, ErrNotFound)
	}
	if fsp, ok := be.(interface{ FS() rfs.FS }); ok {
		return fsp.FS(), nil
	}
	return rfs.NewLocalFS(), nil
}

// PerTargetHealth collects each lane's passive reachability row (/health,
// D6). Lanes without the concept report reachable with LastChecked = now.
// RecordTargetContact records a daemon-observed reachability proof for one
// target's lane (RQ-74). No-op for unknown targets and lanes without a
// contact record (local).
func (m *MultiBackend) RecordTargetContact(name string) {
	if be, ok := m.get(name); ok {
		if rc, ok := be.(interface{ RecordContactOK() }); ok {
			rc.RecordContactOK()
		}
	}
}

func (m *MultiBackend) PerTargetHealth() []TargetHealth {
	targets := m.snapshot()
	out := make([]TargetHealth, 0, len(targets))
	for name, be := range targets {
		if h, ok := be.(interface{ TargetHealth() TargetHealth }); ok {
			out = append(out, h.TargetHealth())
			continue
		}
		out = append(out, TargetHealth{Name: name, Reachable: true, LastChecked: time.Now().Unix()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ── Ownership lookups (remote-CLI guard, RQ-45) ───────────────────────────
// Store-level point reads: the guard must resolve WHOSE job/task an id
// names before letting a forwarded request act on it. "" = not found.

func (m *MultiBackend) JobTarget(ctx context.Context, jobID string) (string, error) {
	j, err := m.store.GetJob(ctx, jobID)
	if err != nil || j == nil {
		return "", err
	}
	return j.Target, nil
}

func (m *MultiBackend) TaskTarget(ctx context.Context, taskID string) (string, error) {
	t, err := m.store.GetTask(ctx, taskID)
	if err != nil || t == nil {
		return "", err
	}
	return t.Target, nil
}

// ── L4: freshness routing ──────────────────────────────────────────────────

// SyncInfo aggregates lane freshness for envelope stamping. target scoping:
// named target → that lane's row; "" (aggregated list) → the OLDEST
// refreshed_at and OR of stale — a mixed response is only as fresh as its
// weakest ingredient.
func (m *MultiBackend) SyncInfo(ctx context.Context, target string) (refreshedAt int64, stale bool, known bool) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	type syncer interface {
		SyncInfo(context.Context) (int64, bool)
	}
	first := true
	for _, be := range m.liveLanes(target) {
		sy, ok := be.(syncer)
		if !ok {
			continue
		}
		at, st := sy.SyncInfo(ctx)
		if first || at < refreshedAt {
			refreshedAt = at
		}
		first = false
		stale = stale || st
		known = true
	}
	return refreshedAt, stale, known
}

// ForceRefreshTarget refreshes every live generation of the named target.
// The receipt is conservative across lanes: refreshed is true only when all
// lanes refreshed, refreshed_at is the oldest lane timestamp, and retry_after
// is the longest requested delay. Lane errors are joined after every lane has
// been attempted, so one success cannot mask another generation's failure.
func (m *MultiBackend) ForceRefreshTarget(ctx context.Context, target string) (*RefreshReceipt, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	if target == "" {
		target = m.defaultName()
	}
	if target == "" {
		return nil, noTargetConfiguredError()
	}
	lanes := m.liveLanes(target)
	if len(lanes) == 0 {
		return nil, fmt.Errorf("refresh: %w: unknown target %q", ErrNotFound, target)
	}

	receipt := &RefreshReceipt{Refreshed: true}
	var reasons []string
	var refreshErrs []error
	for i, be := range lanes {
		laneReceipt := &RefreshReceipt{RefreshedAt: time.Now().Unix(), Refreshed: true}
		if fr, ok := be.(interface {
			ForceRefresh(context.Context) (*RefreshReceipt, error)
		}); ok {
			var err error
			laneReceipt, err = fr.ForceRefresh(ctx)
			if err != nil {
				refreshErrs = append(refreshErrs, fmt.Errorf("refresh target %q lane %d: %w", target, i+1, err))
			}
			if laneReceipt == nil {
				laneReceipt = &RefreshReceipt{Reason: "refresh returned no receipt"}
			}
		}

		if i == 0 || laneReceipt.RefreshedAt < receipt.RefreshedAt {
			receipt.RefreshedAt = laneReceipt.RefreshedAt
		}
		receipt.Refreshed = receipt.Refreshed && laneReceipt.Refreshed
		if laneReceipt.RetryAfterSeconds > receipt.RetryAfterSeconds {
			receipt.RetryAfterSeconds = laneReceipt.RetryAfterSeconds
		}
		if laneReceipt.Reason != "" {
			reasons = append(reasons, laneReceipt.Reason)
		}
	}
	if len(refreshErrs) > 0 {
		receipt.Refreshed = false
	}
	receipt.Reason = strings.Join(reasons, "; ")
	return receipt, errors.Join(refreshErrs...)
}

// ListTasks — flat table over the shared store (all lanes write here;
// tasks.target scopes). No routing needed.
func (m *MultiBackend) ListTasks(ctx context.Context, opts TaskListOptions) ([]TaskView, int, error) {
	views, total, err := listTasksFromStore(ctx, m.store, opts)
	if err != nil {
		return nil, 0, err
	}
	jobs := map[string]bool{}
	for i := range views {
		row, err := m.store.GetTask(ctx, views[i].ID)
		if err != nil {
			return nil, 0, err
		}
		if row == nil {
			continue
		}
		views[i] = m.storedTaskView(ctx, *row)
		jobs[row.JobID] = true
	}
	for id := range jobs {
		attachStoredMetrics(ctx, m.store, id, views)
	}
	return views, total, nil
}

func (m *MultiBackend) TaskLogTail(ctx context.Context, taskID string, maxLines int) (*LogPage, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	be, err := m.resolveTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return be.TaskLogTail(ctx, taskID, maxLines)
}

func (m *MultiBackend) TaskLogPage(ctx context.Context, taskID string, req logfile.PageRequest) (*LogPage, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	be, err := m.resolveTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return be.TaskLogPage(ctx, taskID, req)
}

func (m *MultiBackend) TaskLogFollow(ctx context.Context, taskID string, offset int64) (LogFollower, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	be, err := m.resolveTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return be.TaskLogFollow(ctx, taskID, offset)
}

func (m *MultiBackend) JobLogSearch(ctx context.Context, jobID, query string) ([]LogMatch, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	_, _, groups, err := m.jobArtifactGroups(ctx, jobID)
	if err != nil {
		return nil, err
	}
	out := make([]LogMatch, 0)
	for _, group := range groups {
		matches, err := jobLogSearchRowsViaExec(ctx, group.fs, group.tasks, query, jobLogSearchMaxMatches-len(out))
		if err != nil {
			return nil, err
		}
		out = append(out, matches...)
		if len(out) >= jobLogSearchMaxMatches {
			break
		}
	}
	return out, nil
}

// TaskLogGrep greps one task's log on the target that owns it and returns
// the last tail matches (see taskLogGrepViaExec). Optional capability, not
// part of Backend: only the router knows which lane/generation owns a task.
func (m *MultiBackend) TaskLogGrep(ctx context.Context, taskID, pattern string, tail int) ([]LogMatch, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	row, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("task %q: %w", taskID, ErrNotFound)
	}
	be, err := m.resolveTaskRow(row)
	if err != nil {
		return nil, err
	}
	fsys := rfs.FS(rfs.NewLocalFS())
	if fsp, ok := be.(interface{ FS() rfs.FS }); ok {
		fsys = fsp.FS()
	}
	if active, ok := be.(interface{ touchActivity() }); ok {
		active.touchActivity()
	}
	return taskLogGrepViaExec(ctx, fsys, *row, pattern, tail)
}

func (m *MultiBackend) JobActivity(ctx context.Context, jobID string) (*JobActivity, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	j, rows, groups, err := m.jobArtifactGroups(ctx, jobID)
	if err != nil {
		return nil, err
	}
	// A finished job's activity no longer changes (one SSH read ≈ 0.7s):
	// serve it from memory until the job changes (an append reopens it).
	key := ""
	if store.IsTerminalJobStatus(j.Status) && j.FinishedAt != nil {
		key = fmt.Sprintf("%s|%d|%d", j.Status, j.FinishedAt.Unix(), len(rows))
		if hit, ok := m.activityCache.Load(j.ID); ok && hit.(cachedActivity).key == key {
			return hit.(cachedActivity).activity, nil
		}
	}
	byTask := make(map[string]TaskActivity, len(rows))
	for _, group := range groups {
		activity, err := taskActivityRowsViaExec(ctx, group.fs, group.tasks)
		if err != nil {
			return nil, err
		}
		for i := range activity {
			byTask[activity[i].TaskID] = activity[i]
		}
	}
	out := &JobActivity{Tasks: make([]TaskActivity, 0, len(rows))}
	for i := range rows {
		out.Tasks = append(out.Tasks, byTask[rows[i].ID])
	}
	out.JobStart, out.JobEnd = activityWindow(j, rows)
	if key != "" {
		m.activityCacheN.Add(1)
		if m.activityCacheN.Load() > 256 { // plenty for one dashboard; then start over
			m.activityCache.Clear()
			m.activityCacheN.Store(0)
		}
		m.activityCache.Store(j.ID, cachedActivity{key: key, activity: out})
	}
	return out, nil
}

type cachedActivity struct {
	key      string
	activity *JobActivity
}

func (m *MultiBackend) JobResults(ctx context.Context, jobID string) (*JobResults, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	if _, err := m.storedJob(ctx, jobID); err != nil {
		return nil, err
	}
	return jobResultsFromDB(ctx, m.store, jobID)
}

func (m *MultiBackend) KillTask(ctx context.Context, taskID string) error {
	m.controlMu.Lock()
	defer m.controlMu.Unlock()
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	be, err := m.resolveTask(ctx, taskID)
	if err != nil {
		return err
	}
	return be.KillTask(ctx, taskID)
}

func (m *MultiBackend) KillJob(ctx context.Context, jobID string) error {
	m.controlMu.Lock()
	defer m.controlMu.Unlock()
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	j, err := m.store.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	if j == nil {
		return fmt.Errorf("job %q: %w", jobID, ErrNotFound)
	}
	owners := m.liveLanes(j.Target)
	if tasks, err := m.store.ListTasks(ctx, store.TaskFilter{JobID: j.ID}); err == nil {
		seen := map[string]bool{j.Target: true}
		for _, t := range tasks { // later submissions may run on other targets
			if t.Target != "" && !seen[t.Target] {
				seen[t.Target] = true
				owners = append(owners, m.liveLanes(t.Target)...)
			}
		}
	}
	if len(owners) == 0 {
		return fmt.Errorf("unknown target %q", j.Target)
	}
	var killErrs []error
	// A job may span lane generations after a config change (in-flight
	// tasks stay with the retiring lane, pending work uses the active one).
	// Attempt every owner, but do not hide a retiring-lane failure: returning
	// one joined error is the only honest contract for a partially applied
	// whole-job cancellation.
	for i, be := range owners {
		if err := be.KillJob(ctx, jobID); err != nil {
			killErrs = append(killErrs, fmt.Errorf("kill job %s on owner lane %d: %w", jobID, i+1, err))
		}
	}
	return errors.Join(killErrs...)
}

// SubmitJob routes to the target specified in opts.Target, falling back to
// default_target.
func (m *MultiBackend) SubmitJob(ctx context.Context, cfg job.JobConfig, opts SubmitOptions) (string, int, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	explicit, err := m.resolveInto(ctx, &cfg, opts.Target)
	if err != nil {
		return "", 0, err
	}
	projectCfg, target, source, err := m.submissionTarget(ctx, cfg, explicit)
	if err != nil {
		return "", 0, err
	}
	if cfg.Into != "" && opts.Target == "" {
		source = "into"
	}
	be, err := m.resolve(target)
	if err != nil {
		return "", 0, err
	}
	cfg.Target, cfg.TargetSource = target, source
	cfg.EffectiveConfig = projectCfg
	cfg.Project = projectCfg.ProjectName // job.yaml may name the project by id
	if cfg.Into == "" {
		if _, err := m.resolveJobSection(ctx, &cfg); err != nil {
			return "", 0, err
		}
	}
	sourceTarget := projectSourceTarget(projectCfg)
	if target != sourceTarget {
		m.projectSyncMu.Lock()
		_, syncErr := m.syncProjectTo(projectCfg, sourceTarget, target)
		m.projectSyncMu.Unlock()
		if syncErr != nil {
			return "", 0, fmt.Errorf("sync project %q from %q to %q: %w", cfg.Project, sourceTarget, target, syncErr)
		}
	}
	opts.Target = target
	return be.SubmitJob(ctx, cfg, opts)
}

// SyncProject mirrors one project's allow-listed tree. sourceTarget is a
// one-shot truth override; it never changes project.target. Empty destination
// fans out to every other target declared by the project.
func (m *MultiBackend) SyncProject(ctx context.Context, name, sourceTarget, destinationTarget string) ([]project.SyncResult, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	cfg, err := m.registry.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := m.validateProjectTargets(cfg); err != nil {
		return nil, err
	}
	if sourceTarget == "" {
		sourceTarget = projectSourceTarget(cfg)
	}
	if err := m.validateProjectTarget(cfg, sourceTarget); err != nil {
		return nil, fmt.Errorf("sync source: %w", err)
	}

	var destinations []string
	if destinationTarget != "" {
		if err := m.validateProjectTarget(cfg, destinationTarget); err != nil {
			return nil, fmt.Errorf("sync destination: %w", err)
		}
		if destinationTarget != sourceTarget {
			destinations = []string{destinationTarget}
		}
	} else {
		for _, target := range cfg.Targets {
			if target != sourceTarget {
				destinations = append(destinations, target)
			}
		}
	}

	m.projectSyncMu.Lock()
	defer m.projectSyncMu.Unlock()
	results := make([]project.SyncResult, 0, len(destinations))
	for _, destination := range destinations {
		result, err := m.syncProjectTo(cfg, sourceTarget, destination)
		if err != nil {
			return results, fmt.Errorf("sync project %q from %q to %q: %w", name, sourceTarget, destination, err)
		}
		results = append(results, result)
	}
	return results, nil
}

func (m *MultiBackend) syncProjectTo(cfg *project.Config, sourceTarget, destinationTarget string) (project.SyncResult, error) {
	result := project.SyncResult{SourceTarget: sourceTarget, DestinationTarget: destinationTarget}
	source, err := m.TargetFS(sourceTarget)
	if err != nil {
		return result, err
	}
	destination, err := m.TargetFS(destinationTarget)
	if err != nil {
		return result, err
	}
	result.Copied, result.Deleted, err = project.Mirror(cfg.WorkingDir, source, cfg.WorkingDir, destination)
	return result, err
}

func (m *MultiBackend) validateProjectTarget(cfg *project.Config, target string) error {
	if target == "" {
		return fmt.Errorf("project %q has no target", cfg.ProjectName)
	}
	if _, ok := m.get(target); !ok {
		return fmt.Errorf("project %q references unknown target %q", cfg.ProjectName, target)
	}
	if len(cfg.Targets) == 0 {
		sourceTarget := projectSourceTarget(cfg)
		if target != sourceTarget {
			return fmt.Errorf("project %q is pinned to target %q; add %q to project.targets to enable cross-target submit", cfg.ProjectName, sourceTarget, target)
		}
		return nil
	}
	for _, allowed := range cfg.Targets {
		if allowed == target {
			return nil
		}
	}
	return fmt.Errorf("target %q is not declared in project %q targets", target, cfg.ProjectName)
}

func (m *MultiBackend) validateProjectTargets(cfg *project.Config) error {
	if cfg.Target == "" {
		return fmt.Errorf("project.target is required")
	}
	seen := make(map[string]bool, len(cfg.Targets))
	for _, target := range cfg.Targets {
		if target == "" {
			return fmt.Errorf("project targets cannot contain an empty name")
		}
		if seen[target] {
			return fmt.Errorf("project target %q is duplicated", target)
		}
		seen[target] = true
		if _, ok := m.get(target); !ok {
			return fmt.Errorf("project references unknown target %q", target)
		}
	}
	if len(cfg.Targets) > 0 && !seen[cfg.Target] {
		return fmt.Errorf("project source target %q must also appear in targets", cfg.Target)
	}
	_, ok := m.get(cfg.Target)
	if !ok {
		return fmt.Errorf("project references unknown source target %q", cfg.Target)
	}
	return nil
}

func projectSourceTarget(cfg *project.Config) string {
	if cfg.Target == "" {
		return "local"
	}
	return cfg.Target
}

func (m *MultiBackend) submissionTarget(ctx context.Context, cfg job.JobConfig, explicit string) (*project.Config, string, string, error) {
	if len(m.snapshot()) == 0 {
		return nil, "", "", noTargetConfiguredError()
	}
	proj, err := m.registry.Get(ctx, cfg.Project)
	if err != nil {
		return nil, "", "", err
	}
	target, source := proj.Target, "project"
	if cfg.Target != "" {
		target, source = cfg.Target, "job"
	}
	if explicit != "" {
		target, source = explicit, "cli"
	}
	if target == "" {
		return nil, "", "", fmt.Errorf("project.target is required")
	}
	if err := m.validateProjectTargets(proj); err != nil {
		return nil, "", "", fmt.Errorf("invalid submission target: %w", err)
	}
	if err := m.validateProjectTarget(proj, target); err != nil {
		return nil, "", "", fmt.Errorf("invalid submission target: %w", err)
	}
	if _, err := m.resolve(target); err != nil {
		return nil, "", "", err
	}
	return proj, target, source, nil
}

// resolveInto canonicalizes cfg.Into (id or unique prefix) and returns the
// target the submit must use: an append runs on the job's own target (job
// routing, kill and refresh go by it), and defaults its project from the
// job.
func (m *MultiBackend) resolveInto(ctx context.Context, cfg *job.JobConfig, explicit string) (string, error) {
	if err := cfg.CheckAppendFields(); err != nil {
		return "", err
	}
	if cfg.Into == "" {
		return explicit, nil
	}
	j, err := m.store.ResolveJobRef(ctx, cfg.Into)
	if err != nil {
		return "", fmt.Errorf("--into: %w", err)
	}
	if j == nil {
		return "", fmt.Errorf("--into %s: job %w", cfg.Into, ErrNotFound)
	}
	cfg.Into = j.ID
	if strings.TrimSpace(cfg.Project) == "" {
		cfg.Project = j.ProjectName
	} else if name, err := m.registry.Resolve(ctx, cfg.Project); err == nil && name != j.ProjectName {
		return "", fmt.Errorf("--into %s: the job belongs to project %q, not %q", j.ID, j.ProjectName, name)
	}
	// A submission picks its own target (-t, else job.yaml target:); the
	// default is where the experiment started.
	for _, t := range []string{explicit, cfg.Target} {
		if t != "" {
			return t, nil
		}
	}
	return j.Target, nil
}

func (m *MultiBackend) PreviewSubmit(ctx context.Context, cfg job.JobConfig) (PreviewResult, error) {
	return m.PreviewSubmitForTarget(ctx, "", cfg)
}

func (m *MultiBackend) PreviewSubmitForTarget(ctx context.Context, explicit string, cfg job.JobConfig) (PreviewResult, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	given := explicit
	explicit, err := m.resolveInto(ctx, &cfg, explicit)
	if err != nil {
		return PreviewResult{}, err
	}
	proj, target, source, err := m.submissionTarget(ctx, cfg, explicit)
	if err != nil {
		return PreviewResult{}, err
	}
	if cfg.Into != "" && given == "" {
		source = "into"
	}
	be, err := m.resolve(target)
	if err != nil {
		return PreviewResult{}, err
	}
	cfg.Target, cfg.TargetSource = target, source
	cfg.EffectiveConfig = proj
	cfg.Project = proj.ProjectName // job.yaml may name the project by id
	var sectionLine string
	if cfg.Into == "" {
		if sectionLine, err = m.resolveJobSection(ctx, &cfg); err != nil {
			return PreviewResult{}, err
		}
	} else {
		sectionLine = "section: the job's (an append keeps it)"
	}
	out, err := be.PreviewSubmit(ctx, cfg)
	if out.Plan != nil {
		out.Append = out.Plan.Append
	}
	out.Target, out.TargetSource = target, source
	out.Preview = fmt.Sprintf("target: %s (%s)\n%s\n", target, source, sectionLine) + out.Preview
	if target != projectSourceTarget(proj) {
		out.SyncPending = true
		out.Preview = "code sync pending: preview checks current destination files; submit will sync source first\n" + out.Preview
	}
	return out, err
}

func (m *MultiBackend) ListArchivedJobs(ctx context.Context) ([]JobSummary, error) {
	rows, err := m.store.ListJobsArchived(ctx, "", "")
	if err != nil {
		return nil, err
	}
	return m.renderJobRows(ctx, rows)
}

func (m *MultiBackend) ArchiveJob(ctx context.Context, jobID string) error {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	if _, err := m.storedJob(ctx, jobID); err != nil {
		return err
	}
	return m.store.ArchiveJob(ctx, jobID)
}

func (m *MultiBackend) UnarchiveJob(ctx context.Context, jobID string) error {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	return m.store.UnarchiveJob(ctx, jobID)
}

func (m *MultiBackend) ArchiveProject(ctx context.Context, name string) error {
	return m.registry.Archive(ctx, name)
}

func (m *MultiBackend) UnarchiveProject(ctx context.Context, name string) error {
	return m.registry.Unarchive(ctx, name)
}

type orphanDetector interface{ DetectOrphansNow(context.Context) error }

func (m *MultiBackend) Clean(ctx context.Context, opts CleanOptions) (*CleanResult, error) {
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	// Orphan cleaning: refresh each target's orphan marks first, each through
	// its own FS. Detection failures are non-fatal — an unobservable target
	// simply contributes no NEW marks (its guardrails also prevent false
	// ones), and must not block cleaning the others.
	if opts.Orphan {
		for name, be := range m.snapshot() {
			if opts.Target != "" && name != opts.Target {
				continue
			}
			if det, ok := be.(orphanDetector); ok {
				_ = det.DetectOrphansNow(ctx)
			}
		}
	}
	// Run clean HERE with per-target FS routing — remote tasks' artifacts
	// live on their target's filesystem, and only the Multi knows every
	// lane. (Delegating to the default lane would delete remote dirs with
	// a local os.RemoveAll: silent no-op, artifacts left behind.)
	return PerformClean(ctx, m.store, func(task store.TaskRow) (rfs.FS, error) {
		be, err := m.resolveTaskRow(&task)
		if err != nil {
			return nil, err
		}
		if fsp, ok := be.(interface{ FS() rfs.FS }); ok {
			return fsp.FS(), nil
		}
		return rfs.NewLocalFS(), nil
	}, opts)
}

// ── Project operations (global, not per-target) ────────────────────────────

func (m *MultiBackend) GetProject(ctx context.Context, name string) (*project.Config, error) {
	return m.registry.Get(ctx, name)
}

func (m *MultiBackend) ListProjects(ctx context.Context) ([]ProjectSummary, error) {
	configs, err := m.registry.List(ctx)
	if err != nil {
		return nil, err
	}
	return (&storeQueries{store: m.store, reg: m.registry}).configsToSummaries(ctx, configs)
}

func (m *MultiBackend) MatchProjects(ctx context.Context, dir string) ([]ProjectSummary, error) {
	be, err := m.defaultBackendE()
	if err != nil {
		return nil, err
	}
	return be.MatchProjects(ctx, dir)
}

func (m *MultiBackend) CreateProject(ctx context.Context, cfg project.Config) error {
	// Fill the home target explicitly at birth (empty = wherever the user
	// is aimed by default). A project registered "against tsubame" must
	// SAY tsubame — implicit defaults rot when default_target changes.
	if cfg.Target == "" {
		return fmt.Errorf("project.target is required; set it in project.yaml")
	}
	if len(cfg.Targets) == 0 {
		cfg.Targets = []string{cfg.Target}
	}
	if err := m.validateProjectTargets(&cfg); err != nil {
		return err
	}
	return m.registry.Add(ctx, cfg)
}

func (m *MultiBackend) UpdateProject(ctx context.Context, cfg project.Config) error {
	if err := m.validateProjectTargets(&cfg); err != nil {
		return err
	}
	return m.registry.Update(ctx, cfg)
}

func (m *MultiBackend) RenameProject(ctx context.Context, oldName, newName string) error {
	before, err := m.registry.GetCached(ctx, oldName)
	if err != nil {
		return err
	}
	if err := m.registry.Rename(ctx, before.ProjectName, newName); err != nil {
		return err
	}
	// <…>/<pj…>/<jb…>/<tk…>: refresh the marker of every project directory
	// that exists (pre-rc6 layouts have none). Best effort — decoration.
	refs, _ := m.store.TaskDirs(ctx, newName, "")
	m.rewriteMarkers(ctx, refs, func(taskDir string) string {
		if dir := path.Dir(path.Dir(taskDir)); path.Base(dir) == before.ID {
			return dir
		}
		return ""
	}, newName)
	return nil
}

func (m *MultiBackend) PinVersion(ctx context.Context, jobRef, cell, taskID string) (string, error) {
	j, err := m.store.ResolveJobRef(ctx, jobRef)
	if err != nil {
		return "", err
	}
	return m.store.PinVersion(ctx, j.ID, cell, taskID)
}

func (m *MultiBackend) RenameJob(ctx context.Context, ref, newName string) (string, error) {
	id, err := m.store.RenameJob(ctx, ref, newName)
	if err != nil {
		return "", err
	}
	refs, _ := m.store.TaskDirs(ctx, "", id)
	m.rewriteMarkers(ctx, refs, func(taskDir string) string {
		if dir := path.Dir(taskDir); path.Base(dir) == id {
			return dir
		}
		return ""
	}, strings.TrimSpace(newName))
	return id, nil
}

func (m *MultiBackend) ListSections(ctx context.Context, ref string) ([]store.Section, error) {
	name, err := m.registry.Resolve(ctx, ref)
	if err != nil {
		return nil, err
	}
	return m.store.ListSections(ctx, name)
}

func (m *MultiBackend) CreateSection(ctx context.Context, ref, section string) (*store.Section, error) {
	name, err := m.registry.Resolve(ctx, ref)
	if err != nil {
		return nil, err
	}
	return m.store.CreateSection(ctx, name, section)
}

func (m *MultiBackend) RemoveSection(ctx context.Context, ref, section string) (int, error) {
	name, err := m.registry.Resolve(ctx, ref)
	if err != nil {
		return 0, err
	}
	return m.store.RemoveSection(ctx, name, section)
}

func (m *MultiBackend) AddSectionRule(ctx context.Context, ref, section, kind, pattern string) (*store.SectionRule, error) {
	name, err := m.registry.Resolve(ctx, ref)
	if err != nil {
		return nil, err
	}
	return m.store.AddRule(ctx, name, section, kind, pattern)
}

func (m *MultiBackend) RemoveSectionRule(ctx context.Context, ref string, ruleID int64) error {
	name, err := m.registry.Resolve(ctx, ref)
	if err != nil {
		return err
	}
	return m.store.RemoveRule(ctx, name, ruleID)
}

func (m *MultiBackend) SortSections(ctx context.Context, ref string) (int, error) {
	name, err := m.registry.Resolve(ctx, ref)
	if err != nil {
		return 0, err
	}
	return m.store.SortUnsorted(ctx, name)
}

func (m *MultiBackend) MoveJob(ctx context.Context, jobRef, section string) (string, error) {
	ids, err := m.store.SetJobSection(ctx, section, []string{jobRef})
	if err != nil {
		return "", err
	}
	return ids[0], nil
}

// ExportJob writes a job back in job.yaml form (`tasks:` rows): one row per
// cell, from its selected version. A snapshot, not the job's record (the
// ledger is): command, target and overrides are the latest submission's, and
// a row whose version ran another command says so in a comment. Jobs from
// before rc6 get run names derived from their first row.
func (m *MultiBackend) ExportJob(ctx context.Context, ref string) ([]byte, error) {
	j, err := m.store.ResolveJobRef(ctx, ref)
	if err != nil {
		return nil, err
	}
	tasks, err := m.store.JobTasksInOrder(ctx, j.ID)
	if err != nil {
		return nil, err
	}
	picks, err := m.store.CellPicks(ctx, j.ID)
	if err != nil {
		return nil, err
	}
	var cfg job.JobConfig
	_ = json.Unmarshal([]byte(j.ConfigJSON), &cfg)
	needs := map[string][]string{}
	for _, tr := range cfg.Tasks {
		needs[tr.Run] = tr.NeedsCode
	}
	projCmd := ""
	if proj, err := m.registry.GetCached(ctx, j.ProjectName); err == nil {
		projCmd = proj.CmdTemplate
	}
	effective := func(c string) string {
		if c == "" {
			return projCmd
		}
		return c
	}
	commands := map[int]string{1: effective(cfg.Command)}
	latest := cfg
	if batches, err := m.store.ListJobBatches(ctx, j.ID); err == nil {
		for _, b := range batches {
			var bc job.JobConfig
			_ = json.Unmarshal([]byte(b.ConfigJSON), &bc)
			commands[b.Seq] = effective(bc.Command)
			for _, tr := range bc.Tasks {
				needs[tr.Run] = tr.NeedsCode
			}
			if b.Seq > 1 {
				latest = bc
			}
		}
	}
	rows := make([]job.Row, 0, len(tasks))
	for _, c := range store.GroupCells(tasks, picks) {
		for _, t := range c.Versions {
			if t.ID != c.Selected {
				continue
			}
			var params job.TaskParams
			if err := json.Unmarshal([]byte(t.ParamsJSON), &params); err != nil {
				return nil, fmt.Errorf("task %s params: %w", t.ID, err)
			}
			rows = append(rows, job.Row{Run: t.RunName, NeedsCode: needs[t.RunName], Params: params, Command: commands[t.BatchSeq]})
		}
	}
	// Pre-rc6 tasks have no run names: re-plan the stored config for its
	// names when it still yields the same rows, else derive them.
	if len(rows) > 0 && rows[0].Run == "" {
		if planned, err := job.Rows(&cfg); err == nil && sameParams(planned, rows) {
			for i := range rows {
				rows[i].Run = planned[i].Run
			}
		}
	}
	job.NameRows(rows)
	in := job.ExportInput{
		JobID: j.ID, ExportedAt: time.Now(), Project: j.ProjectName,
		Name: j.Name, Description: j.Description, Section: j.Section,
		Overrides: latest.Overrides, Baseline: cfg.Baseline, Rows: rows,
		Command: effective(latest.Command),
	}
	if proj, err := m.registry.GetCached(ctx, j.ProjectName); err == nil {
		for _, d := range proj.Params {
			in.ParamOrder = append(in.ParamOrder, d.Name)
		}
		if j.Target != proj.Target {
			in.Target = j.Target
		}
		in.DefaultCommand = proj.CmdTemplate
	} else {
		in.Target = j.Target
	}
	return job.ExportYAML(in)
}

// resolveJobSection fills cfg.SectionID: an explicit section must exist;
// otherwise the project's rules decide, and no match means unsorted. The
// returned line says which, for the preview.
func (m *MultiBackend) resolveJobSection(ctx context.Context, cfg *job.JobConfig) (string, error) {
	if strings.TrimSpace(cfg.Section) != "" {
		id, err := m.store.ResolveSection(ctx, cfg.Project, cfg.Section)
		if err != nil {
			return "", err
		}
		cfg.SectionID = id
		return fmt.Sprintf("section: %s (job.yaml / -s)", strings.TrimSpace(cfg.Section)), nil
	}
	named := *cfg
	named.NormalizeLegacy()
	id, rule, err := m.store.MatchSection(ctx, cfg.Project, named.Name, cfg.SourcePath)
	if err != nil {
		return "", err
	}
	cfg.SectionID = id
	if rule == nil {
		return "section: unsorted (no rule matched; set one with -s or `section:`)", nil
	}
	return fmt.Sprintf("section: %s (rule #%d: %s %s)", rule.Section, rule.ID, rule.Kind, rule.Pattern), nil
}

// rewriteMarkers rewrites the 0-<name> marker in each distinct directory
// dirOf derives from a task dir ("" = no id-named directory to mark).
func (m *MultiBackend) rewriteMarkers(ctx context.Context, refs []store.TaskDirRef, dirOf func(string) string, name string) {
	done := map[string]bool{}
	for _, r := range refs {
		dir := dirOf(r.TaskDir)
		if dir == "" || done[r.Target+"\x00"+dir] {
			continue
		}
		done[r.Target+"\x00"+dir] = true
		fsys, err := m.TargetFS(r.Target)
		if err != nil {
			continue
		}
		_ = workspace.WriteMarker(ctx, fsys, dir, name)
	}
}

// compile-time interface check
var (
	_ Backend       = (*MultiBackend)(nil)
	_ Sectioner     = (*MultiBackend)(nil)
	_ JobExporter   = (*MultiBackend)(nil)
	_ JobRenamer    = (*MultiBackend)(nil)
	_ VersionPinner = (*MultiBackend)(nil)
)

func sameParams(a, b []job.Row) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i].Params) != len(b[i].Params) {
			return false
		}
		for k, v := range a[i].Params {
			w, ok := b[i].Params[k]
			if !ok || !job.SameValue(v, w) {
				return false
			}
		}
	}
	return true
}
