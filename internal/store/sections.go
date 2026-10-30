package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gliese129/runq-lab/internal/utils"
)

// Sections organize a project's jobs (rc6). They carry no execution config.
// "unsorted" is virtual (section_id NULL); "smoke" is a real, built-in row
// created per project together with a default `name: *smoke*` rule.
const (
	SectionUnsorted = "unsorted"
	SectionSmoke    = "smoke"
)

// Rule kinds: match the job name, or the job file path as passed to submit.
const (
	RuleName = "name"
	RulePath = "path"
)

type SectionRule struct {
	ID      int64  `json:"id"`
	Section string `json:"section"`
	Kind    string `json:"kind"`
	Pattern string `json:"pattern"`
}

type Section struct {
	ID      string        `json:"id,omitempty"` // "" for unsorted
	Name    string        `json:"name"`
	Builtin bool          `json:"builtin"`
	Jobs    int           `json:"jobs"`
	Rules   []SectionRule `json:"rules,omitempty"`
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) projectID(ctx context.Context, project string) (string, error) {
	var id sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id FROM projects WHERE name = ?`, project).Scan(&id)
	if err == sql.ErrNoRows || (err == nil && id.String == "") {
		return "", fmt.Errorf("project %q not found", project)
	}
	return id.String, err
}

// ensureSmoke creates the built-in smoke section (and its default rule) the
// first time a project needs it. Deleting the rule later sticks: the rule
// is only created together with the section.
func (s *Store) ensureSmoke(ctx context.Context, projectID string) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sections WHERE project_id = ? AND builtin = ?`, projectID, SectionSmoke).Scan(&n); err != nil || n > 0 {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := "sc" + utils.GenerateID()
	if _, err := tx.ExecContext(ctx, `INSERT INTO sections (id, project_id, name, builtin, position, created_at) VALUES (?, ?, ?, ?, 0, ?)`,
		id, projectID, SectionSmoke, SectionSmoke, time.Now().Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO section_rules (project_id, section_id, kind, pattern, position) VALUES (?, ?, ?, ?, 0)`,
		projectID, id, RuleName, "*smoke*"); err != nil {
		return err
	}
	return tx.Commit()
}

// ListSections returns unsorted first, then the project's sections in
// order, each with its job count and rules.
func (s *Store) ListSections(ctx context.Context, project string) ([]Section, error) {
	pid, err := s.projectID(ctx, project)
	if err != nil {
		return nil, err
	}
	if err := s.ensureSmoke(ctx, pid); err != nil {
		return nil, err
	}
	var unsorted int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE project_name = ? AND (section_id IS NULL OR section_id = '')`, project).Scan(&unsorted); err != nil {
		return nil, err
	}
	out := []Section{{Name: SectionUnsorted, Builtin: true, Jobs: unsorted}}
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.name, s.builtin != '', (SELECT COUNT(*) FROM jobs j WHERE j.section_id = s.id)
		FROM sections s WHERE s.project_id = ? ORDER BY s.position, s.created_at, s.name`, pid)
	if err != nil {
		return nil, err
	}
	index := map[string]int{}
	for rows.Next() {
		var sec Section
		if err := rows.Scan(&sec.ID, &sec.Name, &sec.Builtin, &sec.Jobs); err != nil {
			rows.Close()
			return nil, err
		}
		index[sec.ID] = len(out)
		out = append(out, sec)
	}
	rows.Close()
	rules, err := s.listRules(ctx, pid)
	if err != nil {
		return nil, err
	}
	for _, r := range rules {
		if i, ok := index[r.sectionID]; ok {
			out[i].Rules = append(out[i].Rules, r.SectionRule)
		}
	}
	return out, nil
}

type ruleRow struct {
	SectionRule
	sectionID string
}

func (s *Store) listRules(ctx context.Context, pid string) ([]ruleRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, s.name, r.kind, r.pattern, r.section_id FROM section_rules r
		JOIN sections s ON s.id = r.section_id
		WHERE r.project_id = ? ORDER BY r.position, r.id`, pid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ruleRow
	for rows.Next() {
		var r ruleRow
		if err := rows.Scan(&r.ID, &r.Section, &r.Kind, &r.Pattern, &r.sectionID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateSection adds a section at the end of the project's order.
func (s *Store) CreateSection(ctx context.Context, project, name string) (*Section, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.EqualFold(name, SectionUnsorted) {
		return nil, fmt.Errorf("section name %q is reserved or empty", name)
	}
	pid, err := s.projectID(ctx, project)
	if err != nil {
		return nil, err
	}
	if err := s.ensureSmoke(ctx, pid); err != nil {
		return nil, err
	}
	var pos int
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM sections WHERE project_id = ?`, pid).Scan(&pos)
	id := "sc" + utils.GenerateID()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO sections (id, project_id, name, position, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, pid, name, pos, time.Now().Unix()); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("section %q already exists in project %q", name, project)
		}
		return nil, err
	}
	return &Section{ID: id, Name: name}, nil
}

// RemoveSection deletes a non-built-in section; its jobs become unsorted
// and its rules go with it.
func (s *Store) RemoveSection(ctx context.Context, project, name string) (int, error) {
	id, err := s.ResolveSection(ctx, project, name)
	if err != nil {
		return 0, err
	}
	var builtin string
	_ = s.db.QueryRowContext(ctx, `SELECT builtin FROM sections WHERE id = ?`, id).Scan(&builtin)
	if id == "" || builtin != "" {
		return 0, fmt.Errorf("section %q is built in and cannot be removed", name)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE jobs SET section_id = NULL WHERE section_id = ?`, id)
	if err != nil {
		return 0, err
	}
	moved, _ := res.RowsAffected()
	// Its views become project-level rather than pointing at nothing.
	if _, err := tx.ExecContext(ctx, `UPDATE views SET section_id = NULL WHERE section_id = ?`, id); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM section_rules WHERE section_id = ?`, id); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sections WHERE id = ?`, id); err != nil {
		return 0, err
	}
	return int(moved), tx.Commit()
}

// ResolveSection maps a section name to its id ("" = unsorted). A miss
// lists the existing sections and how to create one.
func (s *Store) ResolveSection(ctx context.Context, project, name string) (string, error) {
	name = strings.TrimSpace(name)
	if strings.EqualFold(name, SectionUnsorted) {
		return "", nil
	}
	secs, err := s.ListSections(ctx, project)
	if err != nil {
		return "", err
	}
	var names []string
	for _, sec := range secs {
		if sec.Name == name {
			return sec.ID, nil
		}
		names = append(names, sec.Name)
	}
	return "", fmt.Errorf("section %q does not exist in project %q (sections: %s); create it with: runq section create %s %s",
		name, project, strings.Join(names, ", "), utils.ShellQuote(project), utils.ShellQuote(name))
}

// AddRule appends a rule that sorts new jobs into section.
func (s *Store) AddRule(ctx context.Context, project, section, kind, pattern string) (*SectionRule, error) {
	if kind != RuleName && kind != RulePath {
		return nil, fmt.Errorf("rule kind must be %q or %q", RuleName, RulePath)
	}
	if strings.TrimSpace(pattern) == "" {
		return nil, fmt.Errorf("rule pattern must not be empty")
	}
	if _, err := globRegexp(pattern); err != nil {
		return nil, err
	}
	sid, err := s.ResolveSection(ctx, project, section)
	if err != nil {
		return nil, err
	}
	if sid == "" {
		return nil, fmt.Errorf("jobs that match no rule are already unsorted; point the rule at a section")
	}
	pid, err := s.projectID(ctx, project)
	if err != nil {
		return nil, err
	}
	var pos int
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM section_rules WHERE project_id = ?`, pid).Scan(&pos)
	res, err := s.db.ExecContext(ctx, `INSERT INTO section_rules (project_id, section_id, kind, pattern, position) VALUES (?, ?, ?, ?, ?)`,
		pid, sid, kind, pattern, pos)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &SectionRule{ID: id, Section: section, Kind: kind, Pattern: pattern}, nil
}

// RemoveRule deletes one of the project's rules by id.
func (s *Store) RemoveRule(ctx context.Context, project string, ruleID int64) error {
	pid, err := s.projectID(ctx, project)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM section_rules WHERE id = ? AND project_id = ?`, ruleID, pid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("rule %d not found in project %q", ruleID, project)
	}
	return nil
}

// MatchSection applies the project's rules in order to a job; the first
// match wins. Returns the section id and the matching rule ("" / nil = no
// match, i.e. unsorted).
func (s *Store) MatchSection(ctx context.Context, project, jobName, sourcePath string) (string, *SectionRule, error) {
	pid, err := s.projectID(ctx, project)
	if err != nil {
		return "", nil, err
	}
	if err := s.ensureSmoke(ctx, pid); err != nil {
		return "", nil, err
	}
	rules, err := s.listRules(ctx, pid)
	if err != nil {
		return "", nil, err
	}
	for _, r := range rules {
		if RuleMatches(r.Kind, r.Pattern, jobName, sourcePath) {
			rule := r.SectionRule
			return r.sectionID, &rule, nil
		}
	}
	return "", nil, nil
}

// RuleMatches reports whether one rule matches a job. Path patterns
// without a '/' match the file's base name.
func RuleMatches(kind, pattern, jobName, sourcePath string) bool {
	re, err := globRegexp(pattern)
	if err != nil {
		return false
	}
	switch kind {
	case RuleName:
		return jobName != "" && re.MatchString(jobName)
	case RulePath:
		if sourcePath == "" {
			return false
		}
		p := path.Clean(strings.ReplaceAll(sourcePath, "\\", "/"))
		if !strings.Contains(pattern, "/") {
			p = path.Base(p)
		}
		return re.MatchString(p)
	}
	return false
}

// globRegexp compiles a glob: `**` crosses '/', `*` and `?` do not.
// Matching is case-insensitive (names are free text).
func globRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("(?i)^")
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("bad pattern %q: %w", pattern, err)
	}
	return re, nil
}

// SetJobSection moves jobs (ids or unique prefixes) of one project into
// section ("unsorted" clears it). Returns the resolved job ids.
func (s *Store) SetJobSection(ctx context.Context, section string, refs []string) ([]string, error) {
	var ids []string
	project := ""
	for _, ref := range refs {
		j, err := s.ResolveJobRef(ctx, ref)
		if err != nil {
			return nil, err
		}
		if project != "" && j.ProjectName != project {
			return nil, fmt.Errorf("jobs %s and %s belong to different projects; move them separately", ids[0], j.ID)
		}
		project = j.ProjectName
		ids = append(ids, j.ID)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no jobs given")
	}
	sid, err := s.ResolveSection(ctx, project, section)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `UPDATE jobs SET section_id = ? WHERE id = ?`, nullIfEmpty(sid), id); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// SortUnsorted re-applies the project's rules to its unsorted jobs (by
// name; job file paths are not recorded). Returns how many moved.
func (s *Store) SortUnsorted(ctx context.Context, project string) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(note, '') FROM jobs WHERE project_name = ? AND (section_id IS NULL OR section_id = '')`, project)
	if err != nil {
		return 0, err
	}
	type pending struct{ id, name string }
	var jobs []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.name); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, p)
	}
	rows.Close()
	moved := 0
	for _, j := range jobs {
		sid, _, err := s.MatchSection(ctx, project, j.name, "")
		if err != nil {
			return moved, err
		}
		if sid == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE jobs SET section_id = ? WHERE id = ?`, sid, j.id); err != nil {
			return moved, err
		}
		moved++
	}
	return moved, nil
}

// ErrRefNotFound: a ref names no job/task (wrapped with the ref).
var ErrRefNotFound = errors.New("not found")

// likePrefix is ref as a LIKE prefix pattern: % and _ in a typed ref are
// literal characters, not wildcards (use with ESCAPE '\').
// likePrefix and prefixOf match an id prefix as the dashboard does: LIKE
// finds candidates (it ignores ASCII case), substr keeps the exact-case ones.
const prefixOf = `substr(id, 1, length(?)) = ?`

func likePrefix(ref string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(ref) + "%"
}

// ResolveJobRef finds a job by id or unique id prefix.
func (s *Store) ResolveJobRef(ctx context.Context, ref string) (*JobRow, error) {
	if ref == "" {
		return nil, fmt.Errorf("job %q: %w", ref, ErrRefNotFound)
	}
	if j, err := s.GetJob(ctx, ref); err != nil || j != nil {
		return j, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM jobs WHERE id LIKE ? ESCAPE '\' AND `+prefixOf+` ORDER BY id LIMIT 6`, likePrefix(ref), ref, ref)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	switch len(ids) {
	case 0:
		return nil, fmt.Errorf("job %q: %w", ref, ErrRefNotFound)
	case 1:
		return s.GetJob(ctx, ids[0])
	}
	return nil, &AmbiguousRefError{Kind: "job", Ref: ref, IDs: ids}
}

// ResolveTaskRef resolves a task by exact id or unique id prefix.
func (s *Store) ResolveTaskRef(ctx context.Context, ref string) (string, error) {
	if exp, cell, ok := strings.Cut(ref, "/"); ok {
		return s.resolveVersionRef(ctx, exp, cell)
	}
	if ref == "" {
		return "", fmt.Errorf("task %q: %w", ref, ErrRefNotFound)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM tasks WHERE id = ? OR (id LIKE ? ESCAPE '\' AND `+prefixOf+`) ORDER BY id = ? DESC, id LIMIT 6`, ref, likePrefix(ref), ref, ref, ref)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		if id == ref {
			return id, nil
		}
		ids = append(ids, id)
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("task %q: %w", ref, ErrRefNotFound)
	case 1:
		return ids[0], nil
	}
	return "", &AmbiguousRefError{Kind: "task", Ref: ref, IDs: ids}
}

// AmbiguousRefError: an id prefix matches several ids (at most 6 listed).
type AmbiguousRefError struct {
	Kind, Ref string
	IDs       []string
}

func (e *AmbiguousRefError) Error() string {
	return fmt.Sprintf("%s %q is ambiguous: %s", e.Kind, e.Ref, strings.Join(e.IDs, ", "))
}

// resolveVersionRef resolves <experiment>/<cell>[@vN]: version N of the cell
// (1 = its first; numbers are never reused), or its selected version
// without @vN. A deleted version is an error, never another version.
func (s *Store) resolveVersionRef(ctx context.Context, expRef, cellRef string) (string, error) {
	j, err := s.ResolveJobRef(ctx, expRef)
	if err != nil {
		return "", err
	}
	name, n := cellRef, 0
	if at := strings.LastIndex(cellRef, "@v"); at >= 0 {
		if k, err := strconv.Atoi(cellRef[at+2:]); err == nil && k > 0 {
			name, n = cellRef[:at], k
		}
	}
	tasks, err := s.JobTasksInOrder(ctx, j.ID)
	if err != nil {
		return "", err
	}
	picks, err := s.CellPicks(ctx, j.ID)
	if err != nil {
		return "", err
	}
	last, err := s.CellLastVersions(ctx, j.ID)
	if err != nil {
		return "", err
	}
	for _, c := range GroupCells(tasks, picks) {
		if c.Name != name {
			continue
		}
		if n == 0 {
			return c.Selected, nil
		}
		var have []string
		for _, v := range c.Versions {
			if v.Version == n {
				return v.ID, nil
			}
			have = append(have, fmt.Sprintf("v%d", v.Version))
		}
		if n <= last[name] {
			return "", fmt.Errorf("version %d of cell %q of %s was deleted (versions left: %s): %w", n, name, j.ID, strings.Join(have, ", "), ErrRefNotFound)
		}
		return "", fmt.Errorf("cell %q of %s has no version %d (versions: %s): %w", name, j.ID, n, strings.Join(have, ", "), ErrRefNotFound)
	}
	if n > 0 && last[name] >= n {
		return "", fmt.Errorf("version %d of cell %q of %s was deleted (no versions left): %w", n, name, j.ID, ErrRefNotFound)
	}
	return "", fmt.Errorf("%s has no cell %q: %w", j.ID, name, ErrRefNotFound)
}
