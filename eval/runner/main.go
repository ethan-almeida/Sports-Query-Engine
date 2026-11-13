// reads eval/questions.yaml, calls the
// /ask API for each case, and prints PASS/FAIL per question.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// Case is one eval question with the answer fragment it must contain.
type Case struct {
	Question string `yaml:"question"`
	Expected string `yaml:"expected"`
}

// askResponse mirrors the API's /ask JSON body.
type askResponse struct {
	Answer   string   `json:"answer"`
	SQL      string   `json:"sql"`
	Rows     [][]any  `json:"rows"`
	Columns  []string `json:"columns"`
	Cached   bool     `json:"cached"`
	DataAsOf string   `json:"data_as_of"`
}

const (
	throttle = 2 * time.Second
	filePath = "eval/questions.yaml"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	baseURL := envDefault("EVAL_BASE_URL", "http://localhost:8080")
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	n := fs.Int("n", 0, "run only the first N cases (0 means: refuse to run the full set, print usage)")
	all := fs.Bool("all", false, "run the entire questions.yaml (throttled); off by default on purpose")
	_ = fs.Parse(args)

	if *n <= 0 && !*all {
		fmt.Fprintln(os.Stderr, "refusing to run the full set by default. Use -n <k> for a subset, or -all to run everything (throttled).")
		return 2
	}

	cases, err := loadCases(filePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval: "+err.Error())
		return 2
	}
	if *n > 0 && *n < len(cases) {
		cases = cases[:*n]
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: 3 * time.Minute}
	passed, failed := 0, 0

	for _, c := range cases {
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "eval interrupted")
			return 1
		}
		ok, detail, err := runOne(ctx, client, baseURL, c)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERR ] %q: %v\n", c.Question, err)
			failed++
			continue
		}
		if ok {
			passed++
			fmt.Printf("[PASS] %q\n", c.Question)
		} else {
			failed++
			fmt.Printf("[FAIL] %q\n  expected fragment: %q\n  got: %s\n", c.Question, c.Expected, detail)
		}
		if len(cases) > 1 {
			pause(ctx, throttle)
		}
	}

	fmt.Printf("\neval summary: %d passed, %d failed (of %d)\n", passed, failed, len(cases))
	if failed > 0 {
		return 1
	}
	return 0
}

// runOne posts one question and reports whether the answer contains the
// expected fragment. detail is the answer on a mismatch.
func runOne(ctx context.Context, client *http.Client, baseURL string, c Case) (bool, string, error) {
	body, _ := json.Marshal(map[string]string{"question": c.Question})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/ask", bytes.NewReader(body))
	if err != nil {
		return false, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	var parsed askResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return false, "", fmt.Errorf("HTTP %d, non-JSON response: %.160s", resp.StatusCode, string(data))
	}
	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}

	needle := strings.ToLower(strings.TrimSpace(c.Expected))
	hay := strings.ToLower(parsed.Answer)
	if needle == "" {
		return false, "", fmt.Errorf("expected fragment is empty in questions.yaml")
	}
	if strings.Contains(hay, needle) {
		return true, "", nil
	}
	detail := parsed.Answer
	if len(detail) > 300 {
		detail = detail[:300] + "…"
	}
	extra := ""
	if parsed.Cached {
		extra = " (cached)"
	}
	return false, fmt.Sprintf("%s (sql=%q, rows=%d%s)", detail, parsed.SQL, len(parsed.Rows), extra), nil
}

// loadCases reads and parses the YAML questions file.
func loadCases(path string) ([]Case, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cases []Case
	if err := yaml.Unmarshal(raw, &cases); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("%s has no eval cases", path)
	}
	return cases, nil
}

func envDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
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
