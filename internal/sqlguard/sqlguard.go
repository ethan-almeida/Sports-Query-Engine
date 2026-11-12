// Package sqlguard does a first-pass check that LLM output is a single read-only
// SELECT. The read-only DB role is the protection; this filter just keeps
// obviously unsafe or malformed SQL from reaching Postgres.
package sqlguard

import (
	"fmt"
	"regexp"
	"strings"
)

// MaxRows caps every query so one answer can't scan the whole database.
const MaxRows = 200

var (
	blocklist    = []string{"insert", "update", "delete", "drop", "alter", "truncate", "create", "grant", "revoke", "copy", "vacuum", "select into", "pg_sleep", "pg_read_file", "lo_import", "set"}
	multiSelect  = regexp.MustCompile(`(?is);\s*\S`)
	commentStart = regexp.MustCompile(`(?s)/\*|\-\-`)
)

// Check cleans up the LLM reply, verifies it is one SELECT statement, and
// returns the query wrapped in an outer LIMIT 200. The returned error is worded
// so it can be fed back to the LLM.
func Check(raw string) (string, error) {
	sql := extractStatement(raw)
	if sql == "" {
		return "", fmt.Errorf("no SQL statement found in the reply")
	}
	upper := strings.ToUpper(strings.TrimSpace(sql))
	if !strings.HasPrefix(upper, "SELECT") {
		return "", fmt.Errorf("only a single SELECT statement is allowed, got: %s", firstWords(sql, 5))
	}
	if i := commentStart.FindStringIndex(sql); i != nil {
		return "", fmt.Errorf("SQL comments are not allowed")
	}
	if multiSelect.FindString(sql) != "" {
		return "", fmt.Errorf("multiple statements are not allowed")
	}
	for _, bad := range blocklist {
		if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(bad) + `\b`).MatchString(sql) {
			return "", fmt.Errorf("the keyword or function %q is not allowed", bad)
		}
	}
	return fmt.Sprintf("SELECT * FROM (%s) AS sportsql_guard LIMIT %d", sql, MaxRows), nil
}

// extractStatement pulls the SQL out of a reply: strips code fences and
// surrounding prose, and drops a single trailing semicolon.
func extractStatement(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```sql")
		s = strings.TrimPrefix(s, "```")
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
	}
	s = strings.TrimSpace(s)
	trimmed := strings.TrimRight(s, " ;\t\n")
	if len(trimmed) < len(s) {
		s = strings.TrimRight(trimmed, " ;\t\n")
	}
	return s
}

// firstWords returns at most n leading words, for error messages.
func firstWords(s string, n int) string {
	fields := strings.Fields(s)
	if len(fields) > n {
		fields = fields[:n]
	}
	return strings.Join(fields, " ")
}
