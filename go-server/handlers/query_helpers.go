package handlers

import (
	"net/http"
	"strconv"
)

// Pagination encapsulates limit and offset query values for list endpoints.
type Pagination struct {
	Limit  int
	Offset int
}

// ParseQueryInt extracts an integer query parameter with fallback default and min/max clamping.
// If maxVal <= 0, no upper bound clamping is applied.
func ParseQueryInt(r *http.Request, key string, defaultVal, minVal, maxVal int) int {
	valStr := r.URL.Query().Get(key)
	if valStr == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(valStr)
	if err != nil || val < minVal {
		return defaultVal
	}
	if maxVal > 0 && val > maxVal {
		return maxVal
	}
	return val
}

// ParsePagination extracts standard limit and offset query parameters.
func ParsePagination(r *http.Request, defaultLimit, maxLimit int) Pagination {
	return Pagination{
		Limit:  ParseQueryInt(r, "limit", defaultLimit, 1, maxLimit),
		Offset: ParseQueryInt(r, "offset", 0, 0, 0),
	}
}

// ResolveTargetUserID determines the target user ID for a request:
// - If the ?username= query parameter is provided: queries the DB for the target user (returning sql.ErrNoRows if not found).
// - If ?username= is omitted: returns the authenticated caller's ID from request context with 0 DB queries.
func (h *Handler) ResolveTargetUserID(r *http.Request) (int64, error) {
	if username := r.URL.Query().Get("username"); username != "" {
		target, err := h.getUserByUsername(r.Context(), username)
		if err != nil {
			return 0, err
		}
		return target.ID, nil
	}

	userID, _ := UserIDFromContext(r.Context())
	return userID, nil
}
