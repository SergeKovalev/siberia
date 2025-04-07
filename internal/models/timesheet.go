package models

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/sergekovalev/siberia/internal/utils"
	"google.golang.org/api/sheets/v4"
)

// TimesheetData представляет структуру данных табеля учета рабочего времени
type TimesheetData struct {
	Date     string `json:"date"`     // Дата записи
	FullName string `json:"fullName"` // Полное имя сотрудника
	Hours    string `json:"hours"`    // Количество отработанных часов
}

// AppendTimesheetData добавляет данные табеля в Google Sheets
func AppendTimesheetData(srv *sheets.Service, spreadsheetID string, data TimesheetData) error {
	// Находим ячейку, соответствующую имени сотрудника и дню
	colLetter, row, col, err := findTimesheetCell(srv, spreadsheetID, data)
	if err != nil {
		return fmt.Errorf("failed to find cell: %v", err) // Возвращаем ошибку, если ячейка не найдена
	}

	// Формируем адрес ячейки для записи данных
	cell := fmt.Sprintf("Табель!%s%d", colLetter, row)
	log.Printf("Writing to cell %s (row %d, col %d)", cell, row, col) // Логируем адрес ячейки

	// Устанавливаем контекст с таймаутом для выполнения запроса
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Обновляем значение ячейки в Google Sheets
	_, err = srv.Spreadsheets.Values.Update(
		spreadsheetID,
		cell,
		&sheets.ValueRange{
			Values: [][]interface{}{{data.Hours}}, // Записываем количество часов
		},
	).ValueInputOption("USER_ENTERED").Context(ctx).Do()

	if err != nil {
		return fmt.Errorf("failed to update cell: %v", err) // Возвращаем ошибку, если не удалось обновить ячейку
	}

	log.Printf("Successfully wrote hours to %s", cell) // Логируем успешную запись
	return nil
}

// findTimesheetCell находит ячейку в таблице, соответствующую имени сотрудника и дню
func findTimesheetCell(srv *sheets.Service, spreadsheetID string, data TimesheetData) (string, int, int, error) {
	// Устанавливаем контекст с таймаутом для выполнения запроса
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Парсим дату из входных данных, чтобы извлечь день
	inputDate, err := time.Parse("2006-01-02", data.Date)
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid date format, expected YYYY-MM-DD: %v", err) // Ошибка, если формат даты некорректен
	}
	dayToFind := inputDate.Day() // Извлекаем день из даты

	// Получаем список имен сотрудников из столбца B (строки 4-12)
	respNames, err := srv.Spreadsheets.Values.Get(
		spreadsheetID,
		"Табель!B4:B12",
	).Context(ctx).Do()

	if err != nil {
		return "", 0, 0, fmt.Errorf("failed to get names: %v", err) // Ошибка, если не удалось получить имена
	}

	// Ищем строку, соответствующую имени сотрудника
	var targetRow int
	for i, row := range respNames.Values {
		if len(row) > 0 && strings.TrimSpace(row[0].(string)) == data.FullName {
			targetRow = 4 + i // Строки начинаются с 4
			break
		}
	}

	if targetRow == 0 {
		return "", 0, 0, fmt.Errorf("full name '%s' not found in timesheet", data.FullName) // Ошибка, если имя не найдено
	}

	// Получаем список дней (номера) из строки 3 (столбцы C:AG)
	respDays, err := srv.Spreadsheets.Values.Get(
		spreadsheetID,
		"Табель!C3:AG3",
	).Context(ctx).Do()

	if err != nil {
		return "", 0, 0, fmt.Errorf("failed to get days: %v", err) // Ошибка, если не удалось получить дни
	}

	// Ищем столбец, соответствующий дню
	var targetCol int
	if len(respDays.Values) > 0 {
		for i, cell := range respDays.Values[0] {
			cellStr := fmt.Sprintf("%v", cell) // Преобразуем значение в строку
			cellStr = strings.TrimSpace(cellStr)

			cellDay, err := strconv.Atoi(cellStr) // Преобразуем строку в число
			if err == nil && cellDay == dayToFind {
				targetCol = 3 + i // Столбцы начинаются с C (индекс 3)
				break
			}
		}
	}

	if targetCol == 0 {
		// Если день не найден, возвращаем ошибку с доступными днями
		availableDays := make([]string, 0)
		if len(respDays.Values) > 0 {
			for _, cell := range respDays.Values[0] {
				availableDays = append(availableDays, fmt.Sprintf("%v", cell))
			}
		}
		return "", 0, 0, fmt.Errorf("day %d not found in timesheet. Available days: %v", dayToFind, availableDays)
	}

	// Преобразуем номер столбца в букву
	colLetter := utils.ColumnToLetter(targetCol)
	return colLetter, targetRow, targetCol, nil
}
