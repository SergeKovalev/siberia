package handlers

import (
	"net/http"

	"google.golang.org/api/sheets/v4"

	"github.com/sergekovalev/siberia/internal/config"
	"github.com/sergekovalev/siberia/internal/utils"
)

// InitHandlers инициализирует обработчики HTTP-запросов
func InitHandlers(srv *sheets.Service, cfg config.Config) {
	// Обработчик для отправки данных о производстве
	// Включает CORS (разрешение междоменных запросов) через utils.EnableCORS
	http.HandleFunc("/submit-production", utils.EnableCORS(ProductionHandler(srv, cfg)))

	// Обработчик для отправки данных табеля учета рабочего времени
	// Также включает CORS
	http.HandleFunc("/submit-timesheet", utils.EnableCORS(TimesheetHandler(srv, cfg)))

	// Обработчик для проверки состояния сервера (health check)
	http.HandleFunc("/health", HealthHandler)
}
