package handlers

import (
	"net/http"

	"github.com/uptrace/bun"
)

func RegisterRoutes(mux *http.ServeMux, db *bun.DB) {
	authHandler := &AuthHandler{DB: db}
	mux.HandleFunc("POST /api/login", authHandler.CheckPassword)
	registerHandler := &RegisterHandler{DB: db}
	mux.HandleFunc("POST /api/register", registerHandler.TryRegister)
}
