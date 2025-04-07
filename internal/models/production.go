package models

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/api/sheets/v4"

	"github.com/sergekovalev/siberia/internal/config"
)

// ProductionData представляет структуру данных о производстве
type ProductionData struct {
	Date             string `json:"date"`             // Дата производства
	FullName         string `json:"fullName"`         // Полное имя сотрудника
	PartAndOperation string `json:"partAndOperation"` // Деталь и операция
	TotalParts       string `json:"totalParts"`       // Общее количество деталей
	Defective        string `json:"defective"`        // Количество дефектных деталей
	GoodParts        string `json:"goodParts"`        // Количество годных деталей
	Notes            string `json:"notes"`            // Примечания
}

// AppendProductionData добавляет данные о производстве в Google Sheets
func AppendProductionData(srv *sheets.Service, cfg config.Config, data ProductionData) error {
	// Находим последнюю непустую строку в таблице
	lastRow, err := findLastNonEmptyRow(srv, cfg.SpreadsheetID, cfg.ProductionSheet)
	if err != nil {
		return fmt.Errorf("failed to find last row: %v", err) // Возвращаем ошибку, если не удалось найти строку
	}

	// Определяем строку, в которую будут добавлены данные
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

	// Устанавливаем контекст с таймаутом для выполнения запроса
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Формируем диапазон для обновления данных
	rangeData := fmt.Sprintf("%s!A%d:G%d", cfg.ProductionSheet, targetRow, targetRow)
	_, err = srv.Spreadsheets.Values.Update(
		cfg.SpreadsheetID,
		rangeData,
		&sheets.ValueRange{Values: values},
	).ValueInputOption("USER_ENTERED").Context(ctx).Do()

	if err != nil {
		return fmt.Errorf("failed to update sheet: %v", err) // Возвращаем ошибку, если не удалось обновить таблицу
	}

	// Логируем успешное добавление данных
	log.Printf("Production data written to row %d", targetRow)
	return nil
}

// findLastNonEmptyRow находит последнюю непустую строку в указанном листе Google Sheets
func findLastNonEmptyRow(srv *sheets.Service, spreadsheetID, sheetName string) (int, error) {
	// Устанавливаем контекст с таймаутом для выполнения запроса
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Получаем данные из первого столбца (A:A) указанного листа
	resp, err := srv.Spreadsheets.Values.Get(
		spreadsheetID,
		fmt.Sprintf("%s!A:A", sheetName),
	).Context(ctx).Do()

	if err != nil {
		return 0, fmt.Errorf("failed to get sheet data: %v", err) // Возвращаем ошибку, если не удалось получить данные
	}

	// Ищем последнюю непустую строку
	lastNonEmpty := 0
	for i, row := range resp.Values {
		if len(row) > 0 && strings.TrimSpace(row[0].(string)) != "" {
			lastNonEmpty = i + 1
		}
	}

	return lastNonEmpty, nil // Возвращаем номер последней непустой строки
}
