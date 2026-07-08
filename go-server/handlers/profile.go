package handlers

import (
	"dbBackend/models"
	"encoding/json"
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/uptrace/bun"
)

type ProfileHandler struct {
	DB *bun.DB
}

type ProfileResponse struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	TotalWins   int32  `json:"total_wins"`
	TotalLosses int32  `json:"total_losses"`
	AvatarURL   string `json:"avatar_url"`
}

type UpdateProfileInput struct {
	Username *string `json:"username,omitempty"`
	Email    *string `json:"email,omitempty"`
}

// GetProfile godoc
// @Summary      Get Player Profile
// @Description  Fetch profile parameters, statistics (wins/losses), and metadata for the current authenticated user session.
// @Tags         profile
// @Produce      json
// @Security     CookieAuth
// @Success      200  {object}  handlers.ProfileResponse
// @Failure      500  {string}  string  "Internal Server Error - Context missing user state"
// @Router       /api/profile [get]
func (h *ProfileHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(userContextKey).(*models.User)
	if !ok {
		http.Error(w, "User context missing", http.StatusInternalServerError)
		return
	}

	resp := ProfileResponse{
		ID:          user.ID,
		Username:    user.Username,
		Email:       user.Email,
		TotalWins:   user.TotalWins,
		TotalLosses: user.TotalLosses,
		AvatarURL:   user.AvatarURL,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// UpdateProfile godoc
// @Summary      Update Player Profile Fields
// @Description  Perform partial updates (PATCH style validation) on specific authorized profile parameters like username or email.
// @Tags         profile
// @Accept       json
// @Produce      json
// @Security     CookieAuth
// @Param        profile  body      handlers.UpdateProfileInput  true  "Updatable Profile Properties"
// @Success      200      {object}  models.User
// @Failure      400      {string}  string  "Invalid JSON payload, validation breach, or empty field mismatch"
// @Failure      500      {string}  string  "Internal Server Error - Database unique constraint or persistence failure"
// @Router       /api/profile [patch]
func (h *ProfileHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(userContextKey).(*models.User)
	if !ok {
		http.Error(w, "User context missing", http.StatusInternalServerError)
		return
	}
	var input UpdateProfileInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}
	hasChanges := false
	if changed, err := processPatchField[RegisterRequest](input.Username, "Username", &user.Username, validate); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if changed {
		hasChanges = true
	}
	if changed, err := processPatchField[RegisterRequest](input.Email, "Email", &user.Email, validate); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if changed {
		hasChanges = true
	}
	if hasChanges {
		_, err := h.DB.NewUpdate().
			Model(user).
			Column("username", "email").
			Where("id = ?", user.ID).
			Exec(r.Context())
		if err != nil {
			if HandleDBConflict(w, err) {
				return
			}
			http.Error(w, "Failed to update profile records", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(user)
}

func processPatchField[T any](fieldValue *string, fieldName string, dbTarget *string, validate *validator.Validate) (bool, error) {
	if fieldValue == nil || *fieldValue == "" {
		return false, nil
	}
	tags := GetValidationTag[T](fieldName, "required,")
	if err := validate.Var(*fieldValue, tags); err != nil {
		return false, err
	}
	if *dbTarget == *fieldValue {
		return false, nil
	}
	*dbTarget = *fieldValue
	return true, nil
}
