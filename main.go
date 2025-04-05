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
	SpreadsheetID   string `json:"spreadsheetID"`
	ProductionSheet string `json:"productionSheet"`
	TimesheetSheet  string `json:"timesheetSheet"`
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
	config        Config
	sheetsService *sheets.Service
)

func main() {
	// Инициализация логгера
	log.SetOutput(os.Stdout)
	log.Println("Запуск приложения...")

	// Загрузка конфигурации
	loadConfig()
	log.Printf("Конфигурация загружена: %+v", config)

	// Инициализация сервиса Google Sheets
	if err := initSheetsService(); err != nil {
		log.Fatalf("Ошибка инициализации Google Sheets: %v", err)
	}

	// Проверка доступа к таблице
	if err := verifySheetsAccess(); err != nil {
		log.Fatalf("Ошибка доступа к таблице: %v", err)
	}

	// Настройка маршрутов HTTP
	http.HandleFunc("/submit-production", enableCORS(productionHandler))
	http.HandleFunc("/submit-timesheet", enableCORS(timesheetHandler))
	http.HandleFunc("/health", healthHandler)

	// Обслуживание статических файлов
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	// Настройка HTTP сервера
	srv := &http.Server{
		Addr:         ":" + config.Port,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Сервер запущен на порту %s", config.Port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Ошибка сервера: %v", err)
	}
}

func loadConfig() {
	// Установка значений по умолчанию
	config = Config{
		Port:            "8080",
		ProductionSheet: "Production",
		TimesheetSheet:  "Timesheet",
	}

	// Загрузка конфигурации из файла
	if file, err := os.Open("config.json"); err == nil {
		defer file.Close()
		if err := json.NewDecoder(file).Decode(&config); err != nil {
			log.Printf("Ошибка чтения config.json: %v", err)
		}
	}

	// Переопределение переменными окружения
	if envID := os.Getenv("SPREADSHEET_ID"); envID != "" {
		config.SpreadsheetID = envID
	}

	// Валидация обязательных полей
	if config.SpreadsheetID == "" {
		log.Fatal("SpreadsheetID должен быть указан в config.json или SPREADSHEET_ID")
	}
}

func initSheetsService() error {
	ctx := context.Background()

	// Загрузка учетных данных
	creds, err := loadCredentials()
	if err != nil {
		return fmt.Errorf("ошибка загрузки учетных данных: %v", err)
	}

	// Создание JWT конфигурации
	conf, err := google.JWTConfigFromJSON(creds, sheets.SpreadsheetsScope)
	if err != nil {
		return fmt.Errorf("ошибка создания JWT конфига: %v", err)
	}

	// Создание сервиса Google Sheets
	sheetsService, err = sheets.NewService(ctx, option.WithHTTPClient(conf.Client(ctx)))
	if err != nil {
		return fmt.Errorf("ошибка создания сервиса Sheets: %v", err)
	}

	return nil
}

func verifySheetsAccess() error {
	// Проверка существования таблицы
	_, err := sheetsService.Spreadsheets.Get(config.SpreadsheetID).Do()
	if err != nil {
		return fmt.Errorf("ошибка доступа к таблице: %v", err)
	}

	// Проверка существования листов
	if _, err := sheetsService.Spreadsheets.Values.Get(
		config.SpreadsheetID,
		config.ProductionSheet+"!A1",
	).Do(); err != nil {
		return fmt.Errorf("лист производства не найден: %v", err)
	}

	if _, err := sheetsService.Spreadsheets.Values.Get(
		config.SpreadsheetID,
		config.TimesheetSheet+"!A1",
	).Do(); err != nil {
		return fmt.Errorf("лист табеля не найден: %v", err)
	}

	log.Println("Успешная проверка доступа к Google Sheets")
	return nil
}

func loadCredentials() ([]byte, error) {
	// 1. Пробуем получить из переменной окружения
	if base64Data := os.Getenv("GOOGLE_CREDENTIALS_BASE64"); base64Data != "" {
		data, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			return nil, fmt.Errorf("ошибка декодирования base64: %v", err)
		}
		log.Println("Используются учетные данные из GOOGLE_CREDENTIALS_BASE64")
		return data, nil
	}

	// 2. Пробуем прочитать из файла
	if data, err := os.ReadFile("credentials.json"); err == nil {
		log.Println("Используются учетные данные из credentials.json")
		return data, nil
	}

	return nil, fmt.Errorf("не найдены учетные данные (ни в GOOGLE_CREDENTIALS_BASE64, ни в credentials.json)")
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
			time.Now().Format("2006-01-02 15:04:05"),
		},
	}

	// Выполняем запись
	resp, err := sheetsService.Spreadsheets.Values.Append(
		config.SpreadsheetID,
		config.ProductionSheet+"!A1",
		&sheets.ValueRange{Values: values},
	).ValueInputOption("USER_ENTERED").InsertDataOption("INSERT_ROWS").Do()

	if err != nil {
		return fmt.Errorf("ошибка при добавлении данных: %v", err)
	}

	log.Printf("Данные производства записаны. Обновленный диапазон: %s", resp.Updates.UpdatedRange)
	return nil
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

func appendTimesheetData(data TimesheetData) error {
	values := [][]interface{}{
		{
			data.Date,
			data.FullName,
			data.Hours,
			time.Now().Format("2006-01-02 15:04:05"),
		},
	}

	// Выполняем запись
	resp, err := sheetsService.Spreadsheets.Values.Append(
		config.SpreadsheetID,
		config.TimesheetSheet+"!A1",
		&sheets.ValueRange{Values: values},
	).ValueInputOption("USER_ENTERED").InsertDataOption("INSERT_ROWS").Do()

	if err != nil {
		return fmt.Errorf("ошибка при добавлении данных: %v", err)
	}

	log.Printf("Данные табеля записаны. Обновленный диапазон: %s", resp.Updates.UpdatedRange)
	return nil
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Сервис работает нормально")
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
