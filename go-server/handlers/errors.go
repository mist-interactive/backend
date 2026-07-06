package handlers

import (
	"net/http"
	"strings"

	"github.com/uptrace/bun/driver/pgdriver"
)

func HandleDBConflict(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if pgErr, ok := err.(pgdriver.Error); ok {
		if pgErr.Field('C') == "23505" {
			errMessage := pgErr.Error()
			if strings.Contains(errMessage, "username") {
				http.Error(w, "Conflict: That username is already registered.", http.StatusConflict)
				return true
			}
			if strings.Contains(errMessage, "email") {
				http.Error(w, "Conflict: That email address is already registered.", http.StatusConflict)
				return true
			}
		}
	}
	return false
}
