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
	SheetName       string `json:"sheetName"`
}

// UserData представляет данные для вашей таблицы
type UserData struct {
	Date             string `json:"date"`             // Дата
	FullName         string `json:"fullName"`         // ФИО
	PartAndOperation string `json:"partAndOperation"` // Название детали и операции
	TotalParts       string `json:"totalParts"`       // Кол-во деталей общее
	Defective        string `json:"defective"`        // Брак
	GoodParts        string `json:"goodParts"`        // Кол-во деталей чистое
	Notes            string `json:"notes"`            // Для заметок
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
	http.HandleFunc("/submit", enableCORS(submitHandler))
	http.HandleFunc("/health", healthHandler)

	// Запуск сервера
	log.Printf("Сервис запущен на порту %s", config.Port)

	// Обработка статики
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	// Настройка HTTP-сервера с таймаутами
	srv := &http.Server{
		Addr:         ":" + config.Port,
		ReadTimeout:  10 * time.Second, // Макс время чтения запроса
		WriteTimeout: 30 * time.Second, // Макс время записи ответа
		IdleTimeout:  60 * time.Second, // Макс время бездействия
	}

	log.Printf("Сервис запущен на порту %s", config.Port)

	// Запуск сервера с обработкой ошибок
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
		Port:          "8080",
		SpreadsheetID: "",
		SheetName:     "Sheet1",
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

func submitHandler(w http.ResponseWriter, r *http.Request) {
	// Разрешаем только POST запросы
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Парсим данные пользователя
	var userData UserData
	if err := json.NewDecoder(r.Body).Decode(&userData); err != nil {
		http.Error(w, "Неверный формат данных", http.StatusBadRequest)
		return
	}

	// Установим текущую дату, если не указана
	if userData.Date == "" {
		userData.Date = time.Now().Format("2006-01-02")
	}

	// Валидация обязательных полей
	if userData.FullName == "" || userData.PartAndOperation == "" {
		http.Error(w, "ФИО и Название детали/операции обязательны для заполнения", http.StatusBadRequest)
		return
	}

	// Записываем данные в Google Таблицу
	if err := appendToGoogleSheet(userData); err != nil {
		log.Printf("Ошибка записи в Google Таблицу: %v", err)
		http.Error(w, "Ошибка обработки данных", http.StatusInternalServerError)
		return
	}

	// Отправляем успешный ответ
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func appendToGoogleSheet(userData UserData) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Чтение учетных данных
	credentials, err := loadCredentials()
	if err != nil {
		return fmt.Errorf("ошибка загрузки учетных данных: %v", err)
	}

	// Создание клиента Google Sheets
	client, err := google.JWTConfigFromJSON(credentials, sheets.SpreadsheetsScope)
	if err != nil {
		return fmt.Errorf("не удалось создать JWT конфиг: %v", err)
	}

	srv, err := sheets.NewService(ctx, option.WithHTTPClient(client.Client(ctx)))
	if err != nil {
		return fmt.Errorf("не удалось создать сервис Google Sheets: %v", err)
	}

	// Подготовка данных для записи (соответствует вашим столбцам)
	values := [][]interface{}{
		{
			userData.Date,
			userData.FullName,
			userData.PartAndOperation,
			userData.TotalParts,
			userData.Defective,
			userData.GoodParts,
			userData.Notes,
		},
	}

	// Получаем последнюю заполненную строку
	resp, err := srv.Spreadsheets.Values.Get(
		config.SpreadsheetID,
		config.SheetName+"!A:A", // Проверяем столбец A (Дата)
	).Do()
	if err != nil {
		return fmt.Errorf("ошибка получения данных: %v", err)
	}

	lastRow := len(resp.Values) + 1 // Следующая после последней
	rangeData := fmt.Sprintf("%s!A%d:G%d", config.SheetName, lastRow, lastRow)

	// Используем Update вместо Append
	_, err = srv.Spreadsheets.Values.Update(
		config.SpreadsheetID,
		rangeData,
		&sheets.ValueRange{Values: values},
	).ValueInputOption("USER_ENTERED").Do()

	return err
}

func enableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Разрешаем запросы с любого origin
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			return
		}

		next(w, r)
	}
}
