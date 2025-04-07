package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/sheets/v4"

	"project/internal/config"
	"project/internal/models"
)

func TimesheetHandler(srv *sheets.Service, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var data models.TimesheetData
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if strings.TrimSpace(data.FullName) == "" || strings.TrimSpace(data.Hours) == "" {
			http.Error(w, "Full name and hours are required", http.StatusBadRequest)
			return
		}

		if data.Date == "" {
			data.Date = time.Now().Format("2006-01-02")
		} else {
			// Validate date format
			if _, err := time.Parse("2006-01-02", data.Date); err != nil {
				http.Error(w, "Invalid date format, expected YYYY-MM-DD", http.StatusBadRequest)
				return
			}
		}

		if _, err := strconv.ParseFloat(data.Hours, 64); err != nil {
			http.Error(w, "Hours must be a number", http.StatusBadRequest)
			return
		}

		if err := models.AppendTimesheetData(srv, cfg.SpreadsheetID, data); err != nil {
			log.Printf("Error writing timesheet data: %v", err)
			http.Error(w, fmt.Sprintf("Failed to process data: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	}
}
