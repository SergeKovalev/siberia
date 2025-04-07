package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"google.golang.org/api/sheets/v4"

	"project/internal/config"
	"project/internal/models"
)

func ProductionHandler(srv *sheets.Service, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var data models.ProductionData
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if strings.TrimSpace(data.FullName) == "" || strings.TrimSpace(data.PartAndOperation) == "" || strings.TrimSpace(data.TotalParts) == "" {
			http.Error(w, "Full name, part/operation and total parts are required", http.StatusBadRequest)
			return
		}

		if data.Date == "" {
			data.Date = time.Now().Format("2006-01-02")
		}

		if err := models.AppendProductionData(srv, cfg, data); err != nil {
			log.Printf("Error writing production data: %v", err)
			http.Error(w, "Failed to process data", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	}
}
