package handlers

import (
	"dbBackend/models"
	"net/http"
	"strings"

	"github.com/uptrace/bun"
	"golang.org/x/crypto/bcrypt"
)

type RegisterHandler struct {
	DB *bun.DB
}

type RegisterRequest struct {
	Username string `json:"username" validate:"required,min=3,max=50,username_safety"`
	Password string `json:"password" validate:"required,min=8,max=72,password_complexity"`
	Email    string `json:"email" validate:"required,email,max=255"`
}

func (h *RegisterHandler) TryRegister(w http.ResponseWriter, r *http.Request) {
	request, err := DecodeAndValidate[RegisterRequest](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	hashedBytes, _ := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	newUser := models.User{
		Username: request.Username,
		Email:    strings.ToLower(request.Email),
		PWHash:   string(hashedBytes),
	}
	_, err = h.DB.NewInsert().
		Model(&newUser).
		Exec(r.Context())
	if err != nil {
		HandleDBError(w, err, "Insert User")
		return
	}
	w.WriteHeader(http.StatusCreated)
}
