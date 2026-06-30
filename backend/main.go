package main

import (
	"context"
	"dbBackend/db"
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	postgres, err := db.InitDB()
	if err != nil {
		log.Fatal("error connecting to db")
	}
	defer postgres.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*120)
	defer cancel()
	err = db.RunMigrations(ctx, postgres)
	cancel()
	dummy()
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
