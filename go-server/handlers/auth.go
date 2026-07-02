package handlers

import (
	"dbBackend/models"
	"encoding/json"
	"net/http"

	"github.com/uptrace/bun"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	DB *bun.DB
}

type LoginRequest struct {
	Username string `json:"username" validate:"required,min=3,max=50"`
	Password string `json:"password" validate:"required,min=8,max=72"`
}

// to be able to be registered as a handler in the server, the function prototype has to be exactly (http.ResponseWriter, *http.Request)
// so any further input parameters have to be in the receiver, which is why it's a struct
func (h *AuthHandler) CheckPassword(w http.ResponseWriter, r *http.Request) {
	request, err := DecodeAndValidate[LoginRequest](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var user models.User
	err = h.DB.NewSelect().
		Model(&user).
		Where("username = ?", request.Username).
		Scan(r.Context())
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	//	log.Printf("DEBUG RUNTIME CHECK:\n -> Incoming Password: [%s] (Length: %d)\n -> Stored DB Hash:    [%s] (Length: %d)\n",
	//		request.Password, len(request.Password), user.PWHash, len(user.PWHash))
	err = bcrypt.CompareHashAndPassword([]byte(user.PWHash), []byte(request.Password))
	if err != nil {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(user); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
