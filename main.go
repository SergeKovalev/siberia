package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
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
	log.Println("Проверка инициализации...")
	log.Printf("SpreadsheetID: %s", config.SpreadsheetID)

	log.SetOutput(os.Stdout)
	log.Println("Starting application...")

	loadConfig()
	log.Printf("Configuration loaded: %+v", config)

	if err := initSheetsService(); err != nil {
		log.Fatalf("Failed to initialize Google Sheets: %v", err)
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
		return fmt.Errorf("ошибка загрузки учетных данных: %v", err)
	}

	log.Println("Учетные данные получены, создаем конфиг JWT...")

	conf, err := google.JWTConfigFromJSON(creds, sheets.SpreadsheetsScope)
	if err != nil {
		return fmt.Errorf("ошибка создания JWT конфига: %v", err)
	}

	log.Println("JWT конфиг создан, инициализируем сервис...")

	sheetsService, err = sheets.NewService(ctx, option.WithHTTPClient(conf.Client(ctx)))
	if err != nil {
		return fmt.Errorf("ошибка создания сервиса Sheets: %v", err)
	}

	log.Println("Сервис Google Sheets успешно инициализирован!")
	return nil
}

func loadCredentials() ([]byte, error) {
	if base64Data := os.Getenv("GOOGLE_CREDENTIALS_BASE64"); base64Data != "" {
		data, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 credentials: %v", err)
		}
		return data, nil
	}

	if data, err := os.ReadFile("credentials.json"); err == nil {
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
			lastNonEmpty = i + 1 // +1 потому что строки нумеруются с 1
		}
	}

	return lastNonEmpty, nil
}

func appendProductionData(data ProductionData) error {
	log.Println("Поиск последней заполненной строки...")
	lastRow, err := findLastNonEmptyRow(config.ProductionSheet)
	if err != nil {
		log.Printf("Ошибка поиска строки: %v", err)
		return fmt.Errorf("ошибка поиска последней строки: %v", err)
	}
	log.Printf("Последняя заполненная строка: %d", lastRow)

	targetRow := lastRow + 1
	log.Printf("Запись в строку: %d", targetRow)

	values := [][]interface{}{
		{data.Date, data.FullName, data.PartAndOperation, data.TotalParts, data.Defective, data.GoodParts, data.Notes},
	}

	rangeData := fmt.Sprintf("%s!A%d:G%d", config.ProductionSheet, targetRow, targetRow)
	log.Printf("Диапазон для записи: %s", rangeData)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Println("Отправка запроса к Google Sheets API...")
	_, err = sheetsService.Spreadsheets.Values.Update(
		config.SpreadsheetID,
		rangeData,
		&sheets.ValueRange{Values: values},
	).ValueInputOption("RAW").Context(ctx).Do()

	if err != nil {
		log.Printf("Полная ошибка от API: %+v", err)
		return fmt.Errorf("ошибка записи: %v", err)
	}

	log.Println("Данные успешно записаны!")
	return nil
}

func findTimesheetCell(data TimesheetData) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Получаем все данные с листа
	resp, err := sheetsService.Spreadsheets.Values.Get(
		config.SpreadsheetID,
		fmt.Sprintf("%s!A:ZZ", config.TimesheetSheet),
	).Context(ctx).Do()

	if err != nil {
		return "", fmt.Errorf("failed to get sheet data: %v", err)
	}

	if len(resp.Values) == 0 {
		return "", fmt.Errorf("timesheet is empty")
	}

	// Находим строку с ФИО
	var targetRow int
	for i, row := range resp.Values {
		if len(row) > 0 && strings.TrimSpace(row[0].(string)) == data.FullName {
			targetRow = i + 1 // +1 потому что строки нумеруются с 1
			break
		}
	}

	if targetRow == 0 {
		return "", fmt.Errorf("full name not found in timesheet")
	}

	// Находим столбец с датой (первая строка - заголовки)
	if len(resp.Values) < 1 {
		return "", fmt.Errorf("no header row in timesheet")
	}

	headerRow := resp.Values[0]
	var targetCol int
	for i, cell := range headerRow {
		if cellStr, ok := cell.(string); ok && strings.TrimSpace(cellStr) == data.Date {
			targetCol = i + 1 // +1 потому что столбцы нумеруются с 1
			break
		}
	}

	if targetCol == 0 {
		return "", fmt.Errorf("date not found in timesheet header")
	}

	// Конвертируем номер столбца в буквенное обозначение (A, B, ..., AA, AB, ...)
	colLetter := columnToLetter(targetCol)
	return fmt.Sprintf("%s!%s%d", config.TimesheetSheet, colLetter, targetRow), nil
}

func columnToLetter(col int) string {
	letter := ""
	for col > 0 {
		col--
		letter = string(rune('A'+col%26)) + letter
		col = col / 26
	}
	return letter
}

func appendTimesheetData(data TimesheetData) error {
	log.Println("Поиск ячейки для табеля...")
	cell, err := findTimesheetCell(data)
	if err != nil {
		log.Printf("Ошибка поиска ячейки: %v", err)
		return fmt.Errorf("ошибка поиска ячейки: %v", err)
	}
	log.Printf("Найдена ячейка: %s", cell)

	if _, err := strconv.ParseFloat(data.Hours, 64); err != nil {
		return fmt.Errorf("часы должны быть числом: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Println("Отправка запроса на обновление табеля...")
	_, err = sheetsService.Spreadsheets.Values.Update(
		config.SpreadsheetID,
		cell,
		&sheets.ValueRange{Values: [][]interface{}{{data.Hours}}},
	).ValueInputOption("USER_ENTERED").Context(ctx).Do()

	if err != nil {
		log.Printf("Полная ошибка от API: %+v", err)
		return fmt.Errorf("ошибка обновления табеля: %v", err)
	}

	log.Println("Данные табеля успешно обновлены!")
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
