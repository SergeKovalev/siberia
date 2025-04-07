package handlers

import "net/http"

// HealthHandler обрабатывает запросы для проверки состояния сервера (health check)
// Возвращает HTTP-статус 200 (OK) и сообщение "Service is healthy"
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)          // Устанавливаем статус ответа 200 (OK)
	w.Write([]byte("Service is healthy")) // Отправляем текстовый ответ
}
