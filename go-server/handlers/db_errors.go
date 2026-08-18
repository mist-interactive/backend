package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/uptrace/bun/driver/pgdriver"
)

// HandleDBError inspects the database error and writes the appropriate HTTP response.
func HandleDBError(w http.ResponseWriter, err error, resourceName string) {
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, fmt.Sprintf("'%s' not found", resourceName), http.StatusNotFound)
		return
	}

	if pgErr, ok := err.(pgdriver.Error); ok {
		errMsg := pgErr.Error()
		switch pgErr.Field('C') { // 'C' is SQLSTATE code
		case "23505": // unique_violation
			http.Error(w, fmt.Sprintf("Conflict: '%s' , error: '%s'", resourceName, errMsg), http.StatusConflict)
			return

		case "23503": // foreign_key_violation
			http.Error(w, fmt.Sprintf("Referenced record does not exist, '%s'", errMsg), http.StatusBadRequest)
			return

		case "23514": // check_violation
			http.Error(w, fmt.Sprintf("Invalid data violates constraints, '%s'", errMsg), http.StatusBadRequest)
			return
		}
	}

	//fallback
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}
