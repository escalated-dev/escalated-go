package plugins

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
)

const configKey = "__config__"

// activation is one escalated_plugins row.
type activation struct {
	Slug          string
	IsActive      bool
	ActivatedAt   *time.Time
	DeactivatedAt *time.Time
}

// rows reads and writes escalated_plugins and escalated_plugin_store.
type rows struct {
	db     *sql.DB
	prefix string
}

func (s *rows) q(query string) string {
	return sqldialect.Rebind(s.db, strings.ReplaceAll(query, "{p}", s.prefix))
}

func (s *rows) activations(ctx context.Context) (map[string]activation, error) {
	out := map[string]activation{}
	rs, err := s.db.QueryContext(ctx, s.q(`SELECT slug, is_active, activated_at, deactivated_at FROM {p}plugins`))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rs.Close() }()
	for rs.Next() {
		var a activation
		var activated, deactivated sql.NullTime
		if err := rs.Scan(&a.Slug, &a.IsActive, &activated, &deactivated); err != nil {
			return nil, err
		}
		if activated.Valid {
			t := activated.Time
			a.ActivatedAt = &t
		}
		if deactivated.Valid {
			t := deactivated.Time
			a.DeactivatedAt = &t
		}
		out[a.Slug] = a
	}
	return out, rs.Err()
}

func (s *rows) activation(ctx context.Context, slug string) (*activation, error) {
	all, err := s.activations(ctx)
	if err != nil {
		return nil, err
	}
	a, ok := all[slug]
	if !ok {
		return nil, nil
	}
	return &a, nil
}

// activate upserts the row as active.
func (s *rows) activate(ctx context.Context, slug string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE {p}plugins SET is_active = ?, activated_at = ?, deactivated_at = NULL, updated_at = ? WHERE slug = ?`), true, now, now, slug)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = s.db.ExecContext(ctx, s.q(`INSERT INTO {p}plugins (slug, is_active, activated_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`), slug, true, now, now, now)
	return err
}

func (s *rows) deactivate(ctx context.Context, slug string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE {p}plugins SET is_active = ?, deactivated_at = ?, updated_at = ? WHERE slug = ?`), false, now, now, slug)
	return err
}

func (s *rows) forget(ctx context.Context, slug string) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM {p}plugins WHERE slug = ?`), slug)
	return err
}

// StoreRecord is one ctx.store record as plugins see it: its data with the
// row id as _id.
type StoreRecord = map[string]any

// QueryOptions are ctx.store.query's options.
type QueryOptions struct {
	Limit   *int   `json:"limit,omitempty"`
	OrderBy string `json:"orderBy,omitempty"`
	Order   string `json:"order,omitempty"`
}

// fieldPattern is a dotted field name. Anything else in a filter is an error,
// never SQL.
var fieldPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)*$`)

var queryOperators = map[string]bool{"$gt": true, "$gte": true, "$lt": true, "$lte": true, "$ne": true, "$in": true, "$nin": true}

type storeRow struct {
	id   int64
	data any
}

func (s *rows) find(ctx context.Context, plugin, collection, key string) (*storeRow, error) {
	var id int64
	var raw []byte
	err := s.db.QueryRowContext(ctx, s.q(`SELECT id, data FROM {p}plugin_store WHERE plugin = ? AND collection = ? AND key = ? ORDER BY id LIMIT 1`), plugin, collection, key).Scan(&id, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &storeRow{id: id, data: decodeJSON(raw)}, nil
}

func decodeJSON(raw []byte) any {
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return nil
	}
	return normalizeNumbers(v)
}

// normalizeNumbers turns json.Number into float64, keeping the values plugins
// compare with the same types the bridge's JSON gives them.
func normalizeNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		f, _ := x.Float64()
		return f
	case map[string]any:
		for k, item := range x {
			x[k] = normalizeNumbers(item)
		}
		return x
	case []any:
		for i, item := range x {
			x[i] = normalizeNumbers(item)
		}
		return x
	}
	return v
}

func encodeJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// roundTrip gives a value the shape the bridge's JSON would: maps, slices,
// float64, strings, bools and nil.
func roundTrip(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return decodeJSON(b), nil
}

func asRecord(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func (s *rows) configAll(ctx context.Context, plugin string) (map[string]any, error) {
	row, err := s.find(ctx, plugin, configKey, configKey)
	if err != nil || row == nil {
		return map[string]any{}, err
	}
	if m := asRecord(row.data); m != nil {
		return m, nil
	}
	return map[string]any{}, nil
}

func (s *rows) configGet(ctx context.Context, plugin, key string) (any, error) {
	all, err := s.configAll(ctx, plugin)
	if err != nil {
		return nil, err
	}
	return all[key], nil
}

func (s *rows) configSet(ctx context.Context, plugin string, data map[string]any) error {
	if data == nil {
		return fmt.Errorf("ctx.config.set requires an object")
	}
	merged, err := s.configAll(ctx, plugin)
	if err != nil {
		return err
	}
	clean, err := roundTrip(data)
	if err != nil {
		return err
	}
	for k, v := range asRecord(clean) {
		merged[k] = v
	}
	return s.set(ctx, plugin, configKey, configKey, merged)
}

func (s *rows) get(ctx context.Context, plugin, collection, key string) (any, error) {
	row, err := s.find(ctx, plugin, collection, key)
	if err != nil || row == nil {
		return nil, err
	}
	return row.data, nil
}

func (s *rows) set(ctx context.Context, plugin, collection, key string, value any) error {
	data, err := encodeJSON(value)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	row, err := s.find(ctx, plugin, collection, key)
	if err != nil {
		return err
	}
	if row != nil {
		_, err = s.db.ExecContext(ctx, s.q(`UPDATE {p}plugin_store SET data = ?, updated_at = ? WHERE id = ?`), data, now, row.id)
		return err
	}
	_, err = s.db.ExecContext(ctx, s.q(`INSERT INTO {p}plugin_store (plugin, collection, key, data, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`), plugin, collection, key, data, now, now)
	return err
}

func present(id int64, data any) StoreRecord {
	out := StoreRecord{}
	for k, v := range asRecord(data) {
		out[k] = v
	}
	out["_id"] = id
	return out
}

func (s *rows) insert(ctx context.Context, plugin, collection string, data map[string]any) (StoreRecord, error) {
	if data == nil {
		return nil, fmt.Errorf("ctx.store.insert requires an object")
	}
	clean, err := roundTrip(data)
	if err != nil {
		return nil, err
	}
	var key any
	if k, ok := data["key"]; ok && k != nil {
		key = fmt.Sprint(k)
		if f, isFloat := k.(float64); isFloat && f == math.Trunc(f) {
			key = fmt.Sprintf("%.0f", f)
		}
	}
	encoded, _ := encodeJSON(clean)
	now := time.Now().UTC()
	res, err := sqldialect.ExecInsertContext(ctx, s.db, strings.ReplaceAll(`INSERT INTO {p}plugin_store (plugin, collection, key, data, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, "{p}", s.prefix), plugin, collection, key, encoded, now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return present(id, clean), nil
}

func (s *rows) update(ctx context.Context, plugin, collection, key string, data map[string]any) (StoreRecord, error) {
	if data == nil {
		return nil, fmt.Errorf("ctx.store.update requires an object")
	}
	row, err := s.find(ctx, plugin, collection, key)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("no %s record with key %s", collection, key)
	}
	merged := map[string]any{}
	for k, v := range asRecord(row.data) {
		merged[k] = v
	}
	clean, err := roundTrip(data)
	if err != nil {
		return nil, err
	}
	for k, v := range asRecord(clean) {
		merged[k] = v
	}
	encoded, _ := encodeJSON(merged)
	if _, err := s.db.ExecContext(ctx, s.q(`UPDATE {p}plugin_store SET data = ?, updated_at = ? WHERE id = ?`), encoded, time.Now().UTC(), row.id); err != nil {
		return nil, err
	}
	return present(row.id, merged), nil
}

func (s *rows) delete(ctx context.Context, plugin, collection, key string) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM {p}plugin_store WHERE plugin = ? AND collection = ? AND key = ?`), plugin, collection, key)
	return err
}

// query filters in this process rather than in SQL, as NestJS does: the
// operators then behave the same on every database, and a field name is only
// ever a path lookup.
func (s *rows) query(ctx context.Context, plugin, collection string, filter map[string]any, opts QueryOptions) ([]StoreRecord, error) {
	for field, cond := range filter {
		if !fieldPattern.MatchString(field) {
			return nil, fmt.Errorf("invalid store field name: %s", field)
		}
		if m := asRecord(cond); m != nil {
			for op := range m {
				if !queryOperators[op] {
					return nil, fmt.Errorf("unsupported store operator: %s", op)
				}
			}
		}
	}
	if opts.OrderBy != "" && !fieldPattern.MatchString(opts.OrderBy) {
		return nil, fmt.Errorf("invalid store field name: %s", opts.OrderBy)
	}
	cleanFilter, err := roundTrip(filter)
	if err != nil {
		return nil, err
	}
	filter = asRecord(cleanFilter)

	rs, err := s.db.QueryContext(ctx, s.q(`SELECT id, data FROM {p}plugin_store WHERE plugin = ? AND collection = ? ORDER BY id`), plugin, collection)
	if err != nil {
		return nil, err
	}
	var found []storeRow
	for rs.Next() {
		var id int64
		var raw []byte
		if err := rs.Scan(&id, &raw); err != nil {
			_ = rs.Close()
			return nil, err
		}
		row := storeRow{id: id, data: decodeJSON(raw)}
		if matchesFilter(row.data, filter) {
			found = append(found, row)
		}
	}
	_ = rs.Close()
	if err := rs.Err(); err != nil {
		return nil, err
	}

	if opts.OrderBy != "" {
		desc := strings.EqualFold(opts.Order, "desc")
		sort.SliceStable(found, func(i, j int) bool {
			c := compareSortKeys(fieldOf(found[i].data, opts.OrderBy), fieldOf(found[j].data, opts.OrderBy))
			if desc {
				return c > 0
			}
			return c < 0
		})
	}
	if opts.Limit != nil {
		n := *opts.Limit
		if n < 0 {
			n = 0
		}
		if n < len(found) {
			found = found[:n]
		}
	}
	out := make([]StoreRecord, 0, len(found))
	for _, row := range found {
		out = append(out, present(row.id, row.data))
	}
	return out, nil
}

func matchesFilter(data any, filter map[string]any) bool {
	for field, cond := range filter {
		value := fieldOf(data, field)
		if ops := asRecord(cond); ops != nil {
			for op, operand := range ops {
				if !matchOp(value, op, operand) {
					return false
				}
			}
			continue
		}
		if !contains(value, cond) {
			return false
		}
	}
	return true
}

func fieldOf(data any, field string) any {
	v := data
	for _, part := range strings.Split(field, ".") {
		m := asRecord(v)
		if m == nil {
			return nil
		}
		v = m[part]
	}
	return v
}

func same(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return bytes.Equal(ja, jb)
}

// contains is equality as escalated-laravel's whereJsonContains: an array
// field contains the value.
func contains(value, wanted any) bool {
	if list, ok := value.([]any); ok {
		if wantList, ok := wanted.([]any); ok {
			for _, w := range wantList {
				if !anySame(list, w) {
					return false
				}
			}
			return true
		}
		return anySame(list, wanted)
	}
	return same(value, wanted)
}

func anySame(list []any, v any) bool {
	for _, item := range list {
		if same(item, v) {
			return true
		}
	}
	return false
}

func matchOp(value any, op string, operand any) bool {
	switch op {
	case "$ne":
		return !same(value, operand)
	case "$in", "$nin":
		list, ok := operand.([]any)
		if !ok {
			list = []any{operand}
		}
		in := anySame(list, value)
		if op == "$in" {
			return in
		}
		return !in
	}
	c, ok := compareValues(value, operand)
	if !ok {
		return false
	}
	switch op {
	case "$gt":
		return c > 0
	case "$gte":
		return c >= 0
	case "$lt":
		return c < 0
	default:
		return c <= 0
	}
}

func compareValues(a, b any) (int, bool) {
	if fa, ok := a.(float64); ok {
		if fb, ok := b.(float64); ok {
			switch {
			case fa < fb:
				return -1, true
			case fa > fb:
				return 1, true
			}
			return 0, true
		}
	}
	if sa, ok := a.(string); ok {
		if sb, ok := b.(string); ok {
			return strings.Compare(sa, sb), true
		}
	}
	return 0, false
}

// compareSortKeys orders numbers first, then strings, then everything else.
func compareSortKeys(a, b any) int {
	rank := func(v any) int {
		switch v.(type) {
		case float64:
			return 0
		case string:
			return 1
		}
		return 2
	}
	ra, rb := rank(a), rank(b)
	if ra != rb {
		return ra - rb
	}
	if c, ok := compareValues(a, b); ok {
		return c
	}
	return 0
}
