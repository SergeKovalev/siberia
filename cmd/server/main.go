package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"project/internal/config"
	"project/internal/googleapi"
	"project/internal/handlers"
)

func main() {
	log.SetOutput(os.Stdout)
	log.Println("Starting application...")

	cfg := config.LoadConfig()
	log.Printf("Configuration loaded: SpreadsheetID: %s", cfg.SpreadsheetID)

	sheetsService, err := googleapi.InitSheetsService(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize Google Sheets: %v", err)
	}

	if err := googleapi.VerifyAccess(sheetsService, cfg.SpreadsheetID); err != nil {
		log.Fatalf("Access verification failed: %v", err)
	}

	// Инициализация обработчиков
	handlers.InitHandlers(sheetsService, cfg)

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Server running on port %s", cfg.Port)
	log.Fatal(srv.ListenAndServe())
}
