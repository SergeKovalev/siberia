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
	log.Printf("SpreadsheetID: %s", config.SpreadsheetID) // Добавлено для проверки

	if err := initSheetsService(); err != nil {
		log.Fatalf("Failed to initialize Google Sheets: %v", err)
	}

	// Проверка доступа
	if err := verifyAccess(); err != nil {
		log.Fatalf("Access verification failed: %v", err)
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

func verifyAccess() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := sheetsService.Spreadsheets.Get(config.SpreadsheetID).Do()
	if err != nil {
		return fmt.Errorf("failed to access spreadsheet: %v", err)
	}
	log.Println("Spreadsheet access verified successfully")
	return nil
}

func loadConfig() {
	config = Config{
		Port:            "8080",
		ProductionSheet: "Выпуск",
		TimesheetSheet:  "Табель",
	}

	if file, err := os.Open("config.json"); err == nil {
		defer file.Close()
		if err := json.NewDecoder(file).Decode(&config); err != nil {
			log.Printf("Error reading config.json: %v", err)
		}
	}

	if envID := os.Getenv("SPREADSHEET_ID"); envID != "" {
		config.SpreadsheetID = envID
	}

	if config.SpreadsheetID == "" {
		log.Fatal("SpreadsheetID must be specified")
	}
}

func initSheetsService() error {
	ctx := context.Background()

	creds, err := loadCredentials()
	if err != nil {
		return fmt.Errorf("failed to load credentials: %v", err)
	}

	conf, err := google.JWTConfigFromJSON(creds, sheets.SpreadsheetsScope)
	if err != nil {
		return fmt.Errorf("invalid credentials: %v", err)
	}

	sheetsService, err = sheets.NewService(ctx, option.WithHTTPClient(conf.Client(ctx)))
	if err != nil {
		return fmt.Errorf("failed to create sheets service: %v", err)
	}

	return nil
}

func loadCredentials() ([]byte, error) {
	if base64Data := os.Getenv("GOOGLE_CREDENTIALS_BASE64"); base64Data != "" {
		data, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 credentials: %v", err)
		}
		log.Println("Using credentials from GOOGLE_CREDENTIALS_BASE64")
		return data, nil
	}

	if data, err := os.ReadFile("credentials.json"); err == nil {
		log.Println("Using credentials from credentials.json")
		return data, nil
	}

	return nil, fmt.Errorf("no credentials provided")
}

func findLastNonEmptyRow(sheetName string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := sheetsService.Spreadsheets.Values.Get(
		config.SpreadsheetID,
		fmt.Sprintf("%s!A:A", sheetName),
	).Context(ctx).Do()

	if err != nil {
		return 0, fmt.Errorf("failed to get sheet data: %v", err)
	}

	lastNonEmpty := 0
	for i, row := range resp.Values {
		if len(row) > 0 && strings.TrimSpace(row[0].(string)) != "" {
			lastNonEmpty = i + 1
		}
	}

	return lastNonEmpty, nil
}

func appendProductionData(data ProductionData) error {
	lastRow, err := findLastNonEmptyRow(config.ProductionSheet)
	if err != nil {
		return fmt.Errorf("failed to find last row: %v", err)
	}

	targetRow := lastRow + 1
	values := [][]interface{}{
		{
			data.Date,
			data.FullName,
			data.PartAndOperation,
			data.TotalParts,
			data.Defective,
			data.GoodParts,
			data.Notes,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rangeData := fmt.Sprintf("%s!A%d:G%d", config.ProductionSheet, targetRow, targetRow)
	_, err = sheetsService.Spreadsheets.Values.Update(
		config.SpreadsheetID,
		rangeData,
		&sheets.ValueRange{Values: values},
	).ValueInputOption("USER_ENTERED").Context(ctx).Do()

	if err != nil {
		return fmt.Errorf("failed to update sheet: %v", err)
	}

	log.Printf("Production data written to row %d", targetRow)
	return nil
}

func findTimesheetCell(data TimesheetData) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Получаем ФИО (столбец B, строки 4-12)
	respNames, err := sheetsService.Spreadsheets.Values.Get(
		config.SpreadsheetID,
		"Табель!B4:B12",
	).Context(ctx).Do()

	if err != nil {
		return "", fmt.Errorf("failed to get names: %v", err)
	}

	// Находим строку для ФИО
	var targetRow int
	for i, row := range respNames.Values {
		if len(row) > 0 && strings.TrimSpace(row[0].(string)) == data.FullName {
			targetRow = 4 + i // Строки начинаются с 4
			break
		}
	}

	if targetRow == 0 {
		return "", fmt.Errorf("full name not found")
	}

	// Получаем даты (строка 3, столбцы C-AG)
	respDates, err := sheetsService.Spreadsheets.Values.Get(
		config.SpreadsheetID,
		"Табель!C3:AG3",
	).Context(ctx).Do()

	if err != nil {
		return "", fmt.Errorf("failed to get dates: %v", err)
	}

	// Находим столбец для даты
	var targetCol string
	if len(respDates.Values) > 0 {
		for i, cell := range respDates.Values[0] {
			if cellStr, ok := cell.(string); ok && strings.TrimSpace(cellStr) == data.Date {
				targetCol = columnToLetter(3 + i) // C=3, D=4, etc.
				break
			}
		}
	}

	if targetCol == "" {
		return "", fmt.Errorf("date not found")
	}

	return fmt.Sprintf("Табель!%s%d", targetCol, targetRow), nil
}

func columnToLetter(col int) string {
	letter := ""
	for col > 0 {
		col--
		letter = string(rune('A'+(col%26))) + letter
		col = col / 26
	}
	return letter
}

func appendTimesheetData(data TimesheetData) error {
	cell, err := findTimesheetCell(data)
	if err != nil {
		return fmt.Errorf("failed to find cell: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err = sheetsService.Spreadsheets.Values.Update(
		config.SpreadsheetID,
		cell,
		&sheets.ValueRange{
			Values: [][]interface{}{{data.Hours}},
		},
	).ValueInputOption("USER_ENTERED").Context(ctx).Do()

	if err != nil {
		return fmt.Errorf("failed to update cell: %v", err)
	}

	log.Printf("Timesheet data written to %s", cell)
	return nil
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
