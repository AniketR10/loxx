package store

import (
	"context"
	"strings"
)

// Filter narrows searches to a subset of commands. The zero Filter matches
// everything. The meanings were decided with the user (ROADMAP §3, panel
// behaviour).
type Filter struct {
	// Dir keeps commands run in this directory, or anywhere in GitRoot when
	// GitRoot is set (the git repository containing Dir).
	Dir, GitRoot string
	// Session keeps commands run in this shell session.
	Session string
	// Failed keeps commands whose most recent run exited non-zero.
	Failed bool
}

// IsZero reports whether f matches every command.
func (f Filter) IsZero() bool { return f == Filter{} }

// where returns a SQL condition on the commands table aliased c, and its
// arguments. It is "1" (true) for the zero Filter.
func (f Filter) where() (string, []any) {
	var conds []string
	var args []any
	if f.Dir != "" {
		cond := `EXISTS (SELECT 1 FROM executions x WHERE x.command_id = c.id AND (x.cwd = ?`
		args = append(args, f.Dir)
		if f.GitRoot != "" {
			cond += ` OR x.git_root = ?`
			args = append(args, f.GitRoot)
		}
		conds = append(conds, cond+`))`)
	}
	if f.Session != "" {
		conds = append(conds, `EXISTS (SELECT 1 FROM executions x WHERE x.command_id = c.id AND x.session_id = ?)`)
		args = append(args, f.Session)
	}
	if f.Failed {
		conds = append(conds, `coalesce((SELECT x.exit_code FROM executions x WHERE x.command_id = c.id
			ORDER BY x.started_at DESC, x.id DESC LIMIT 1), 0) != 0`)
	}
	if len(conds) == 0 {
		return "1", nil
	}
	return strings.Join(conds, " AND "), args
}

// FilterIDs returns the set of command ids that f keeps, or nil (meaning all)
// for the zero Filter.
func (db *DB) FilterIDs(ctx context.Context, f Filter) (map[int64]bool, error) {
	if f.IsZero() {
		return nil, nil
	}
	cond, args := f.where()
	rows, err := db.sql.QueryContext(ctx, `SELECT c.id FROM commands c WHERE `+cond, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// RecentIDs returns up to limit ids of the commands f keeps, most recently
// run first. It is what the search panel shows before anything is typed.
func (db *DB) RecentIDs(ctx context.Context, f Filter, limit int) ([]int64, error) {
	cond, args := f.where()
	rows, err := db.sql.QueryContext(ctx,
		`SELECT c.id FROM commands c WHERE `+cond+` ORDER BY c.last_seen DESC, c.id DESC LIMIT ?`,
		append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
