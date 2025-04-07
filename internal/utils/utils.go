package utils

import (
	"net/http"
)

func EnableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

func ColumnToLetter(col int) string {
	letter := ""
	for col > 0 {
		col--
		letter = string(rune('A'+(col%26))) + letter
		col = col / 26
	}
	return letter
}
