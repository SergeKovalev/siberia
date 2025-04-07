package utils

import (
	"net/http"
)

// EnableCORS добавляет заголовки CORS (Cross-Origin Resource Sharing) к HTTP-ответу
// Позволяет выполнять междоменные запросы (например, с фронтенда на другой домен)
func EnableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Устанавливаем заголовки для разрешения междоменных запросов
		w.Header().Set("Access-Control-Allow-Origin", "*")              // Разрешаем запросы с любого домена
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS") // Разрешаем методы POST и OPTIONS
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")  // Разрешаем заголовок Content-Type

		// Если метод запроса OPTIONS, возвращаем статус 200 (OK) и завершаем обработку
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Передаем управление следующему обработчику
		next(w, r)
	}
}

// ColumnToLetter преобразует номер столбца (например, 1, 2, 3) в буквенное обозначение (например, A, B, C)
// Используется для работы с адресами ячеек в Google Sheets
func ColumnToLetter(col int) string {
	letter := ""
	for col > 0 {
		col--                                        // Уменьшаем номер столбца, чтобы он стал 0-индексированным
		letter = string(rune('A'+(col%26))) + letter // Вычисляем букву для текущего столбца
		col = col / 26                               // Переходим к следующему разряду
	}
	return letter
}
