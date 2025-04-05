package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type Config struct {
	Port            string `json:"port"`
	SpreadsheetID   string `json:"spreadsheetID"`
	ProductionSheet string `json:"productionSheet"`
	TimesheetSheet  string `json:"timesheetSheet"`
}

type ProductionData struct {
	Date             string `json:"date"`
	FullName         string `json:"fullName"`
	PartAndOperation string `json:"partAndOperation"`
	TotalParts       string `json:"totalParts"`
	Defective        string `json:"defective"`
	GoodParts        string `json:"goodParts"`
	Notes            string `json:"notes"`
}

type TimesheetData struct {
	Date     string `json:"date"`
	FullName string `json:"fullName"`
	Hours    string `json:"hours"`
}

var (
	config        Config
	sheetsService *sheets.Service
)

func main() {
	log.SetOutput(os.Stdout)
	log.Println("Starting application...")

	loadConfig()
	log.Printf("Configuration loaded: %+v", config)

	if err := initSheetsService(); err != nil {
		log.Fatalf("Failed to initialize Google Sheets: %v", err)
	}

	// Perform test write operation
	if err := testWriteOperation(); err != nil {
		log.Fatalf("Test write operation failed: %v", err)
	}

	http.HandleFunc("/submit-production", enableCORS(productionHandler))
	http.HandleFunc("/submit-timesheet", enableCORS(timesheetHandler))
	http.HandleFunc("/health", healthHandler)

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	srv := &http.Server{
		Addr:         ":" + config.Port,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Server running on port %s", config.Port)
	log.Fatal(srv.ListenAndServe())
}

func testWriteOperation() error {
	testData := ProductionData{
		Date:             time.Now().Format("2006-01-02"),
		FullName:         "TEST USER",
		PartAndOperation: "TEST PART/OPERATION",
		TotalParts:       "1",
		Defective:        "0",
		GoodParts:        "1",
		Notes:            "TEST RECORD",
	}

	log.Println("Performing test write operation...")
	if err := appendProductionData(testData); err != nil {
		return fmt.Errorf("test write failed: %v", err)
	}
	log.Println("Test write operation successful")
	return nil
}

func loadConfig() {
	// Default values
	config = Config{
		Port:            "8080",
		ProductionSheet: "Production",
		TimesheetSheet:  "Timesheet",
	}

	// Load from config file if exists
	if file, err := os.Open("config.json"); err == nil {
		defer file.Close()
		if err := json.NewDecoder(file).Decode(&config); err != nil {
			log.Printf("Error reading config.json: %v", err)
		}
	}

	// Override with environment variables
	if envID := os.Getenv("SPREADSHEET_ID"); envID != "" {
		config.SpreadsheetID = envID
	}

	// Validation
	if config.SpreadsheetID == "" {
		log.Fatal("SpreadsheetID must be specified in config.json or SPREADSHEET_ID environment variable")
	}
}

func initSheetsService() error {
	ctx := context.Background()

	creds, err := loadCredentials()
	if err != nil {
		return fmt.Errorf("failed to load credentials: %v", err)
	}

	// Validate credentials by attempting to create JWT config
	conf, err := google.JWTConfigFromJSON(creds, sheets.SpreadsheetsScope)
	if err != nil {
		return fmt.Errorf("invalid credentials: %v", err)
	}

	// Create service with retry
	var service *sheets.Service
	for i := 0; i < 3; i++ {
		service, err = sheets.NewService(ctx, option.WithHTTPClient(conf.Client(ctx)))
		if err == nil {
			break
		}
		time.Sleep(time.Second * time.Duration(i+1))
	}
	if err != nil {
		return fmt.Errorf("failed to create sheets service: %v", err)
	}

	sheetsService = service
	return nil
}

func loadCredentials() ([]byte, error) {
	// Try environment variable first
	if base64Data := os.Getenv("GOOGLE_CREDENTIALS_BASE64"); base64Data != "" {
		data, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 credentials: %v", err)
		}
		log.Println("Using credentials from GOOGLE_CREDENTIALS_BASE64")
		return data, nil
	}

	// Try credentials file
	if data, err := os.ReadFile("credentials.json"); err == nil {
		log.Println("Using credentials from credentials.json")
		return data, nil
	}

	return nil, fmt.Errorf("no credentials provided (neither GOOGLE_CREDENTIALS_BASE64 nor credentials.json)")
}

func appendProductionData(data ProductionData) error {
	values := [][]interface{}{
		{
			data.Date,
			data.FullName,
			data.PartAndOperation,
			data.TotalParts,
			data.Defective,
			data.GoodParts,
			data.Notes,
			time.Now().Format(time.RFC3339),
		},
	}

	// Prepare request with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Execute with retry
	var err error
	for i := 0; i < 3; i++ {
		resp, appendErr := sheetsService.Spreadsheets.Values.Append(
			config.SpreadsheetID,
			config.ProductionSheet,
			&sheets.ValueRange{
				Values:         values,
				MajorDimension: "ROWS",
			},
		).ValueInputOption("USER_ENTERED").InsertDataOption("INSERT_ROWS").Context(ctx).Do()

		if appendErr == nil {
			log.Printf("Data written successfully. Updated range: %s", resp.Updates.UpdatedRange)
			return nil
		}
		err = appendErr
		time.Sleep(time.Second * time.Duration(i+1))
	}

	return fmt.Errorf("failed to append data after 3 attempts: %v", err)
}

func appendTimesheetData(data TimesheetData) error {
	values := [][]interface{}{
		{
			data.Date,
			data.FullName,
			data.Hours,
			time.Now().Format(time.RFC3339),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var err error
	for i := 0; i < 3; i++ {
		resp, appendErr := sheetsService.Spreadsheets.Values.Append(
			config.SpreadsheetID,
			config.TimesheetSheet,
			&sheets.ValueRange{
				Values:         values,
				MajorDimension: "ROWS",
			},
		).ValueInputOption("USER_ENTERED").InsertDataOption("INSERT_ROWS").Context(ctx).Do()

		if appendErr == nil {
			log.Printf("Timesheet data written successfully. Updated range: %s", resp.Updates.UpdatedRange)
			return nil
		}
		err = appendErr
		time.Sleep(time.Second * time.Duration(i+1))
	}

	return fmt.Errorf("failed to append timesheet data after 3 attempts: %v", err)
}

func productionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var data ProductionData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validation
	if strings.TrimSpace(data.FullName) == "" || strings.TrimSpace(data.PartAndOperation) == "" || strings.TrimSpace(data.TotalParts) == "" {
		http.Error(w, "Full name, part/operation and total parts are required", http.StatusBadRequest)
		return
	}

	if data.Date == "" {
		data.Date = time.Now().Format("2006-01-02")
	}

	if err := appendProductionData(data); err != nil {
		log.Printf("Error writing production data: %v", err)
		http.Error(w, "Failed to process data", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func timesheetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var data TimesheetData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validation
	if strings.TrimSpace(data.FullName) == "" || strings.TrimSpace(data.Hours) == "" {
		http.Error(w, "Full name and hours are required", http.StatusBadRequest)
		return
	}

	if data.Date == "" {
		data.Date = time.Now().Format("2006-01-02")
	}

	if err := appendTimesheetData(data); err != nil {
		log.Printf("Error writing timesheet data: %v", err)
		http.Error(w, "Failed to process data", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Service is healthy"))
}

func enableCORS(next http.HandlerFunc) http.HandlerFunc {
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
