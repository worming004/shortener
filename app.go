package main

import (
	"database/sql"
	"flag"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateCode(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// App holds the application dependencies.
type App struct {
	db *sql.DB
}

// NewApp creates an App with the given database connection.
func NewApp(db *sql.DB) *App {
	return &App{db: db}
}

// Handler returns an http.Handler with all routes registered.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/shorten", a.shortenHandler)
	mux.HandleFunc("/", a.redirectHandler)
	return mux
}

func (a *App) shortenHandler(w http.ResponseWriter, r *http.Request) {
	urlValue := r.URL.Query().Get("url")
	if urlValue == "" {
		http.Error(w, "Missing url parameter", http.StatusBadRequest)
		return
	}

	if !strings.HasPrefix(urlValue, "http://") && !strings.HasPrefix(urlValue, "https://") {
		log.Println("adding https prefix")
		urlValue = "https://" + urlValue
	}

	code := generateCode(8)

	_, err := a.db.Exec(
		"INSERT INTO short_urls (code, original_url, created_at) VALUES (?, ?, ?)",
		code, urlValue, time.Now(),
	)

	if err != nil {
		http.Error(w, "DB error", http.StatusInternalServerError)
		return
	}

	log.Printf("generate url with code %v", code)

	w.Write([]byte(code))
}

func (a *App) redirectHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Path[1:]

	var originalURL string
	err := a.db.QueryRow(
		"SELECT original_url FROM short_urls WHERE code = ?",
		code,
	).Scan(&originalURL)

	if err != nil {
		http.NotFound(w, r)
		return
	}

	log.Printf("redirect for code %v, url: %v", code, originalURL)

	http.Redirect(w, r, originalURL, http.StatusFound)
}

func (a *App) cleanupWorker() {
	for {
		time.Sleep(10 * time.Hour * 24)

		_, err := a.db.Exec(`
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

// initDB creates the required schema in db if it does not exist yet.
func initDB(db *sql.DB) error {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS short_urls (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		code TEXT UNIQUE,
		original_url TEXT NOT NULL,
		created_at DATETIME
	);
	`)
	return err
}

func main() {
	var port string
	flag.StringVar(&port, "port", "8080", "port to bind the server to")
	flag.Parse()

	db, err := sql.Open("sqlite3", "./shortener.db")
	if err != nil {
		log.Fatal(err)
	}

	if err := initDB(db); err != nil {
		log.Fatal(err)
	}

	app := NewApp(db)

	// Start cleanup worker
	go app.cleanupWorker()

	addr := ":" + port
	log.Println("Server running on", addr)
	log.Fatal(http.ListenAndServe(addr, app.Handler()))
}
