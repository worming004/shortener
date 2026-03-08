package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// newTestApp creates an App backed by an in-memory SQLite database.
func newTestApp(t *testing.T) *App {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := initDB(db); err != nil {
		t.Fatalf("initDB: %v", err)
	}

	return NewApp(db)
}

// shortenURL is a test helper that calls /shorten and returns the generated code.
func shortenURL(t *testing.T, app *App, rawURL string) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/shorten?url="+url.QueryEscape(rawURL), nil)
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("shorten: expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	code := w.Body.String()
	if len(code) != 8 {
		t.Fatalf("shorten: expected 8-char code, got %q", code)
	}

	return code
}

func TestShortenHandler_MissingURL(t *testing.T) {
	app := newTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/shorten", nil)
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestShortenHandler_ReturnsEightCharCode(t *testing.T) {
	app := newTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/shorten?url=https://example.com", nil)
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	code := w.Body.String()
	if len(code) != 8 {
		t.Errorf("expected 8-char code, got %q (len=%d)", code, len(code))
	}

	for _, ch := range code {
		if !strings.ContainsRune(charset, ch) {
			t.Errorf("code %q contains unexpected character %q", code, ch)
		}
	}
}

func TestShortenHandler_AddsHTTPSPrefix(t *testing.T) {
	app := newTestApp(t)

	code := shortenURL(t, app, "example.com")

	req := httptest.NewRequest(http.MethodGet, "/"+code, nil)
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}

	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://") {
		t.Errorf("expected https:// prefix in Location, got %q", loc)
	}
}

func TestRedirectHandler_NotFound(t *testing.T) {
	app := newTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/unknowncode", nil)
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestShortenAndRedirect_SimpleURL(t *testing.T) {
	app := newTestApp(t)
	originalURL := "https://example.com/path?foo=bar"

	code := shortenURL(t, app, originalURL)

	req := httptest.NewRequest(http.MethodGet, "/"+code, nil)
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}

	if got := w.Header().Get("Location"); got != originalURL {
		t.Errorf("Location: want %q, got %q", originalURL, got)
	}
}

// TestShortenAndRedirect_SpecialCharsURL verifies that a long URL containing
// percent-encoded characters (like a poker hand replay link) is stored and
// redirected without any additional encoding or corruption.
func TestShortenAndRedirect_SpecialCharsURL(t *testing.T) {
	app := newTestApp(t)

	originalURL := "https://handreplayer.poker.craftlabit.be/?hand=PokerStars%20Hand%20%23256029604529%3A%20Tournament%20%233885363705%2C%20%240.98%2B%240.12%20USD%20Hold%27em%20No%20Limit%20-%20Level%20VII%20%2860%2F120%29%20-%202025%2F05%2F06%2016%3A21%3A43%20CET%20%5B2025%2F05%2F06%2010%3A21%3A43%20ET%5D%0ATable%20%273885363705%204%27%209-max%20Seat%20%239%20is%20the%20button%0ASeat%201%3A%20mayarasander%20%287424%20in%20chips%29%0ASeat%202%3A%20dew_tornado%20%2812840%20in%20chips%29%0ASeat%203%3A%20Worming004%20%285224%20in%20chips%29"

	code := shortenURL(t, app, originalURL)

	req := httptest.NewRequest(http.MethodGet, "/"+code, nil)
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}

	if got := w.Header().Get("Location"); got != originalURL {
		t.Errorf("Location: want %q, got %q", originalURL, got)
	}
}

// TestShortenTwice verifies that two different codes are generated for the
// same URL when shortened twice.
func TestShortenTwice_DifferentCodes(t *testing.T) {
	app := newTestApp(t)

	code1 := shortenURL(t, app, "https://example.com")
	code2 := shortenURL(t, app, "https://example.com")

	if code1 == code2 {
		t.Errorf("expected different codes for two shortenings, but got %q twice", code1)
	}
}
