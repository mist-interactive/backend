package main

import (
	"context"
	"dbBackend/db"
	"dbBackend/handlers"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof" //debug purposes
	"time"
)

func main() {
	postgres, err := db.InitDB()
	if err != nil {
		log.Fatal("error connecting to db")
	}
	defer postgres.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
	err = db.RunMigrations(ctx, postgres)
	if err != nil {
		cancel()
		log.Fatalf("%v\n", err)
	}
	cancel()

	mux := http.NewServeMux()
	mux.Handle("/debug/pprof/", http.DefaultServeMux)
	handlers.RegisterRoutes(mux, postgres)
	fmt.Println("Server starting on port 8080...")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal("error starting server")
	}
}

func dummy() {
	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"status": "ok", "message": "Go backend is alive!"}`)
	})

	fmt.Println("Server starting on port 8080...")

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("%v", err)
	}
}
