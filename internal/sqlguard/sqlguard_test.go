package sqlguard

import (
	"strings"
	"testing"
)

func TestCheckAccepts(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain select", in: "SELECT 1", want: "SELECT * FROM (SELECT 1) AS sportsql_guard LIMIT 200"},
		{name: "lowercase", in: "select * from players", want: "SELECT * FROM (select * from players) AS sportsql_guard LIMIT 200"},
		{name: "multiline with order and limit", in: "SELECT name FROM teams\nORDER BY name\nLIMIT 5", want: "SELECT * FROM (SELECT name FROM teams\nORDER BY name\nLIMIT 5) AS sportsql_guard LIMIT 200"},
		{name: "trailing semicolon trimmed", in: "SELECT 1;", want: "SELECT * FROM (SELECT 1) AS sportsql_guard LIMIT 200"},
		{name: "code fenced sql", in: "```sql\nSELECT 1\n```", want: "SELECT * FROM (SELECT 1) AS sportsql_guard LIMIT 200"},
		{name: "plain code fence", in: "```\nSELECT 1\n```", want: "SELECT * FROM (SELECT 1) AS sportsql_guard LIMIT 200"},
		{name: "sql fence with trailing newline", in: "SELECT 1\n", want: "SELECT * FROM (SELECT 1) AS sportsql_guard LIMIT 200"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Check(c.in)
			if err != nil {
				t.Fatalf("Check(%q) unexpected error: %v", c.in, err)
			}
			if c.want != "" && got != c.want {
				t.Fatalf("Check(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestCheckRejects(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "no sql", in: "I cannot help you"},
		{name: "prose blocking the select", in: "Here is the query:\n\nSELECT count(*) FROM games"},
		{name: "non select", in: "SHOW TABLES"},
		{name: "explain", in: "EXPLAIN SELECT * FROM players"},
		{name: "with delete cte", in: "WITH x AS (DELETE FROM players RETURNING *) SELECT * FROM x"},
		{name: "multiple statements", in: "SELECT 1; SELECT 2"},
		{name: "drop", in: "DROP TABLE players"},
		{name: "drop lowercase", in: "drop table players"},
		{name: "insert", in: "INSERT INTO players VALUES ('x','y')"},
		{name: "update", in: "UPDATE players SET full_name = 'x'"},
		{name: "delete", in: "DELETE FROM players"},
		{name: "truncate", in: "TRUNCATE players"},
		{name: "alter", in: "ALTER TABLE players ADD COLUMN x int"},
		{name: "create", in: "CREATE TABLE hack(x int)"},
		{name: "grant", in: "GRANT ALL ON players TO api_ro"},
		{name: "revoke", in: "REVOKE SELECT ON players FROM api_ro"},
		{name: "copy", in: "COPY players TO STDOUT"},
		{name: "vacuum", in: "VACUUM FULL players"},
		{name: "select into", in: "SELECT * INTO temp FROM players"},
		{name: "pg_sleep", in: "SELECT pg_sleep(10)"},
		{name: "pg_read_file", in: "SELECT pg_read_file('/etc/passwd')"},
		{name: "lo_import", in: "SELECT lo_import('/tmp/x')"},
		{name: "set search_path", in: "SELECT 1; SET search_path TO public"},
		{name: "block comment", in: "SELECT /* no */ 1"},
		{name: "line comment", in: "SELECT 1 -- note"},
		{name: "semicolon then drop", in: "SELECT 1; DROP TABLE players"},
		{name: "leading semicolon", in: ";SELECT 1"},
		{name: "drop phrase in comment", in: "SELECT 1 /* drop */"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Check(c.in)
			if err == nil {
				t.Fatalf("Check(%q) expected error, got output: %q", c.in, got)
			}
		})
	}
}

func TestCheckErrorMessage(t *testing.T) {
	_, err := Check("SELECT pg_sleep(10)")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "pg_sleep") {
		t.Fatalf("error %q should mention the offending keyword", err)
	}
}
