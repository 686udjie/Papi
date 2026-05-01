package main

import (
	"log"
	"net/http"
	"time"

	"papi/handlers"
	"papi/internal"
	"papi/storage"
)

func buildApp() *handlers.App {
	store, err := storage.NewPostgresStoreFromEnv()
	if err != nil {
		if err == storage.ErrMissingDatabaseURL {
			log.Println("DATABASE_URL not set; /api/homefeed will be unavailable")
		} else {
			log.Fatal(err)
		}
	}

	return &handlers.App{
		Store:         store,
		Client:        &http.Client{Timeout: 15 * time.Second},
		AuthConfirmed: internal.AuthConfirmedFromEnv(),
		Debug:         internal.DebugEnabledFromEnv(),
	}
}

func registerRoutes(app *handlers.App) {
	http.HandleFunc("/api/pin", app.Pin)
	http.HandleFunc("/api/login", app.Login)
	http.HandleFunc("/api/homefeed", app.Homefeed)
	http.HandleFunc("/api/search", app.Search)
	http.HandleFunc("/api/board", app.Board)
	http.HandleFunc("/api/user", app.User)
}

func runServer() {
	app := buildApp()
	registerRoutes(app)

	log.Println("Running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
