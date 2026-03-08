package main

import (
	"database/sql"
	"flag"
	"log"
	"math/rand"
	"net/http"
	"net/url"
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

func shortenHandler(w http.ResponseWriter, r *http.Request) {
	urlValue := r.URL.Query().Get("url")
	if urlValue == "" {
		http.Error(w, "Missing url parameter", http.StatusBadRequest)
		return
	}

	if !strings.HasPrefix(urlValue, "http://") && !strings.HasPrefix(urlValue, "https://") {
		log.Println("adding https prefix")
		urlValue = "https://" + urlValue
	}
	urlEncoded := url.QueryEscape(urlValue)

	code := generateCode(8)

	_, err := db.Exec(
		"INSERT INTO short_urls (code, original_url, created_at) VALUES (?, ?, ?)",
		code, urlEncoded, time.Now(),
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
