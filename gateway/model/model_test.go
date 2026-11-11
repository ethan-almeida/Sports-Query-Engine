package model

import (
	"strings"
	"testing"
)

func requestJSON(t *testing.T, body string) QueryRequest {
	t.Helper()
	request, err := NewRequest([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error for %s: %v", body, err)
	}
	return request
}

func TestDefaultsApplied(t *testing.T) {
	request := requestJSON(t, `{"sport":"nba","question":"who?"}`)
	if request.MaxRows != DefaultRows {
		t.Errorf("MaxRows = %d, want %d", request.MaxRows, DefaultRows)
	}
}

func TestValidBounds(t *testing.T) {
	request := requestJSON(t, `{"sport":"n","question":"q","max_rows":1}`)
	if request.Sport != "n" || request.Question != "q" || request.MaxRows != 1 {
		t.Errorf("unexpected request %+v", request)
	}
	request = requestJSON(t, `{"sport":"`+strings.Repeat("a", 32)+`","question":"q","max_rows":1000}`)
	if request.MaxRows != 1000 {
		t.Errorf("1000 rows rejected")
	}
}

func TestInvalidBodies(t *testing.T) {
	cases := map[string][]byte{
		"empty":               {},
		"not json":            []byte("not json"),
		"missing sport":       []byte(`{"question":"q"}`),
		"missing question":    []byte(`{"sport":"nba"}`),
		"empty sport":         []byte(`{"sport":"","question":"q"}`),
		"empty question":      []byte(`{"sport":"nba","question":""}`),
		"long question":       []byte(`{"sport":"nba","question":"` + strings.Repeat("a", 2001) + `","max_rows":10}`),
		"long sport":          []byte(`{"sport":"` + strings.Repeat("a", 33) + `","question":"q"}`),
		"low rows":            []byte(`{"sport":"nba","question":"q","max_rows":-1}`),
		"high rows":           []byte(`{"sport":"nba","question":"q","max_rows":1001}`),
		"negative rows":       []byte(`{"sport":"nba","question":"q","max_rows":-5}`),
		"sport type mismatch": []byte(`{"sport":5,"question":"q"}`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewRequest(body); err == nil {
				t.Fatalf("expected rejection")
			}
		})
	}
}

func TestExplicitMaxRowsWins(t *testing.T) {
	request := requestJSON(t, `{"sport":"nba","question":"q","max_rows":7}`)
	if request.MaxRows != 7 {
		t.Errorf("MaxRows = %d, want 7", request.MaxRows)
	}
}
