package project

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/utils"
)

// Registry manages the set of registered projects, backed by SQLite.
type Registry struct {
	db    *sql.DB
	fsFor func(target string) rfs.FS // nil = everything local
}

// NewRegistry creates a Registry using the given database connection.
// WithFSRouter injects the target→filesystem router (RQ-65): a project's
// yaml lives on its home target's filesystem, and every read/write of it
// must go there. nil router (or nil result) = local filesystem.
func (r *Registry) WithFSRouter(fn func(target string) rfs.FS) *Registry {
	r.fsFor = fn
	return r
}

func NewRegistry(db *sql.DB) *Registry {
	return &Registry{db: db}
}

// Add registers a new project. The registration is the only truth runq
// reads (rc6 explicit upsert): project.yaml is never written or re-read.
// Returns an error if a project with the same name already exists.
func (r *Registry) Add(ctx context.Context, cfg Config) error {
	if err := ValidateName(cfg.ProjectName); err != nil {
		return err
	}
	var existing int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE name = ?`, cfg.ProjectName).Scan(&existing)
	if err == nil {
		return fmt.Errorf("project %q already exists", cfg.ProjectName)
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("query project %q: %w", cfg.ProjectName, err)
	}

	if cfg.ID == "" {
		cfg.ID = utils.GenerateProjectID()
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal project config: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO projects (name, id, config_json) VALUES (?, ?, ?)`,
		cfg.ProjectName, cfg.ID, string(data),
	)
	if err != nil {
		// SQLite UNIQUE constraint violation contains "UNIQUE constraint failed"
		return fmt.Errorf("project %q already exists or database error: %w", cfg.ProjectName, err)
	}
	return nil
}

// Get returns a project by reference (name, id, or unique id prefix).
// Returns a descriptive error if the project does not exist.
func (r *Registry) Get(ctx context.Context, ref string) (*Config, error) {
	cfg, err := r.GetCached(ctx, ref)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// GetCached reads the controller's persisted snapshot without filesystem I/O.
// ref is a name, an id, or a unique id prefix.
func (r *Registry) GetCached(ctx context.Context, ref string) (*Config, error) {
	name, err := r.Resolve(ctx, ref)
	if err != nil {
		return nil, err
	}
	var raw string
	var id sql.NullString
	err = r.db.QueryRowContext(ctx,
		`SELECT config_json, id FROM projects WHERE name = ?`, name,
	).Scan(&raw, &id)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("project %q not found", ref)
	}
	if err != nil {
		return nil, fmt.Errorf("query project %q: %w", name, err)
	}
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal project %q: %w", name, err)
	}
	cfg.ProjectName, cfg.ID = name, id.String
	return &cfg, nil
}

// Resolve maps a project reference to its current name: an exact name wins,
// then an exact id, then a unique id prefix. Ambiguity and misses list the
// candidates so the error is the fix.
func (r *Registry) Resolve(ctx context.Context, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("project reference is empty")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT name, COALESCE(id, '') FROM projects ORDER BY name`)
	if err != nil {
		return "", fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	type entry struct{ name, id string }
	var all []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.name, &e.id); err != nil {
			return "", err
		}
		if e.name == ref || e.id == ref {
			return e.name, nil
		}
		all = append(all, e)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if strings.HasPrefix(ref, "pj") {
		var matches []entry
		for _, e := range all {
			if strings.HasPrefix(e.id, ref) {
				matches = append(matches, e)
			}
		}
		if len(matches) == 1 {
			return matches[0].name, nil
		}
		if len(matches) > 1 {
			var c []string
			for _, e := range matches {
				c = append(c, e.id+" ("+e.name+")")
			}
			return "", fmt.Errorf("project %q is ambiguous: %s", ref, strings.Join(c, ", "))
		}
	}
	var known []string
	for _, e := range all {
		known = append(known, e.id+" ("+e.name+")")
	}
	if len(known) == 0 {
		return "", fmt.Errorf("project %q not found (no projects registered; run: runq project project.yaml)", ref)
	}
	return "", fmt.Errorf("project %q not found; known: %s", ref, strings.Join(known, ", "))
}

// List returns all registered projects, ordered by name.
func (r *Registry) List(ctx context.Context) ([]Config, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT name, COALESCE(id, ''), config_json FROM projects ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var configs []Config
	for rows.Next() {
		var name, id, raw string
		if err := rows.Scan(&name, &id, &raw); err != nil {
			return nil, fmt.Errorf("scan project row: %w", err)
		}
		var cfg Config
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal project: %w", err)
		}
		cfg.ProjectName, cfg.ID = name, id
		configs = append(configs, cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	return configs, nil
}

// Update replaces an existing project's registered config.
// Returns an error if the project does not exist.
func (r *Registry) Update(ctx context.Context, cfg Config) error {
	var existing int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE name = ?`, cfg.ProjectName).Scan(&existing)
	if err == sql.ErrNoRows {
		return fmt.Errorf("project %q not found", cfg.ProjectName)
	}
	if err != nil {
		return fmt.Errorf("query project %q: %w", cfg.ProjectName, err)
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal project config: %w", err)
	}
	result, err := r.db.ExecContext(ctx,
		`UPDATE projects SET config_json = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ?`,
		string(data), cfg.ProjectName,
	)
	if err != nil {
		return fmt.Errorf("update project %q: %w", cfg.ProjectName, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("project %q not found", cfg.ProjectName)
	}
	return nil
}

// Match returns all projects whose WorkingDir is equal to or a parent of dir.
// This is used to auto-detect which project(s) a script belongs to.
func (r *Registry) Match(ctx context.Context, dir string) ([]Config, error) {
	all, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	dir = filepath.Clean(dir)
	var matched []Config
	for _, cfg := range all {
		wd := filepath.Clean(cfg.WorkingDir)
		if wd == "" {
			continue
		}
		if dir == wd || strings.HasPrefix(dir, wd+string(filepath.Separator)) {
			matched = append(matched, cfg)
		}
	}
	return matched, nil
}

// Rename atomically renames a project; ref is its name, id or id prefix.
// The id never changes. Every table that carries the name denormalized is
// updated in the same transaction. project.yaml is not touched: the CLI
// tells the user to update project_name there.
func (r *Registry) Rename(ctx context.Context, ref, newName string) error {
	oldName, err := r.Resolve(ctx, ref)
	if err != nil {
		return err
	}
	if oldName == newName {
		return nil
	}
	if err := ValidateName(newName); err != nil {
		return err
	}
	var existing int
	if err := r.db.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE name = ?`, newName).Scan(&existing); err == nil {
		return fmt.Errorf("project %q already exists", newName)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	// views → view_source_cache is a composite FK without ON UPDATE; check
	// it at commit, after both sides carry the new name.
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return fmt.Errorf("defer foreign keys: %w", err)
	}

	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT config_json FROM projects WHERE name = ?`, oldName).Scan(&raw); err != nil {
		return fmt.Errorf("project %q not found", oldName)
	}
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return fmt.Errorf("unmarshal project %q: %w", oldName, err)
	}
	cfg.ProjectName = newName
	// W&B defaults its project to the runq name; pin the old one so new
	// runs keep landing next to the existing ones.
	if cfg.Wandb != nil && cfg.Wandb.Project == "" {
		cfg.Wandb.Project = oldName
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal project config: %w", err)
	}

	for _, q := range []struct{ sql, what string }{
		{`UPDATE projects SET name = ?, config_json = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ?`, "projects"},
		{`UPDATE jobs SET project_name = ? WHERE project_name = ?`, "jobs"},
		{`UPDATE tasks SET project_name = ? WHERE project_name = ?`, "tasks"},
		{`UPDATE views SET project_name = ? WHERE project_name = ?`, "views"},
		{`UPDATE view_source_cache SET project_name = ? WHERE project_name = ?`, "view_source_cache"},
	} {
		args := []any{newName, oldName}
		if q.what == "projects" {
			args = []any{newName, string(data), oldName}
		}
		if _, err := tx.ExecContext(ctx, q.sql, args...); err != nil {
			return fmt.Errorf("rename in %s: %w", q.what, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit rename: %w", err)
	}

	return nil
}

var idShapedName = regexp.MustCompile(`^pj[0-9a-f]{8}$`)

// ValidateName rejects project names that cannot be a label: empty, path
// separators, or shaped like a project id (which would shadow id lookup).
func ValidateName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fmt.Errorf("project name must not be empty")
	case strings.ContainsAny(name, `/\`):
		return fmt.Errorf("project name %q must not contain path separators", name)
	case idShapedName.MatchString(name):
		return fmt.Errorf("project name %q looks like a project id; pick a descriptive name", name)
	}
	return nil
}

// Remove deletes a project by name.
// Returns an error if the project does not exist.
func (r *Registry) Remove(ctx context.Context, name string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM projects WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("remove project %q: %w", name, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("project %q not found", name)
	}
	return nil
}

// ── Archive ──
//
// Archive hides a project (and, in global listings, its jobs — the cascade
// is applied by the job queries) from default lists. The flag lives in its
// own column, NEVER in config_json: project.yaml is the truth for the
// experiment config, but "archived" is runq bookkeeping — a yaml re-sync
// must not resurrect or clobber it.

func (r *Registry) Archive(ctx context.Context, ref string) error {
	name, err := r.Resolve(ctx, ref)
	if err != nil {
		return err
	}
	// Guard: archiving cascades the project's jobs out of the global lists,
	// so live work must not be hidden — same rule as the per-job guard
	// (paused counts: it is resumable, hence forgettable).
	var active int
	err = r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs WHERE project_name = ? AND status IN `+store.ActiveStatusesSQL(),
		name).Scan(&active)
	if err != nil {
		return err
	}
	if active > 0 {
		return fmt.Errorf("project %q has %d active job(s) — kill them (or let them finish) before archiving", name, active)
	}
	res, err := r.db.ExecContext(ctx, `UPDATE projects SET archived_at = ? WHERE name = ?`, time.Now().Unix(), name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("project %q not found", name)
	}
	return nil
}

func (r *Registry) Unarchive(ctx context.Context, ref string) error {
	name, err := r.Resolve(ctx, ref)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `UPDATE projects SET archived_at = NULL WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("project %q not found", name)
	}
	return nil
}

// IsArchived reports whether a project is archived.
func (r *Registry) IsArchived(ctx context.Context, name string) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM projects WHERE name = ? AND archived_at IS NOT NULL`, name).Scan(&n)
	return n > 0, err
}

// RequireSubmittable is THE precondition for landing new work in a project:
// it must exist and must not be archived (archived jobs cascade out of the
// default lists — a submit would run invisibly). FAIL-CLOSED throughout:
// an unreadable state is a refusal, never a pass. Every submit path
// (daemon service, HPC backend, and any future writer) calls this single
// gate so a new entry point can't forget the rule.
func (r *Registry) RequireSubmittable(ctx context.Context, name string) error {
	var archived sql.NullInt64
	err := r.db.QueryRowContext(ctx, `SELECT archived_at FROM projects WHERE name = ?`, name).Scan(&archived)
	if err == sql.ErrNoRows {
		return fmt.Errorf("project %q not found", name)
	}
	if err != nil {
		return fmt.Errorf("check project %q: %w", name, err)
	}
	if archived.Valid {
		return fmt.Errorf("project %q is archived — `runq project unarchive %s` before submitting", name, name)
	}
	return nil
}

// ArchivedNames returns the set of archived project names. Kept separate
// from List() (which returns Configs — yaml-truth material) so archived
// state rides alongside, not inside, the config.
func (r *Registry) ArchivedNames(ctx context.Context) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT name FROM projects WHERE archived_at IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}
