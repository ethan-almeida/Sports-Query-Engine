package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sportsql/internal/llm"
	"sportsql/internal/prompt"
	"sportsql/internal/sqlguard"
	"sportsql/internal/store"
)

// askDeps holds the collaborators the /ask handler needs.
type askDeps struct {
	exec                  *store.Executor
	llm                   *llm.Client
	sqlModel, answerModel string
}

// askResponse is the JSON body returned by POST /ask.
type askResponse struct {
	Answer   string   `json:"answer"`
	SQL      string   `json:"sql"`
	Rows     [][]any  `json:"rows"`
	Columns  []string `json:"columns,omitempty"`
	Cached   bool     `json:"cached"`
	DataAsOf string   `json:"data_as_of"`
}

// askHandler implements the full M3 flow: question -> SQL -> execute -> sentence.
func askHandler(deps *askDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var req struct {
			Question string `json:"question"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAskError(w, http.StatusBadRequest, "request body must be JSON with a \"question\" field")
			return
		}
		question := strings.TrimSpace(req.Question)
		if question == "" {
			writeAskError(w, http.StatusBadRequest, "question is empty")
			return
		}

		ctx := prompt.Context{
			Today:    time.Now().UTC().Format("2006-01-02"),
			Season:   "",
			DataAsOf: "",
		}
		ctx.Season, _ = deps.exec.CurrentSeason(r.Context())
		ctx.DataAsOf, _ = deps.exec.DataAsOf(r.Context())

		sqlText, err := generateSQL(r.Context(), deps, ctx, question)
		if err != nil {
			slog.Error("sql generation failed", "question", question, "error", err.Error(), "elapsed_ms", elapsed(start))
			writeAskError(w, http.StatusBadGateway, "could not generate SQL: "+err.Error())
			return
		}

		result, err := deps.exec.Query(r.Context(), sqlText)
		if err != nil {
			slog.Error("query execution failed", "question", question, "sql", sqlText, "error", err.Error(), "elapsed_ms", elapsed(start))
			writeAskError(w, http.StatusBadGateway, "query failed: "+err.Error())
			return
		}
		slog.Info("query executed", "question", question, "rows", len(result.Rows), "elapsed_ms", elapsed(start))
		if len(result.Rows) == 0 {
			writeJSON(w, http.StatusOK, askResponse{
				Answer:   "I couldn't find any data for that question.",
				SQL:      sqlText,
				Columns:  result.Columns,
				Cached:   false,
				DataAsOf: ctx.DataAsOf,
			})
			return
		}

		rendered := renderRows(result)
		answer, err := deps.llm.Chat(r.Context(), deps.answerModel, prompt.AnswerPrompt(ctx, question, sqlText, rendered))
		if err != nil {
			slog.Error("answer generation failed", "question", question, "error", err.Error(), "elapsed_ms", elapsed(start))
			// Rows are already real data; return them even without a sentence.
			writeJSON(w, http.StatusOK, askResponse{
				Answer:   "The query ran, but I couldn't turn the results into a sentence: " + err.Error(),
				SQL:      sqlText,
				Rows:     result.Rows,
				Columns:  result.Columns,
				Cached:   false,
				DataAsOf: ctx.DataAsOf,
			})
			return
		}
		slog.Info("ask complete", "question", question, "elapsed_ms", elapsed(start))
		writeJSON(w, http.StatusOK, askResponse{
			Answer:   strings.TrimSpace(answer),
			SQL:      sqlText,
			Rows:     result.Rows,
			Columns:  result.Columns,
			Cached:   false,
			DataAsOf: ctx.DataAsOf,
		})
	}
}

// generateSQL asks the SQL model for a query, guards it, and executes nothing
// itself. M3 has no repair retry; the guard error surfaces to the caller.
func generateSQL(ctx context.Context, deps *askDeps, pctx prompt.Context, question string) (string, error) {
	raw, err := deps.llm.Chat(ctx, deps.sqlModel, prompt.SQLPrompt(pctx, question))
	if err != nil {
		return "", err
	}
	safe, err := sqlguard.Check(raw)
	if err != nil {
		return "", fmt.Errorf("guard rejected the SQL: %w", err)
	}
	return safe, nil
}

// renderRows formats columns and rows as plain text for the answer prompt,
// capping at 25 rows so the prompt stays small.
func renderRows(result store.QueryResult) string {
	var b strings.Builder
	b.WriteString(strings.Join(result.Columns, "\t"))
	b.WriteString("\n")
	limit := 25
	if len(result.Rows) < limit {
		limit = len(result.Rows)
	}
	for _, row := range result.Rows[:limit] {
		cells := make([]string, 0, len(row))
		for _, v := range row {
			cells = append(cells, cellString(v))
		}
		b.WriteString(strings.Join(cells, "\t"))
		b.WriteString("\n")
	}
	if len(result.Rows) > 25 {
		fmt.Fprintf(&b, "... (%d more rows)\n", len(result.Rows)-25)
	}
	return b.String()
}

// cellString renders one row value for the prompt.
func cellString(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case time.Time:
		return t.Format("2006-01-02")
	default:
		return fmt.Sprint(t)
	}
}

// writeJSON sends a JSON body with the given status.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	if err := enc.Encode(body); err != nil {
		slog.Error("writing response failed", "error", err.Error())
	}
}

// writeAskError sends a JSON error body.
func writeAskError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func elapsed(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}
