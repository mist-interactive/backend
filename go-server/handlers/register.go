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

// TryRegister godoc
// @Summary      Register New User Account
// @Description  Create a new user account profile. Automatically hashes user passwords using bcrypt and stores emails in lowercase.
// @Tags         auth
// @Accept       json
// @Param        register  body  handlers.RegisterRequest  true  "Registration Form Data"
// @Success      201       "Account created successfully"
// @Failure      400       {string}  string  "Invalid input layout or validation constraints failed (username_safety/password_complexity)"
// @Failure      409       {string}  string  "Conflict - Username or Email already registered"
// @Failure      500       {string}  string  "Internal Server Error - Database writing failure"
// @Router       /api/register [post]
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
		if HandleDBConflict(w, err) {
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
