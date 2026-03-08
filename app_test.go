package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no query string",
			input: "https://example.com/path",
			want:  "https://example.com/path",
		},
		{
			name:  "plus signs decoded as spaces and re-encoded as %20",
			input: "https://example.com?q=hello+world",
			want:  "https://example.com?q=hello%20world",
		},
		{
			name:  "percent-encoded special chars preserved",
			input: "https://example.com?q=foo%23bar%3Abaz",
			want:  "https://example.com?q=foo%23bar%3Abaz",
		},
		{
			name:  "literal plus (originally %2B) stays as %2B",
			input: "https://example.com?price=%240.96%2B%241.00",
			want:  "https://example.com?price=%240.96%2B%241.00",
		},
		{
			name: "full poker hand URL from issue report",
			input: "https://handreplayer.poker.craftlabit.be?hand=PokerStars+Hand+%23256159580493%3A+Zoom+Tournament+%233889643742%2C+%240.96%2B%241.00%2B%240.24+USD+Hold%27em+No+Limit+-+Level+IV+%2830%2F60%29",
			want:  "https://handreplayer.poker.craftlabit.be?hand=PokerStars%20Hand%20%23256159580493%3A%20Zoom%20Tournament%20%233889643742%2C%20%240.96%2B%241.00%2B%240.24%20USD%20Hold%27em%20No%20Limit%20-%20Level%20IV%20%2830%2F60%29",
		},
		{
			name:  "newline encoded as %0A is preserved",
			input: "https://example.com?text=line1%0Aline2",
			want:  "https://example.com?text=line1%0Aline2",
		},
		{
			name:  "multiple query params are sorted",
			input: "https://example.com?z=last&a=first",
			want:  "https://example.com?a=first&z=last",
		},
		{
			name:  "empty query string after question mark",
			input: "https://example.com?",
			want:  "https://example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeURL(tt.input)
			if got != tt.want {
				t.Errorf("normalizeURL(%q)\n  got  %q\n  want %q", tt.input, got, tt.want)
			}
		})
	}
}

func setupTestDB(t *testing.T) {
	t.Helper()
	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS short_urls (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			code TEXT UNIQUE,
			original_url TEXT NOT NULL,
			created_at DATETIME
		);
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { db.Close() })
}

func TestShortenAndRedirect_EncodedURL(t *testing.T) {
	setupTestDB(t)

	// The URL to shorten uses '+' for spaces and '%XX' for special chars,
	// as it would arrive verbatim in the shortener request's query string.
	rawParam := "https://handreplayer.poker.craftlabit.be?hand=PokerStars+Hand+%23256159580493%3A+Zoom+Tournament+%233889643742%2C+%240.96%2B%241.00%2B%240.24+USD+Hold%27em+No+Limit"

	// Step 1: shorten
	shortenReq := httptest.NewRequest(http.MethodGet, "/shorten?url="+rawParam, nil)
	shortenRec := httptest.NewRecorder()
	shortenHandler(shortenRec, shortenReq)

	if shortenRec.Code != http.StatusOK {
		t.Fatalf("shorten: expected 200, got %d", shortenRec.Code)
	}
	code := strings.TrimSpace(shortenRec.Body.String())
	if len(code) == 0 {
		t.Fatal("shorten: empty code returned")
	}

	// Step 2: redirect
	redirectReq := httptest.NewRequest(http.MethodGet, "/"+code, nil)
	redirectRec := httptest.NewRecorder()
	redirectHandler(redirectRec, redirectReq)

	if redirectRec.Code != http.StatusFound {
		t.Fatalf("redirect: expected 302, got %d", redirectRec.Code)
	}

	location := redirectRec.Header().Get("Location")

	// Spaces must be encoded as %20, not as '+' or literal spaces.
	if strings.Contains(location, "+") {
		t.Errorf("Location header contains '+' (unencoded space): %s", location)
	}
	if strings.Contains(location, " ") {
		t.Errorf("Location header contains literal space: %s", location)
	}

	// The literal plus signs (originally %2B) must remain as %2B.
	if !strings.Contains(location, "%2B") {
		t.Errorf("Location header is missing %%2B for literal plus signs: %s", location)
	}

	// The hash character (originally %23) must remain encoded.
	if strings.Contains(location, "#") {
		t.Errorf("Location header contains unencoded '#': %s", location)
	}
	if !strings.Contains(location, "%23") {
		t.Errorf("Location header is missing %%23 for hash character: %s", location)
	}

	// The scheme must be intact.
	if !strings.HasPrefix(location, "https://") {
		t.Errorf("Location header does not start with https://: %s", location)
	}
}

func TestShortenHandler_MissingParam(t *testing.T) {
	setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/shorten", nil)
	rec := httptest.NewRecorder()
	shortenHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestShortenHandler_AddsHTTPSPrefix(t *testing.T) {
	setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/shorten?url=example.com%2Fpath", nil)
	rec := httptest.NewRecorder()
	shortenHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	code := strings.TrimSpace(rec.Body.String())

	redirectReq := httptest.NewRequest(http.MethodGet, "/"+code, nil)
	redirectRec := httptest.NewRecorder()
	redirectHandler(redirectRec, redirectReq)

	location := redirectRec.Header().Get("Location")
	if !strings.HasPrefix(location, "https://") {
		t.Errorf("expected https:// prefix in location, got: %s", location)
	}
}
