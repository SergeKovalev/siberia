package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/sheets/v4"

	"github.com/sergekovalev/siberia/internal/config"
	"github.com/sergekovalev/siberia/internal/models"
)

// TimesheetHandler обрабатывает HTTP-запросы для добавления данных табеля учета рабочего времени
func TimesheetHandler(srv *sheets.Service, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Проверяем, что метод запроса - POST
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed) // Возвращаем ошибку 405
			return
		}

		// Декодируем тело запроса в структуру TimesheetData
		var data models.TimesheetData
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest) // Ошибка 400, если тело запроса некорректно
			return
		}

		// Проверяем обязательные поля: FullName и Hours
		if strings.TrimSpace(data.FullName) == "" || strings.TrimSpace(data.Hours) == "" {
			http.Error(w, "Full name and hours are required", http.StatusBadRequest) // Ошибка 400, если поля пустые
			return
		}

		// Если дата не указана, устанавливаем текущую дату
		if data.Date == "" {
			data.Date = time.Now().Format("2006-01-02") // Форматируем дату в формате YYYY-MM-DD
		} else {
			// Проверяем формат даты
			if _, err := time.Parse("2006-01-02", data.Date); err != nil {
				http.Error(w, "Invalid date format, expected YYYY-MM-DD", http.StatusBadRequest) // Ошибка 400, если формат даты некорректен
				return
			}
		}

		// Проверяем, что поле Hours содержит число
		if _, err := strconv.ParseFloat(data.Hours, 64); err != nil {
			http.Error(w, "Hours must be a number", http.StatusBadRequest) // Ошибка 400, если поле Hours не является числом
			return
		}

		// Добавляем данные табеля в Google Sheets
		if err := models.AppendTimesheetData(srv, cfg.SpreadsheetID, data); err != nil {
			log.Printf("Error writing timesheet data: %v", err)                                           // Логируем ошибку
			http.Error(w, fmt.Sprintf("Failed to process data: %v", err), http.StatusInternalServerError) // Ошибка 500, если не удалось записать данные
			return
		}

		// Успешный ответ с кодом 201 (Created)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "success"}) // Отправляем JSON-ответ с сообщением об успехе
	}
}
