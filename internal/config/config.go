package config

import (
	"encoding/json"
	"log"
	"os"
)

// Структура Config содержит параметры конфигурации приложения
type Config struct {
	Port            string `json:"port"`            // Порт для запуска HTTP-сервера
	SpreadsheetID   string `json:"spreadsheetID"`   // ID таблицы Google Sheets
	ProductionSheet string `json:"productionSheet"` // Название листа для данных о производстве
	TimesheetSheet  string `json:"timesheetSheet"`  // Название листа для табеля учета рабочего времени
}

// LoadConfig загружает конфигурацию из файла config.json и переменных окружения
func LoadConfig() Config {
	// Устанавливаем значения по умолчанию
	cfg := Config{
		Port:            "8080",   // Порт по умолчанию
		ProductionSheet: "Выпуск", // Название листа для производства по умолчанию
		TimesheetSheet:  "Табель", // Название листа для табеля по умолчанию
	}

	// Пытаемся открыть файл config.json
	if file, err := os.Open("config.json"); err == nil {
		defer file.Close() // Закрываем файл после завершения работы
		// Декодируем содержимое файла в структуру Config
		if err := json.NewDecoder(file).Decode(&cfg); err != nil {
			log.Printf("Ошибка при чтении config.json: %v", err)
		}
	}

	// Проверяем наличие переменной окружения SPREADSHEET_ID
	if envID := os.Getenv("SPREADSHEET_ID"); envID != "" {
		cfg.SpreadsheetID = envID // Если переменная задана, используем её значение
	}

	// Если SpreadsheetID не задан, завершаем выполнение программы с ошибкой
	if cfg.SpreadsheetID == "" {
		log.Fatal("Необходимо указать SpreadsheetID")
	}

	// Возвращаем загруженную конфигурацию
	return cfg
}
