package googleapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"github.com/sergekovalev/siberia/internal/config"
)

// InitSheetsService инициализирует сервис Google Sheets с использованием учетных данных
func InitSheetsService(cfg config.Config) (*sheets.Service, error) {
	// Загружаем учетные данные
	creds, err := loadCredentials()
	if err != nil {
		return nil, fmt.Errorf("failed to load credentials: %v", err) // Ошибка загрузки учетных данных
	}

	// Создаем конфигурацию JWT из учетных данных
	conf, err := google.JWTConfigFromJSON(creds, sheets.SpreadsheetsScope)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials: %v", err) // Ошибка в учетных данных
	}

	// Создаем контекст и инициализируем сервис Google Sheets
	ctx := context.Background()
	sheetsService, err := sheets.NewService(ctx, option.WithHTTPClient(conf.Client(ctx)))
	if err != nil {
		return nil, fmt.Errorf("failed to create sheets service: %v", err) // Ошибка создания сервиса
	}

	return sheetsService, nil
}

// VerifyAccess проверяет доступ к таблице Google Sheets по указанному SpreadsheetID
func VerifyAccess(service *sheets.Service, spreadsheetID string) error {
	// Пытаемся получить таблицу по ID
	_, err := service.Spreadsheets.Get(spreadsheetID).Do()
	if err != nil {
		return fmt.Errorf("failed to access spreadsheet: %v", err) // Ошибка доступа к таблице
	}
	log.Println("Spreadsheet access verified successfully") // Доступ успешно проверен
	return nil
}

// loadCredentials загружает учетные данные для доступа к Google API
func loadCredentials() ([]byte, error) {
	// Проверяем наличие учетных данных в переменной окружения GOOGLE_CREDENTIALS_BASE64
	if base64Data := os.Getenv("GOOGLE_CREDENTIALS_BASE64"); base64Data != "" {
		// Декодируем учетные данные из base64
		data, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 credentials: %v", err) // Ошибка декодирования
		}
		log.Println("Using credentials from GOOGLE_CREDENTIALS_BASE64") // Используем учетные данные из переменной окружения
		return data, nil
	}

	// Если переменной окружения нет, пытаемся загрузить учетные данные из файла credentials.json
	if data, err := os.ReadFile("credentials.json"); err == nil {
		log.Println("Using credentials from credentials.json") // Используем учетные данные из файла
		return data, nil
	}

	// Если учетные данные не найдены, возвращаем ошибку
	return nil, fmt.Errorf("no credentials provided")
}
