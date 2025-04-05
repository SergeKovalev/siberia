package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// Config представляет конфигурацию приложения
type Config struct {
	Port            string `json:"port"`
	CredentialsFile string `json:"credentialsFile"`
	SpreadsheetID   string `json:"spreadsheetID"`
	ProductionSheet string `json:"productionSheet"` // Лист для учета производства
	TimesheetSheet  string `json:"timesheetSheet"`  // Лист для табеля работы
}

// ProductionData представляет данные формы учета производства
type ProductionData struct {
	Date             string `json:"date"`
	FullName         string `json:"fullName"`
	PartAndOperation string `json:"partAndOperation"`
	TotalParts       string `json:"totalParts"`
	Defective        string `json:"defective"`
	GoodParts        string `json:"goodParts"`
	Notes            string `json:"notes"`
}

// TimesheetData представляет данные формы табеля работы
type TimesheetData struct {
	Date     string `json:"date"`
	FullName string `json:"fullName"`
	Hours    string `json:"hours"`
}

var (
	config Config
)

func init() {
	logFile, err := os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		log.SetOutput(logFile)
	}
}

func main() {
	// Загрузка конфигурации
	loadConfig()

	// Настройка HTTP маршрутов
	http.HandleFunc("/submit-production", enableCORS(productionHandler))
	http.HandleFunc("/submit-timesheet", enableCORS(timesheetHandler))
	http.HandleFunc("/health", healthHandler)

	// Обработка статики
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	// Настройка HTTP-сервера с таймаутами
	srv := &http.Server{
		Addr:         ":" + config.Port,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Сервис запущен на порту %s", config.Port)

	// Запуск сервера
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Ошибка сервера: %v", err)
	}
}

func loadCredentials() ([]byte, error) {
	base64Data := os.Getenv("GOOGLE_CREDENTIALS_BASE64")
	if base64Data == "" {
		return nil, fmt.Errorf("переменная GOOGLE_CREDENTIALS_BASE64 не задана")
	}
	return base64.StdEncoding.DecodeString(base64Data)
}

func loadConfig() {
	// Значения по умолчанию
	config = Config{
		Port:            "8080",
		SpreadsheetID:   "",
		ProductionSheet: "Выпуск",
		TimesheetSheet:  "Табель",
	}

	// Попытка загрузить конфиг из файла
	configFile, err := os.Open("config.json")
	if err != nil {
		log.Printf("Не удалось загрузить config.json, используются значения по умолчанию: %v", err)
		return
	}
	defer configFile.Close()

	if err := json.NewDecoder(configFile).Decode(&config); err != nil {
		log.Printf("Ошибка чтения config.json: %v", err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Сервис работает нормально")
}

func productionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var data ProductionData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Неверный формат данных", http.StatusBadRequest)
		return
	}

	// Установка текущей даты, если не указана
	if data.Date == "" {
		data.Date = time.Now().Format("2006-01-02")
	}

	// Валидация обязательных полей
	if data.FullName == "" || data.PartAndOperation == "" || data.TotalParts == "" {
		http.Error(w, "ФИО, Название операции и Количество деталей обязательны", http.StatusBadRequest)
		return
	}

	// Запись в Google Sheets
	if err := appendProductionData(data); err != nil {
		log.Printf("Ошибка записи данных производства: %v", err)
		http.Error(w, "Ошибка обработки данных", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func timesheetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var data TimesheetData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Неверный формат данных", http.StatusBadRequest)
		return
	}

	// Установка текущей даты, если не указана
	if data.Date == "" {
		data.Date = time.Now().Format("2006-01-02")
	}

	// Валидация обязательных полей
	if data.FullName == "" || data.Hours == "" {
		http.Error(w, "ФИО и Количество часов обязательны", http.StatusBadRequest)
		return
	}

	// Запись в Google Sheets
	if err := appendTimesheetData(data); err != nil {
		log.Printf("Ошибка записи данных табеля: %v", err)
		http.Error(w, "Ошибка обработки данных", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func appendProductionData(data ProductionData) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	credentials, err := loadCredentials()
	if err != nil {
		return fmt.Errorf("ошибка загрузки учетных данных: %v", err)
	}

	client, err := google.JWTConfigFromJSON(credentials, sheets.SpreadsheetsScope)
	if err != nil {
		return fmt.Errorf("не удалось создать JWT конфиг: %v", err)
	}

	srv, err := sheets.NewService(ctx, option.WithHTTPClient(client.Client(ctx)))
	if err != nil {
		return fmt.Errorf("не удалось создать сервис Google Sheets: %v", err)
	}

	// Подготовка данных для записи
	values := [][]interface{}{
		{
			data.Date,
			data.FullName,
			data.PartAndOperation,
			data.TotalParts,
			data.Defective,
			data.GoodParts,
			data.Notes,
			time.Now().Format("2006-01-02 15:04:05"), // Timestamp записи
		},
	}

	// Определяем диапазон для записи
	rangeData := fmt.Sprintf("%s!A1", config.ProductionSheet)

	// Используем Append для добавления новой строки
	_, err = srv.Spreadsheets.Values.Append(
		config.SpreadsheetID,
		rangeData,
		&sheets.ValueRange{Values: values},
	).ValueInputOption("USER_ENTERED").Do()

	return err
}

func appendTimesheetData(data TimesheetData) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	credentials, err := loadCredentials()
	if err != nil {
		return fmt.Errorf("ошибка загрузки учетных данных: %v", err)
	}

	client, err := google.JWTConfigFromJSON(credentials, sheets.SpreadsheetsScope)
	if err != nil {
		return fmt.Errorf("не удалось создать JWT конфиг: %v", err)
	}

	srv, err := sheets.NewService(ctx, option.WithHTTPClient(client.Client(ctx)))
	if err != nil {
		return fmt.Errorf("не удалось создать сервис Google Sheets: %v", err)
	}

	// Подготовка данных для записи
	values := [][]interface{}{
		{
			data.Date,
			data.FullName,
			data.Hours,
			time.Now().Format("2006-01-02 15:04:05"), // Timestamp записи
		},
	}

	// Определяем диапазон для записи
	rangeData := fmt.Sprintf("%s!A1", config.TimesheetSheet)

	// Используем Append для добавления новой строки
	_, err = srv.Spreadsheets.Values.Append(
		config.SpreadsheetID,
		rangeData,
		&sheets.ValueRange{Values: values},
	).ValueInputOption("USER_ENTERED").Do()

	return err
}

func enableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			return
		}

		next(w, r)
	}
}
