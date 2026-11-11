package espn

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"sportsql/internal/source"
)

func fakeSleep(ctx context.Context, d time.Duration) error {
	return ctx.Err()
}

func wafChallengePage() string {
	return "<html><head><title></title></head><body><script>/* aws waf challenge */</script></body></html>"
}

func TestClientRetriesOn429ThenSucceeds(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"events":[]}`)
	}))
	defer server.Close()

	c := newTestClient(Options{SiteBaseURL: server.URL, WebBaseURL: server.URL}, fakeSleep)
	body, err := c.get(context.Background(), server.URL+"/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(body) != `{"events":[]}` {
		t.Errorf("body = %q", body)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (one 429 + one success)", calls)
	}
}

func TestClientRetriesOnWAFChallengeThenSucceeds(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(http.StatusAccepted) // WAF challenge status
			fmt.Fprint(w, wafChallengePage())
			return
		}
		fmt.Fprint(w, "<html>window['__espnfitt__']={\"page\":{\"content\":{\"gamepackage\":{\"bxscr\":[]}}}}</html>")
	}))
	defer server.Close()

	c := newTestClient(Options{WebBaseURL: server.URL}, fakeSleep)
	body, err := c.get(context.Background(), server.URL+"/nba/boxscore/_/gameId/1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(string(body), "bxscr") {
		t.Errorf("body missing bxscr: %q", string(body)[:min(len(body), 80)])
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (two challenges + success)", calls)
	}
}

func TestClientGivesUpAfterMaxAttempts(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, wafChallengePage())
	}))
	defer server.Close()

	c := newTestClient(Options{WebBaseURL: server.URL}, fakeSleep)
	_, err := c.get(context.Background(), server.URL+"/nba/boxscore/_/gameId/1")
	if err == nil {
		t.Fatal("persistent challenge must eventually error")
	}
	if calls != maxAttempts {
		t.Errorf("calls = %d, want %d", calls, maxAttempts)
	}
}

func TestClientReturnsHTTPErrorOn404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"code":404}`)
	}))
	defer server.Close()

	c := newTestClient(Options{SiteBaseURL: server.URL}, fakeSleep)
	_, err := c.get(context.Background(), server.URL+"/missing")
	if err == nil {
		t.Fatal("404 must error")
	}
	if he := IsHTTPError(err); he == nil || he.status != http.StatusNotFound {
		t.Errorf("error = %v, want httpError status 404", err)
	}
}

func TestClientHonorsContextCancellation(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	c := newTestClient(Options{SiteBaseURL: server.URL}, fakeSleep)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := c.get(ctx, server.URL+"/slow")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("timed-out get must error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("get did not return after context cancel")
	}
}

// TestIsWAFChallenge pins the detection rule: 202 is always a challenge; a 200
// with a tiny WAF stub is a challenge; a real page is not.
func TestIsWAFChallenge(t *testing.T) {
	if !isWAFChallenge(http.StatusAccepted, []byte("")) {
		t.Error("202 must be a challenge")
	}
	if !isWAFChallenge(http.StatusOK, []byte("<html>waf stub</html>")) {
		t.Error("small 200 waf stub must be a challenge")
	}
	if isWAFChallenge(http.StatusOK, []byte(strings.Repeat("x", 10000))) {
		t.Error("a large 200 page must not be a challenge")
	}
	if isWAFChallenge(http.StatusOK, []byte("<html>full box score page with lots of data</html>")) {
		t.Error("a real 200 page must not be a challenge")
	}
	if isWAFChallenge(http.StatusNotFound, []byte(`{"code":404}`)) {
		t.Error("404 must not be a challenge")
	}
}

func TestGamesOnViaSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("dates") != "20220215" {
			t.Errorf("dates = %q, want 20220215", r.URL.Query().Get("dates"))
		}
		w.Header().Set("Content-Type", "application/json")
		body, err := readFixtureFile("testdata/scoreboard.json")
		if err != nil {
			t.Errorf("fixture: %v", err)
		}
		w.Write(body)
	}))
	defer server.Close()

	s := &Source{client: newTestClient(Options{SiteBaseURL: server.URL}, fakeSleep)}
	games, err := s.GamesOn(context.Background(), time.Date(2022, time.February, 15, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GamesOn: %v", err)
	}
	if len(games) == 0 {
		t.Fatal("no games returned")
	}
	// Every game must carry a season and a status in the allowed set.
	for _, g := range games {
		if g.Season == "" {
			t.Errorf("game %s season empty", g.GameID)
		}
		if g.Status != "scheduled" && g.Status != "live" && g.Status != "final" &&
			g.Status != "postponed" && g.Status != "canceled" {
			t.Errorf("game %s status %q not allowed", g.GameID, g.Status)
		}
	}
}

func TestBoxScoreViaSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/nba/boxscore/_/gameId/401909952") {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, err := readFixtureFile("testdata/boxscore.html")
		if err != nil {
			t.Errorf("fixture: %v", err)
		}
		w.Write(body)
	}))
	defer server.Close()

	s := &Source{client: newTestClient(Options{WebBaseURL: server.URL}, fakeSleep)}
	lines, err := s.BoxScore(context.Background(), "401909952")
	if err != nil {
		t.Fatalf("BoxScore: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("no lines returned")
	}
	for _, line := range lines {
		if line.GameID != "401909952" {
			t.Errorf("line gameID = %q", line.GameID)
		}
		if line.Player.Code == "" || line.Player.FullName == "" || line.Player.TeamCode == "" {
			t.Errorf("incomplete player: %+v", line.Player)
		}
	}
}

func TestSourceImplementsInterface(t *testing.T) {
	var _ source.Source = (*Source)(nil)
}

func readFixtureFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
