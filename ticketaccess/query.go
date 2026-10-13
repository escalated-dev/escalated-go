package ticketaccess

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
)

// QueryContext is the shared entry point for package SQL reads. Visibility is
// applied before aggregation, pagination, joins and caller OR predicates.
func QueryContext(ctx context.Context, db *sql.DB, query string, args ...any) (*sql.Rows, error) {
	query, args, err := scopedSQL(ctx, db, query, args)
	if err != nil {
		return nil, err
	}
	return db.QueryContext(ctx, query, args...)
}
func QueryRowContext(ctx context.Context, db *sql.DB, query string, args ...any) *sql.Row {
	query, args, err := scopedSQL(ctx, db, query, args)
	if err != nil {
		return db.QueryRowContext(ctx, "SELECT 1 WHERE 1=0")
	}
	return db.QueryRowContext(ctx, query, args...)
}
func ExecContext(ctx context.Context, db *sql.DB, query string, args ...any) (sql.Result, error) {
	query, args, err := scopedSQL(ctx, db, query, args)
	if err != nil {
		return nil, err
	}
	return db.ExecContext(ctx, query, args...)
}

type sqlToken struct {
	text              string
	start, end, depth int
	identifier        bool
}

// lexSQL leaves string literals and comments opaque. Package SQL is generated
// from fixed statements; this deliberately rejects unsupported scoped writes.
func lexSQL(query string) []sqlToken {
	var tokens []sqlToken
	depth := 0
	for i := 0; i < len(query); {
		start := i
		c := query[i]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			i++
			continue
		}
		if c == '-' && i+1 < len(query) && query[i+1] == '-' {
			for i < len(query) && query[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(query) && query[i+1] == '*' {
			i += 2
			for i+1 < len(query) && query[i:i+2] != "*/" {
				i++
			}
			i = min(i+2, len(query))
			continue
		}
		quoted := c == '\'' || c == '"' || c == '`'
		identifier := c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if quoted {
			i++
			for i < len(query) {
				if query[i] == c {
					i++
					if i < len(query) && query[i] == c {
						i++
						continue
					}
					break
				}
				i++
			}
		} else if identifier || c == '?' || c == '$' || c >= '0' && c <= '9' {
			i++
			for i < len(query) {
				v := query[i]
				if v == '_' || v >= 'a' && v <= 'z' || v >= 'A' && v <= 'Z' || v >= '0' && v <= '9' {
					i++
				} else {
					break
				}
			}
		} else {
			i++
		}
		if c == ')' {
			depth--
		}
		tokens = append(tokens, sqlToken{query[start:i], start, i, depth, identifier || c == '"' || c == '`'})
		if c == '(' {
			depth++
		}
	}
	return tokens
}
func tokenName(t sqlToken) string   { return strings.ToLower(strings.Trim(t.text, "\"`")) }
func projection(name string) string { return "__esc_visible_" + name }

var parents = map[string]map[string]string{
	"replies": {"ticket_id": "tickets"}, "activities": {"ticket_id": "tickets"}, "ticket_activities": {"ticket_id": "tickets"},
	"ticket_tags": {"ticket_id": "tickets"}, "ticket_subjects": {"ticket_id": "tickets"},
	"ticket_followers": {"ticket_id": "tickets"}, "satisfaction_ratings": {"ticket_id": "tickets"},
	"attachments":        {"ticket_id": "tickets", "reply_id": "replies"},
	"ticket_links":       {"parent_ticket_id": "tickets", "child_ticket_id": "tickets"},
	"side_conversations": {"ticket_id": "tickets"}, "side_conversation_replies": {"side_conversation_id": "side_conversations"},
	"mentions": {"reply_id": "replies"}, "chat_sessions": {"ticket_id": "tickets"},
	"chat_messages": {"chat_session_id": "chat_sessions"}, "workflow_logs": {"ticket_id": "tickets"},
}

func protected(name string) bool { _, ok := parents[name]; return name == "tickets" || ok }
func relationPredicate(name string) string {
	if name == "tickets" {
		return "id IN (SELECT id FROM " + projection("tickets") + ")"
	}
	var parts []string
	for column, parent := range parents[name] {
		part := column + " IN (SELECT id FROM " + projection(parent) + ")"
		if name == "attachments" && column == "reply_id" {
			part = "(reply_id IS NULL OR " + part + ")"
		}
		parts = append(parts, part)
	}
	sort.Strings(parts)
	return strings.Join(parts, " AND ")
}

func scopedSQL(ctx context.Context, db *sql.DB, query string, args []any) (string, []any, error) {
	actor := Current(ctx)
	prefix := ""
	if configured, ok := ctx.Value(accessKey{}).(*Access); ok && configured != nil {
		prefix = configured.Prefix
	} else if actor != nil {
		prefix = actor.Prefix
	}
	if prefix != "" && prefix != "escalated_" {
		var out strings.Builder
		last := 0
		for _, token := range lexSQL(query) {
			if token.identifier && strings.HasPrefix(tokenName(token), "escalated_") {
				out.WriteString(query[last:token.start])
				out.WriteString(prefix + strings.TrimPrefix(tokenName(token), "escalated_"))
				last = token.end
			}
		}
		out.WriteString(query[last:])
		query = out.String()
	}
	if actor == nil || (!actor.Departments && !actor.Accounts) {
		return query, args, nil
	}
	tokens := lexSQL(query)
	if len(tokens) == 0 {
		return query, args, nil
	}
	needed := map[string]bool{}
	for _, token := range tokens {
		name := strings.TrimPrefix(tokenName(token), strings.ToLower(actor.Prefix))
		if token.identifier && tokenName(token) == strings.ToLower(actor.Prefix)+name && protected(name) {
			needed[name] = true
		}
	}
	if len(needed) == 0 {
		return query, args, nil
	}
	for i, token := range tokens {
		if token.text == ";" && i != len(tokens)-1 {
			return "", nil, fmt.Errorf("multiple scoped SQL statements are not supported")
		}
	}
	postgres := sqldialect.Rebind(db, "?") == "$1"
	// Number SQLite placeholders explicitly: CTE bindings appear before the
	// caller's bindings in SQL text, but must keep their original argument order.
	var normalized strings.Builder
	last, number := 0, 0
	for _, token := range tokens {
		if token.text == "?" {
			number++
			normalized.WriteString(query[last:token.start])
			if postgres {
				normalized.WriteString("$")
			} else {
				normalized.WriteString("?")
			}
			normalized.WriteString(strconv.Itoa(number))
			last = token.end
		}
	}
	normalized.WriteString(query[last:])
	query = normalized.String()
	tokens = lexSQL(query)
	args = append([]any(nil), args...)
	bind := func(value any) string {
		args = append(args, value)
		marker := "?"
		if postgres {
			marker = "$"
		}
		return marker + strconv.Itoa(len(args))
	}
	ticketPredicate := "1=1"
	if actor.UserID.Empty() {
		ticketPredicate = "1=0"
	}
	if actor.Departments {
		ticketPredicate += " AND EXISTS (SELECT 1 FROM " + actor.Prefix + "department_agent da WHERE da.department_id = " + actor.Prefix + "tickets.department_id AND CAST(da.agent_id AS TEXT) = " + bind(string(actor.UserID)) + ")"
	}
	if actor.Accounts {
		if actor.Account == nil {
			ticketPredicate += " AND 1=0"
		} else {
			collation := "BINARY"
			if postgres {
				collation = `"C"`
			}
			ticketPredicate += " AND account_type COLLATE " + collation + " = " + bind(actor.Account.Type) + " AND account_id COLLATE " + collation + " = " + bind(actor.Account.ID)
		}
	}
	operation := strings.ToUpper(tokens[0].text)
	if operation == "INSERT" {
		if err := guardInsert(ctx, db, tokens, args, actor.Prefix); err != nil {
			return "", nil, err
		}
		// INSERT has no projection rewrite. It consumes only original arguments.
		return query, args[:len(args)-countScopeArgs(actor)], nil
	}
	if operation != "SELECT" && operation != "WITH" && operation != "UPDATE" && operation != "DELETE" {
		return "", nil, fmt.Errorf("unsupported scoped SQL operation")
	}
	if operation == "UPDATE" || operation == "DELETE" {
		targetIndex := 1
		if operation == "DELETE" {
			targetIndex = 2
		}
		if len(tokens) <= targetIndex {
			return "", nil, sql.ErrNoRows
		}
		name := strings.TrimPrefix(tokenName(tokens[targetIndex]), strings.ToLower(actor.Prefix))
		if !protected(name) {
			return "", nil, fmt.Errorf("unsupported scoped write target")
		}
		if operation == "UPDATE" {
			for i, token := range tokens {
				if token.depth != 0 {
					continue
				}
				if strings.EqualFold(token.text, "WHERE") {
					break
				}
				if _, isParent := parents[name][tokenName(token)]; isParent && i+2 < len(tokens) && tokens[i+1].text == "=" {
					// A child move requires access to its new parent as well as its original parent.
					check := "INSERT INTO " + actor.Prefix + name + " (" + token.text + ") VALUES (" + tokens[i+2].text + ")"
					if err := guardInsert(ctx, db, lexSQL(check), args, actor.Prefix); err != nil {
						return "", nil, err
					}
				}
			}
		}
		// Preserve caller ORs and place the access predicate before RETURNING.
		where, end := -1, len(query)
		for _, token := range tokens {
			if token.depth != 0 {
				continue
			}
			if strings.EqualFold(token.text, "WHERE") {
				where = token.end
			}
			if strings.EqualFold(token.text, "RETURNING") || token.text == ";" {
				end = token.start
				break
			}
		}
		if where < 0 {
			query = query[:end] + " WHERE " + relationPredicate(name) + " " + query[end:]
		} else {
			query = query[:where] + " (" + query[where:end] + ") AND (" + relationPredicate(name) + ") " + query[end:]
		}
		// Only the parent projections are needed for a write target.
		if name != "tickets" {
			delete(needed, name)
			for _, parent := range parents[name] {
				needed[parent] = true
			}
		}

		// Subqueries in SET/WHERE must read the same projections. The write
		// target itself remains the real table.
		writeTokens := lexSQL(query)
		var reads strings.Builder
		previous := 0
		for i, token := range writeTokens {
			if i == targetIndex || !token.identifier || i == 0 {
				continue
			}
			prior := strings.ToUpper(writeTokens[i-1].text)
			source := strings.TrimPrefix(tokenName(token), strings.ToLower(actor.Prefix))
			if (prior == "FROM" || prior == "JOIN") && tokenName(token) == strings.ToLower(actor.Prefix)+source && protected(source) {
				needed[source] = true
				reads.WriteString(query[previous:token.start])
				reads.WriteString(projection(source))
				previous = token.end
			}
		}
		reads.WriteString(query[previous:])
		query = reads.String()
	} else {
		var replaced strings.Builder
		last = 0
		for _, token := range tokens {
			name := strings.TrimPrefix(tokenName(token), strings.ToLower(actor.Prefix))
			if token.identifier && tokenName(token) == strings.ToLower(actor.Prefix)+name && protected(name) {
				replaced.WriteString(query[last:token.start])
				replaced.WriteString(projection(name))
				last = token.end
			}
		}
		replaced.WriteString(query[last:])
		query = replaced.String()
	}
	// Dependencies precede children, and each projection is emitted once.
	var definitions []string
	emitted := map[string]bool{}
	var emit func(string)
	emit = func(name string) {
		if emitted[name] {
			return
		}
		emitted[name] = true
		if name == "tickets" {
			definitions = append(definitions, projection(name)+" AS (SELECT * FROM "+actor.Prefix+name+" WHERE "+ticketPredicate+")")
			return
		}
		parentNames := []string{}
		for _, parent := range parents[name] {
			parentNames = append(parentNames, parent)
		}
		sort.Strings(parentNames)
		for _, parent := range parentNames {
			emit(parent)
		}
		definitions = append(definitions, projection(name)+" AS (SELECT * FROM "+actor.Prefix+name+" WHERE "+relationPredicate(name)+")")
	}
	names := []string{}
	for name := range needed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		emit(name)
	}
	if operation == "WITH" {
		query = strings.TrimSpace(query[tokens[0].end:])
		if strings.HasPrefix(strings.ToUpper(query), "RECURSIVE") {
			return "", nil, fmt.Errorf("recursive scoped queries are not supported")
		}
		return "WITH " + strings.Join(definitions, ",") + "," + query, args, nil
	}
	return "WITH " + strings.Join(definitions, ",") + " " + query, args, nil
}
func countScopeArgs(actor *Actor) int {
	n := 0
	if actor.Departments {
		n++
	}
	if actor.Accounts && actor.Account != nil {
		n += 2
	}
	return n
}
func guardInsert(ctx context.Context, db *sql.DB, tokens []sqlToken, args []any, prefix string) error {
	into := -1
	for i, token := range tokens {
		if strings.EqualFold(token.text, "INTO") {
			into = i
			break
		}
	}
	if into < 0 || into+2 >= len(tokens) {
		return sql.ErrNoRows
	}
	name := strings.TrimPrefix(tokenName(tokens[into+1]), strings.ToLower(prefix))
	if !protected(name) {
		return fmt.Errorf("unsupported scoped insert target")
	}
	if tokens[into+2].text != "(" {
		return sql.ErrNoRows
	}
	var columns []string
	i := into + 3
	for ; i < len(tokens) && tokens[i].text != ")"; i++ {
		if tokens[i].text != "," {
			columns = append(columns, tokenName(tokens[i]))
		}
	}
	if i+2 >= len(tokens) || !strings.EqualFold(tokens[i+1].text, "VALUES") || tokens[i+2].text != "(" {
		return fmt.Errorf("scoped inserts require explicit values")
	}
	values := []sqlToken{}
	i += 3
	for ; i < len(tokens) && tokens[i].text != ")"; i++ {
		if tokens[i].text != "," {
			values = append(values, tokens[i])
		}
	}
	if len(values) != len(columns) {
		return fmt.Errorf("unsupported scoped insert expressions")
	}
	if i+1 < len(tokens) && tokens[i+1].text == "," {
		return fmt.Errorf("scoped multi-row inserts are not supported")
	}
	for index, column := range columns {
		parent, ok := parents[name][column]
		if !ok {
			continue
		}
		value := values[index].text
		if strings.EqualFold(value, "NULL") && name == "attachments" && column == "reply_id" {
			continue
		}
		var id any
		if strings.HasPrefix(value, "$") || strings.HasPrefix(value, "?") {
			position, err := strconv.Atoi(value[1:])
			if err != nil || position < 1 || position > len(args) {
				return sql.ErrNoRows
			}
			id = args[position-1]
		} else {
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return sql.ErrNoRows
			}
			id = parsed
		}
		// Nil pointers also represent nullable attachment reply IDs.
		if name == "attachments" && column == "reply_id" {
			if id == nil {
				continue
			}
			if pointer, ok := id.(*int64); ok && pointer == nil {
				continue
			}
		}
		var found int
		if err := QueryRowContext(ctx, db, sqldialect.Rebind(db, "SELECT 1 FROM "+prefix+parent+" WHERE id = ?"), id).Scan(&found); err != nil {
			return err
		}
	}
	return nil
}

type scopedExecutor struct{ db *sql.DB }

func (e scopedExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return ExecContext(ctx, e.db, query, args...)
}
func (e scopedExecutor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return QueryRowContext(ctx, e.db, query, args...)
}
func ExecInsertContext(ctx context.Context, db *sql.DB, query string, args ...any) (sql.Result, error) {
	return sqldialect.ExecInsertOn(ctx, db, scopedExecutor{db}, query, args...)
}
