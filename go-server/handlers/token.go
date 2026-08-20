package handlers

import (
	"encoding/json"
	"net/http"
)

func TokenHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Internal Server Error: Context missing user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message":  "Welcome to the Memoir 3167 game!",
		"username": user.Username,
		"email":    user.Email,
	})
}
