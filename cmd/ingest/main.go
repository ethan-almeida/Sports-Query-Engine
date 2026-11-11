package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"sportsql/internal/config"
)

const usage = `usage: ingest [flags]

One-shot NBA ingest. Fetches games and box scores for a date range from ESPN
and upserts them idempotently into Postgres.

Flags:
  --from <YYYY-MM-DD>  First date to ingest. Defaults to max(games.game_date) - 2 days.
  --to   <YYYY-MM-DD>  Last date to ingest (inclusive). Defaults to today.
`

func main() {
	settings := config.Load()
	if err := settings.ForIngest(); err != nil {
		slog.Error("config", "error", err.Error())
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(settings.LogLevel),
	}))
	slog.SetDefault(logger)

	from := flag.String("from", "", "first date to ingest (YYYY-MM-DD); defaults to max(games.game_date) - 2 days")
	to := flag.String("to", "", "last date to ingest (YYYY-MM-DD, inclusive); defaults to today")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
	}
	flag.Parse()

	logger.Warn("ingest: not implemented yet", "from", *from, "to", *to)
	os.Exit(2)
}

func parseLevel(level string) slog.Level {
	switch level {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
