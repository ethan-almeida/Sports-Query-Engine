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

	"sportsql/internal/cache"
	"sportsql/internal/llm"
	"sportsql/internal/prompt"
	"sportsql/internal/sqlguard"
	"sportsql/internal/store"
)

// askDeps holds the collaborators the /ask handler needs.
type askDeps struct {
	exec                  *store.Executor
	llm                   *llm.Client
	cache                 *cache.Store
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

		if cached, ok, err := deps.cache.Get(r.Context(), question, ctx.Today); err != nil {
			slog.Warn("cache get failed", "question", question, "error", err.Error())
		} else if ok {
			slog.Info("cache hit", "question", question, "elapsed_ms", elapsed(start))
			writeJSON(w, http.StatusOK, askResponse{
				Answer:   cached.Answer,
				SQL:      cached.SQL,
				Rows:     cached.Rows,
				Columns:  cached.Columns,
				Cached:   true,
				DataAsOf: cached.DataAsOf,
			})
			return
		}

		sqlText, result, err := generateSQL(r.Context(), deps, ctx, question)
		if err != nil {
			slog.Error("sql generation failed", "question", question, "error", err.Error(), "elapsed_ms", elapsed(start))
			writeAskError(w, http.StatusBadGateway, "could not generate SQL: "+err.Error())
			return
		}
		slog.Info("query executed", "question", question, "rows", len(result.Rows), "elapsed_ms", elapsed(start))
		if len(result.Rows) == 0 {
			res := askResponse{
				Answer:   "I couldn't find any data for that question.",
				SQL:      sqlText,
				Columns:  result.Columns,
				Cached:   false,
				DataAsOf: ctx.DataAsOf,
			}
			writeJSON(w, http.StatusOK, res)
			setCache(r.Context(), deps, question, ctx.Today, res)
			return
		}

		rendered := renderRows(*result)
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
		res := askResponse{
			Answer:   strings.TrimSpace(answer),
			SQL:      sqlText,
			Rows:     result.Rows,
			Columns:  result.Columns,
			Cached:   false,
			DataAsOf: ctx.DataAsOf,
		}
		writeJSON(w, http.StatusOK, res)
		setCache(r.Context(), deps, question, ctx.Today, res)
	}
}

// setCache stores a finished result so a repeat question is served from Redis.
func setCache(ctx context.Context, deps *askDeps, question, date string, res askResponse) {
	if err := deps.cache.Set(ctx, question, date, cache.Result{
		Answer:   res.Answer,
		SQL:      res.SQL,
		Rows:     res.Rows,
		Columns:  res.Columns,
		DataAsOf: res.DataAsOf,
	}); err != nil {
		slog.Warn("cache set failed", "question", question, "error", err.Error())
	}
}

// generateSQL runs the whole SQL stage with a single repair retry: attempt
// (LLM -> guard -> execute); on a guard or DB error the failed SQL and the
// message are fed back once, and a second attempt is made. It returns the
// safe SQL and executed result, or the final error.
func generateSQL(ctx context.Context, deps *askDeps, pctx prompt.Context, question string) (string, *store.QueryResult, error) {
	var failedSQL, errMsg string
	for attempt := 1; attempt <= 2; attempt++ {
		raw, err := deps.llm.Chat(ctx, deps.sqlModel, prompt.SQLPrompt(pctx, question, failedSQL, errMsg))
		if err != nil {
			return "", nil, fmt.Errorf("llm sql call: %w", err)
		}
		safe, err := sqlguard.Check(raw)
		if err != nil {
			failedSQL, errMsg = raw, err.Error()
			if attempt == 2 {
				return "", nil, err
			}
			slog.Warn("sql retry: guard rejected", "question", question, "error", err.Error())
			continue
		}
		result, err := deps.exec.Query(ctx, safe)
		if err != nil {
			failedSQL, errMsg = safe, err.Error()
			if attempt == 2 {
				return "", nil, err
			}
			slog.Warn("sql retry: query failed", "question", question, "error", err.Error())
			continue
		}
		return safe, &result, nil
	}
	return "", nil, nil
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
