package handlers

import (
	"fmt"
	"net/http"
	"strconv"
)

// ParsePathInt64 extracts and parses a base-10 int64 path parameter (e.g. {id}).
// Returns an error if the parameter is empty or not a valid int64.
func ParsePathInt64(r *http.Request, key string) (int64, error) {
	valStr := r.PathValue(key)
	if valStr == "" {
		return 0, fmt.Errorf("missing path parameter %q", key)
	}
	val, err := strconv.ParseInt(valStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, valStr, err)
	}
	return val, nil
}
