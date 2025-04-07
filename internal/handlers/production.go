package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"google.golang.org/api/sheets/v4"

	"github.com/sergekovalev/siberia/internal/config"
	"github.com/sergekovalev/siberia/internal/models"
)

// ProductionHandler обрабатывает HTTP-запросы для добавления данных о производстве
func ProductionHandler(srv *sheets.Service, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Проверяем, что метод запроса - POST
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed) // Возвращаем ошибку 405
			return
		}

		// Декодируем тело запроса в структуру ProductionData
		var data models.ProductionData
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest) // Ошибка 400, если тело запроса некорректно
			return
		}

		// Проверяем обязательные поля: FullName, PartAndOperation и TotalParts
		if strings.TrimSpace(data.FullName) == "" || strings.TrimSpace(data.PartAndOperation) == "" || strings.TrimSpace(data.TotalParts) == "" {
			http.Error(w, "Full name, part/operation and total parts are required", http.StatusBadRequest) // Ошибка 400, если поля пустые
			return
		}

		// Если дата не указана, устанавливаем текущую дату
		if data.Date == "" {
			data.Date = time.Now().Format("2006-01-02") // Форматируем дату в формате YYYY-MM-DD
		}

		// Добавляем данные о производстве в Google Sheets
		if err := models.AppendProductionData(srv, cfg, data); err != nil {
			log.Printf("Error writing production data: %v", err)                    // Логируем ошибку
			http.Error(w, "Failed to process data", http.StatusInternalServerError) // Ошибка 500, если не удалось записать данные
			return
		}

		// Успешный ответ с кодом 201 (Created)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "success"}) // Отправляем JSON-ответ с сообщением об успехе
	}
}
