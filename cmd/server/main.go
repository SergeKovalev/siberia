package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/sergekovalev/siberia/internal/config"
	"github.com/sergekovalev/siberia/internal/googleapi"
	"github.com/sergekovalev/siberia/internal/handlers"
)

func main() {
	// Устанавливаем вывод логов в стандартный поток вывода (консоль)
	log.SetOutput(os.Stdout)
	log.Println("Запуск приложения...")

	// Загружаем конфигурацию из файла или переменных окружения
	cfg := config.LoadConfig()
	log.Printf("Конфигурация загружена: SpreadsheetID: %s", cfg.SpreadsheetID)

	// Инициализируем сервис Google Sheets
	sheetsService, err := googleapi.InitSheetsService(cfg)
	if err != nil {
		log.Fatalf("Не удалось инициализировать Google Sheets: %v", err)
	}

	// Проверяем доступ к Google Sheets с указанным SpreadsheetID
	if err := googleapi.VerifyAccess(sheetsService, cfg.SpreadsheetID); err != nil {
		log.Fatalf("Проверка доступа не удалась: %v", err)
	}

	// Инициализируем обработчики HTTP-запросов, передавая сервис Google Sheets и конфигурацию
	handlers.InitHandlers(sheetsService, cfg)

	// Настраиваем файловый сервер для обслуживания статических файлов из папки "./static"
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	// Настраиваем HTTP-сервер с таймаутами для чтения, записи и простоя
	srv := &http.Server{
		Addr:         ":" + cfg.Port,   // Порт, на котором будет работать сервер
		ReadTimeout:  10 * time.Second, // Таймаут чтения запроса
		WriteTimeout: 30 * time.Second, // Таймаут записи ответа
		IdleTimeout:  60 * time.Second, // Таймаут простоя соединения
	}

	// Логируем информацию о запуске сервера
	log.Printf("Сервер запущен на порту %s", cfg.Port)

	// Запускаем сервер и завершаем приложение в случае ошибки
	log.Fatal(srv.ListenAndServe())
}
