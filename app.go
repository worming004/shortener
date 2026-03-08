package main

import (
	"database/sql"
	"flag"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateCode(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// normalizeURL takes a raw URL string (as received verbatim from the request's
// query parameter, where '+' encodes a space and '%XX' are percent-encoded
// characters) and returns a properly encoded URL where spaces in the query
// string are represented as '%20' instead of '+'.
func normalizeURL(rawURL string) string {
	qIdx := strings.IndexByte(rawURL, '?')
	if qIdx == -1 {
		return rawURL
	}

	base := rawURL[:qIdx]
	rawQuery := rawURL[qIdx+1:]

	// url.ParseQuery decodes '+' as space and '%XX' as the corresponding
	// character (e.g. '%2B' becomes a literal '+').
	params, err := url.ParseQuery(rawQuery)
	if err != nil {
		log.Printf("normalizeURL: failed to parse query %q: %v", rawQuery, err)
		return rawURL
	}

	// Re-encode each key and value: url.QueryEscape encodes spaces as '+',
	// so we replace those '+' with '%20'. Literal '+' characters are encoded
	// as '%2B' by url.QueryEscape and are therefore unaffected by the replace.
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(params))
	for _, k := range keys {
		ek := strings.ReplaceAll(url.QueryEscape(k), "+", "%20")
		for _, v := range params[k] {
			ev := strings.ReplaceAll(url.QueryEscape(v), "+", "%20")
			parts = append(parts, ek+"="+ev)
		}
	}

	if len(parts) == 0 {
		return base
	}
	return base + "?" + strings.Join(parts, "&")
}

func shortenHandler(w http.ResponseWriter, r *http.Request) {
	// Use the decoded query to check for a missing parameter; url.Values.Get
	// handles percent-encoded parameter names (e.g. "ur%6C") transparently.
	if r.URL.Query().Get("url") == "" {
		http.Error(w, "Missing url parameter", http.StatusBadRequest)
		return
	}

	// Read the raw value of the 'url' parameter directly from the query string
	// so that '+' characters (which encode spaces in query params) are preserved
	// and handled explicitly by normalizeURL, rather than being silently decoded
	// to spaces by url.Values.Get(). The parameter name itself is expected to be
	// the literal ASCII string "url" and will not be percent-encoded in practice.
	rawURLValue := ""
	rawQuery := r.URL.RawQuery
	for rawQuery != "" {
		var part string
		part, rawQuery, _ = strings.Cut(rawQuery, "&")
		k, v, ok := strings.Cut(part, "=")
		if ok && k == "url" {
			rawURLValue = v
			break
		}
	}

	if !strings.HasPrefix(rawURLValue, "http://") && !strings.HasPrefix(rawURLValue, "https://") {
		log.Println("adding https prefix")
		rawURLValue = "https://" + rawURLValue
	}

	normalizedURL := normalizeURL(rawURLValue)

	code := generateCode(8)

	_, err := db.Exec(
		"INSERT INTO short_urls (code, original_url, created_at) VALUES (?, ?, ?)",
		code, normalizedURL, time.Now(),
	)

	if err != nil {
		http.Error(w, "DB error", http.StatusInternalServerError)
		return
	}

	log.Printf("generate url with code %v", code)

	w.Write([]byte(code))
}

func redirectHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Path[1:]

	var url string
	err := db.QueryRow(
		"SELECT original_url FROM short_urls WHERE code = ?",
		code,
	).Scan(&url)

	if err != nil {
		http.NotFound(w, r)
		return
	}

	log.Printf("redirect for code %v, url: %v", code, url)

	http.Redirect(w, r, url, http.StatusFound)
}

func cleanupWorker() {
	for {
		time.Sleep(10 * time.Hour * 24)

		_, err := db.Exec(`
			DELETE FROM short_urls
			WHERE created_at < datetime('now', '-3 years')
		`)

		if err != nil {
			log.Println("Cleanup error:", err)
		} else {
			log.Println("Cleanup completed")
		}
	}
}

func main() {
	var port string
	flag.StringVar(&port, "port", "8080", "port to bind the server to")
	flag.Parse()

	var err error
	db, err = sql.Open("sqlite3", "./shortener.db")
	if err != nil {
		log.Fatal(err)
	}

	// Create table if not exists
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS short_urls (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		code TEXT UNIQUE,
		original_url TEXT NOT NULL,
		created_at DATETIME
	);
	`)
	if err != nil {
		log.Fatal(err)
	}

	// Start cleanup worker
	go cleanupWorker()

	// HTTP handlers
	http.HandleFunc("/shorten", shortenHandler)
	http.HandleFunc("/", redirectHandler)

	addr := ":" + port
	log.Println("Server running on", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
