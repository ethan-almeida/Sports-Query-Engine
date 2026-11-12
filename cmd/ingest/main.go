package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"sportsql/internal/config"
	"sportsql/internal/source"
	"sportsql/internal/source/espn"
	"sportsql/internal/store"
)

const usage = `usage: ingest [flags]

One-shot NBA ingest. Fetches games and box scores from ESPN for a date range
and upserts them idempotently into Postgres, then exits. Safe to re-run: every
write is an ON CONFLICT DO UPDATE upsert.

Flags:
  --from <YYYY-MM-DD>  First date to ingest (UTC calendar day).
                       Defaults to max(games.game_date) - 2 days, or today - 6
                       days on an empty database.
  --to   <YYYY-MM-DD>  Last date to ingest (inclusive). Defaults to today.
`

const (
	// boxScoreSleep paces consecutive box-score fetches.
	boxScoreSleep = 500 * time.Millisecond
	// daySleep paces consecutive days.
	daySleep = 1 * time.Second
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	settings := config.Load()
	if err := settings.ForIngest(); err != nil {
		slog.Error("config", "error", err.Error())
		return 2
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(settings.LogLevel),
	}))
	slog.SetDefault(logger)

	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	fromFlag := fs.String("from", "", "first date to ingest (YYYY-MM-DD); defaults to max(games.game_date) - 2 days")
	toFlag := fs.String("to", "", "last date to ingest, inclusive (YYYY-MM-DD); defaults to today")
	_ = fs.Parse(args)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := connect(ctx, settings.DatabaseURLRW)
	if err != nil {
		logger.Error("database connection failed", "error", err.Error())
		return 1
	}
	defer pool.Close()

	persistence := store.New(pool)
	src := espn.NewSource(espn.Options{
		SiteBaseURL: settings.ESPNBaseURL,
	})

	from, to, err := resolveRange(ctx, persistence, *fromFlag, *toFlag)
	if err != nil {
		logger.Error("could not resolve date range", "error", err.Error())
		return 2
	}
	if from.After(to) {
		logger.Error("invalid range", "from", from.Format("2006-01-02"), "to", to.Format("2006-01-02"))
		return 2
	}
	logger.Info("ingest starting",
		"from", from.Format("2006-01-02"), "to", to.Format("2006-01-02"),
		"database", redactCredentials(settings.DatabaseURLRW))

	totalGames := 0
	totalLines := 0
	// wafBlocked short-circuits box-score fetches once ESPN's WAF is serving a
	// challenge: every subsequent box score would fail the same way, so we stop
	// paying the per-game backoff and store games only. Re-running later (idempotent
	// upserts) fills in the player stats once the challenge clears.
	wafBlocked := false
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		if ctx.Err() != nil {
			logger.Warn("interrupted", "at", day.Format("2006-01-02"))
			return 1
		}
		games, err := src.GamesOn(ctx, day)
		if err != nil {
			logger.Error("fetching scoreboard failed", "date", day.Format("2006-01-02"), "error", err.Error())
			return 1
		}
		dayStats := make([]store.GameStats, 0, len(games))
		dayLines := 0
		for _, game := range games {
			var lines []source.PlayerLine
			if wafBlocked {
				// Skip; will be filled on a later run once the WAF clears.
			} else if lines, err = src.BoxScore(ctx, game.GameID); err != nil {
				if errors.Is(err, espn.ErrWAFChallenge) {
					wafBlocked = true
					logger.Warn("box scores blocked by ESPN WAF; storing games only this run",
						"game", game.GameID, "hint", "re-run from a residential connection or after the rate limit clears to fill in player stats")
				} else {
					logger.Warn("box score unavailable, storing game without player stats",
						"game", game.GameID, "status", game.Status, "error", err.Error())
				}
				lines = nil
			}
			dayLines += len(lines)
			dayStats = append(dayStats, store.GameStats{Game: game, Lines: lines})
			if !wafBlocked {
				pause(ctx, boxScoreSleep)
			}
		}
		if err := persistence.Save(ctx, dayStats); err != nil {
			logger.Error("saving day failed", "date", day.Format("2006-01-02"), "error", err.Error())
			return 1
		}
		totalGames += len(games)
		totalLines += dayLines
		logger.Info("day ingested", "date", day.Format("2006-01-02"), "games", len(games), "lines", dayLines)
		pause(ctx, daySleep)
	}
	if wafBlocked {
		logger.Warn("finished with games only; player stats pending a successful box-score run", "games", totalGames)
	}

	// data_as_of is the latest game date actually in the data (not the --to
	// flag), so a historical backfill never regresses it.
	dataAsOf, ok, err := persistence.MaxGameDate(ctx)
	if err != nil {
		logger.Error("reading max game_date failed", "error", err.Error())
		return 1
	}
	if !ok {
		logger.Warn("no games stored; leaving data_as_of unset")
		return 0
	}
	if err := persistence.SetDataAsOf(ctx, dataAsOf); err != nil {
		logger.Error("updating data_as_of failed", "error", err.Error())
		return 1
	}
	logger.Info("ingest finished",
		"games", totalGames, "player_lines", totalLines, "data_as_of", dataAsOf)
	return 0
}

// resolveRange fills in default --from/--to per the plan: --to defaults to
// today; --from defaults to max(game_date) - 2 days (or today - 6 on an empty
// database, giving an initial ~1 week).
func resolveRange(ctx context.Context, persistence *store.Store, fromFlag, toFlag string) (from, to time.Time, err error) {
	today := dayFloor(time.Now())
	if to, err = parseFlagDate(toFlag, today); err != nil {
		return time.Time{}, time.Time{}, err
	}
	if fromFlag == "" {
		if max, ok, err := persistence.MaxGameDate(ctx); err != nil {
			return time.Time{}, time.Time{}, err
		} else if ok {
			parsed, err := time.Parse("2006-01-02", max)
			if err != nil {
				return time.Time{}, time.Time{}, fmt.Errorf("max(game_date) %q: %w", max, err)
			}
			from = parsed.AddDate(0, 0, -2)
			return from, to, nil
		}
		from = today.AddDate(0, 0, -6)
		return from, to, nil
	}
	if from, err = parseFlagDate(fromFlag, today); err != nil {
		return time.Time{}, time.Time{}, err
	}
	return from, to, nil
}

func parseFlagDate(raw string, def time.Time) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return def, nil
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf("date %q is not YYYY-MM-DD", raw)
	}
	return parsed, nil
}

// pause sleeps for d unless the context is cancelled.
func pause(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func dayFloor(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
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

// connect opens a pgx pool and verifies it with a ping.
func connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("connect: timed out: %w", err)
		}
		return nil, fmt.Errorf("connect: %w", err)
	}
	return pool, nil
}

// redactCredentials hides the userinfo of a DSN for logging.
func redactCredentials(databaseURL string) string {
	schemeIndex := strings.Index(databaseURL, "://")
	atIndex := strings.LastIndex(databaseURL, "@")
	if schemeIndex < 0 || atIndex < schemeIndex {
		return databaseURL
	}
	return databaseURL[:schemeIndex+3] + "<redacted>" + databaseURL[atIndex:]
}
