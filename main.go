package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// Config содержит настройки приложения
type Config struct {
	Port            string `json:"port"`
	SpreadsheetID   string `json:"spreadsheetID"`
	ProductionSheet string `json:"productionSheet"`
	TimesheetSheet  string `json:"timesheetSheet"`
}

// ProductionData представляет данные о производстве
type ProductionData struct {
	Date             string `json:"date"`
	FullName         string `json:"fullName"`
	PartAndOperation string `json:"partAndOperation"`
	TotalParts       string `json:"totalParts"`
	Defective        string `json:"defective"`
	GoodParts        string `json:"goodParts"`
	Notes            string `json:"notes"`
}

// TimesheetData представляет данные табеля учета времени
type TimesheetData struct {
	Date     string `json:"date"`
	FullName string `json:"fullName"`
	Hours    string `json:"hours"`
}

var (
	config          Config
	sheetsService   *sheets.Service
	sheetCache      = make(map[string]bool)
	sheetCacheMux   sync.RWMutex
	templateSheetID int64
	// Removed unused variable lastActiveSheet
)

func main() {
	log.SetOutput(os.Stdout)
	log.Println("Starting application...")

	loadConfig()
	log.Printf("Configuration loaded: SpreadsheetID: %s", config.SpreadsheetID)

	if err := initSheetsService(); err != nil {
		log.Fatalf("Failed to initialize Google Sheets: %v", err)
	}

	if err := verifyAccess(); err != nil {
		log.Fatalf("Access verification failed: %v", err)
	}

	initSheetCache()
	http.HandleFunc("/submit-production", enableCORS(productionHandler))
	http.HandleFunc("/submit-timesheet", enableCORS(timesheetHandler))
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/get-dropdown-data", enableCORS(getDropdownDataHandler))
	http.HandleFunc("/get-operations-data", enableCORS(getOperationsDataHandler))

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	srv := &http.Server{
		Addr:         ":" + config.Port,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Server running on port %s", config.Port)
	log.Fatal(srv.ListenAndServe())
}

func initSheetCache() {
	sheetsList, err := ListSheets(sheetsService, config.SpreadsheetID)
	if err != nil {
		log.Fatalf("Failed to list sheets: %v", err)
	}

	sheetCacheMux.Lock()
	defer sheetCacheMux.Unlock()

	// Сначала ищем точное совпадение "Табель"
	for _, sheet := range sheetsList {
		sheetCache[sheet.Title] = true
		if strings.EqualFold(sheet.Title, "Табель") {
			templateSheetID = sheet.SheetId
			log.Printf("Found template sheet: %s (ID: %d)", sheet.Title, sheet.SheetId)
			return
		}
	}

	// Если точного совпадения нет, ищем любой лист, содержащий "табель" в названии
	for _, sheet := range sheetsList {
		if strings.Contains(strings.ToLower(sheet.Title), "табель") {
			templateSheetID = sheet.SheetId
			log.Printf("Using sheet '%s' as template (ID: %d)", sheet.Title, sheet.SheetId)
			return
		}
	}

	// Если ничего не найдено
	var sheetNames []string
	for _, sheet := range sheetsList {
		sheetNames = append(sheetNames, sheet.Title)
	}
	log.Fatalf("Template sheet containing 'Табель' not found. Available sheets: %v", sheetNames)
}

func getMonthSheetName(date time.Time) string {
	monthNames := []string{
		"Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
		"Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь",
	}
	month := monthNames[date.Month()-1]
	year := date.Year()
	return fmt.Sprintf("Табель %s %d", month, year)
}

func handleMonthSheet(date time.Time) error {
	monthSheetName := getMonthSheetName(date)

	sheetCacheMux.RLock()
	exists := sheetCache[monthSheetName]
	sheetCacheMux.RUnlock()

	if exists {
		return nil
	}

	sheetCacheMux.Lock()
	defer sheetCacheMux.Unlock()

	// Двойная проверка на случай, если лист был создан другой горутиной
	if _, ok := sheetCache[monthSheetName]; ok {
		return nil
	}

	if err := CopyAndPrepareSheet(sheetsService, config.SpreadsheetID, templateSheetID, date); err != nil {
		return fmt.Errorf("failed to create month sheet: %v", err)
	}

	sheetCache[monthSheetName] = true
	log.Printf("Created new month sheet: %s", monthSheetName)
	return nil
}

// verifyAccess проверяет доступ к таблице Google Sheets
func verifyAccess() error {
	_, err := sheetsService.Spreadsheets.Get(config.SpreadsheetID).Do()
	if err != nil {
		return fmt.Errorf("failed to access spreadsheet: %v", err)
	}
	log.Println("Spreadsheet access verified successfully")
	return nil
}

// loadConfig загружает конфигурацию из файла config.json или переменных окружения
func loadConfig() {
	config = Config{
		Port:            "8080",
		ProductionSheet: "Выпуск",
		TimesheetSheet:  "Табель",
	}

	if file, err := os.Open("config.json"); err == nil {
		defer file.Close()
		if err := json.NewDecoder(file).Decode(&config); err != nil {
			log.Printf("Error reading config.json: %v", err)
		}
	}

	if envID := os.Getenv("SPREADSHEET_ID"); envID != "" {
		config.SpreadsheetID = envID
	}

	if config.SpreadsheetID == "" {
		log.Fatal("SpreadsheetID must be specified")
	}
}

// initSheetsService инициализирует сервис для работы с Google Sheets API
func initSheetsService() error {
	creds, err := loadCredentials()
	if err != nil {
		return fmt.Errorf("failed to load credentials: %v", err)
	}

	conf, err := google.JWTConfigFromJSON(creds, sheets.SpreadsheetsScope)
	if err != nil {
		return fmt.Errorf("invalid credentials: %v", err)
	}

	ctx := context.Background()
	sheetsService, err = sheets.NewService(ctx, option.WithHTTPClient(conf.Client(ctx)))
	if err != nil {
		return fmt.Errorf("failed to create sheets service: %v", err)
	}

	return nil
}

// loadCredentials загружает учетные данные Google из переменной окружения или файла
func loadCredentials() ([]byte, error) {
	if base64Data := os.Getenv("GOOGLE_CREDENTIALS_BASE64"); base64Data != "" {
		data, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 credentials: %v", err)
		}
		log.Println("Using credentials from GOOGLE_CREDENTIALS_BASE64")
		return data, nil
	}

	if data, err := os.ReadFile("credentials.json"); err == nil {
		log.Println("Using credentials from credentials.json")
		return data, nil
	}

	return nil, fmt.Errorf("no credentials provided")
}

// appendProductionData добавляет данные о производстве в таблицу
func appendProductionData(data ProductionData) error {
	// Получаем все данные с листа "Выпуск"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := sheetsService.Spreadsheets.Values.Get(
		config.SpreadsheetID,
		fmt.Sprintf("%s!A:G", config.ProductionSheet),
	).Context(ctx).Do()

	if err != nil {
		return fmt.Errorf("failed to get sheet data: %v", err)
	}

	// Преобразуем существующие данные в массив строк
	var existingData [][]interface{}
	if resp.Values != nil {
		existingData = resp.Values
	}

	log.Printf("Existing data before processing: %+v", existingData)

	// Сохраняем шапку таблицы
	var header []interface{}
	if len(existingData) > 0 {
		header = existingData[0]
		existingData = existingData[1:] // Убираем шапку из данных для обработки
	}

	// Удаляем пустые строки
	var cleanedData [][]interface{}
	for _, row := range existingData {
		if len(row) > 0 && strings.TrimSpace(fmt.Sprintf("%v", row[0])) != "" {
			cleanedData = append(cleanedData, row)
		}
	}

	// Добавляем новую запись в массив данных
	newRow := []interface{}{
		data.Date,
		data.FullName,
		data.PartAndOperation,
		data.TotalParts,
		data.Defective,
		data.GoodParts,
		data.Notes,
	}
	cleanedData = append(cleanedData, newRow)

	// Сортируем данные по дате
	sort.Slice(cleanedData, func(i, j int) bool {
		dateStrI := fmt.Sprintf("%v", cleanedData[i][0])
		dateStrJ := fmt.Sprintf("%v", cleanedData[j][0])

		// Преобразуем формат даты DD.MM.YYYY -> YYYY-MM-DD
		dateI, errI := time.Parse("2006-01-02", convertDateFormat(dateStrI))
		dateJ, errJ := time.Parse("2006-01-02", convertDateFormat(dateStrJ))

		if errI != nil || errJ != nil {
			log.Printf("Error parsing dates: i=%d (%v), j=%d (%v)", i, dateStrI, j, dateStrJ)
			return false
		}

		return dateI.Before(dateJ)
	})

	// Добавляем пустые строки между разными датами
	var finalData [][]interface{}
	var prevDate string
	for _, row := range cleanedData {
		currentDate := fmt.Sprintf("%v", row[0])
		if prevDate != "" && currentDate != prevDate {
			finalData = append(finalData, []interface{}{}) // Пустая строка
		}
		finalData = append(finalData, row)
		prevDate = currentDate
	}

	// Добавляем шапку обратно
	finalData = append([][]interface{}{header}, finalData...)

	log.Printf("Final data after processing: %+v", finalData)

	// Обновляем данные на листе "Выпуск"
	rangeData := fmt.Sprintf("%s!A1:G", config.ProductionSheet)
	_, err = sheetsService.Spreadsheets.Values.Update(
		config.SpreadsheetID,
		rangeData,
		&sheets.ValueRange{Values: finalData},
	).ValueInputOption("USER_ENTERED").Context(ctx).Do()

	if err != nil {
		return fmt.Errorf("failed to update sheet: %v", err)
	}

	// Применяем форматирование (Arial 12, текст по центру)
	formatRequest := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{
			{
				RepeatCell: &sheets.RepeatCellRequest{
					Range: &sheets.GridRange{
						SheetId:          getSheetID(config.ProductionSheet),
						StartRowIndex:    1, // Пропускаем шапку
						StartColumnIndex: 0,
						EndColumnIndex:   7, // Столбцы A:G
					},
					Cell: &sheets.CellData{
						UserEnteredFormat: &sheets.CellFormat{
							HorizontalAlignment: "CENTER",
							TextFormat: &sheets.TextFormat{
								FontFamily: "Arial",
								FontSize:   12,
							},
						},
					},
					Fields: "userEnteredFormat(horizontalAlignment,textFormat)",
				},
			},
		},
	}

	_, err = sheetsService.Spreadsheets.BatchUpdate(config.SpreadsheetID, formatRequest).Do()
	if err != nil {
		return fmt.Errorf("failed to apply formatting: %v", err)
	}

	log.Printf("Production data successfully updated, sorted, and formatted.")
	return nil
}

// getSheetID возвращает ID листа по его названию
func getSheetID(sheetName string) int64 {
	resp, err := sheetsService.Spreadsheets.Get(config.SpreadsheetID).Fields("sheets(properties(sheetId,title))").Do()
	if err != nil {
		log.Fatalf("Failed to get spreadsheet: %v", err)
	}

	for _, sheet := range resp.Sheets {
		if sheet.Properties.Title == sheetName {
			return sheet.Properties.SheetId
		}
	}

	log.Fatalf("Sheet with name '%s' not found", sheetName)
	return 0
}

// convertDateFormat преобразует дату из формата DD.MM.YYYY в формат YYYY-MM-DD
func convertDateFormat(dateStr string) string {
	parts := strings.Split(dateStr, ".")
	if len(parts) == 3 {
		return fmt.Sprintf("%s-%s-%s", parts[2], parts[1], parts[0])
	}
	return dateStr // Возвращаем исходную строку, если формат некорректен
}

// findTimesheetCell находит ячейку в табеле учета времени для указанной даты и сотрудника
func findTimesheetCell(data TimesheetData) (string, int, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Parse input date to extract day
	inputDate, err := time.Parse("2006-01-02", data.Date)
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid date format, expected YYYY-MM-DD: %v", err)
	}
	dayToFind := inputDate.Day()
	monthSheetName := getMonthSheetName(inputDate)

	// Get names from column B (rows 4-12)
	respNames, err := sheetsService.Spreadsheets.Values.Get(
		config.SpreadsheetID,
		fmt.Sprintf("%s!B4:B12", monthSheetName),
	).Context(ctx).Do()

	if err != nil {
		return "", 0, 0, fmt.Errorf("failed to get names: %v", err)
	}

	var targetRow int
	for i, row := range respNames.Values {
		if len(row) > 0 && strings.TrimSpace(row[0].(string)) == data.FullName {
			targetRow = 4 + i
			break
		}
	}

	if targetRow == 0 {
		availableNames := []string{}
		for _, row := range respNames.Values {
			if len(row) > 0 {
				availableNames = append(availableNames, row[0].(string))
			}
		}
		return "", 0, 0, fmt.Errorf("full name '%s' not found. Available names: %v", data.FullName, availableNames)
	}

	// Get dates (day numbers) from row 3 (columns C:AG)
	respDays, err := sheetsService.Spreadsheets.Values.Get(
		config.SpreadsheetID,
		fmt.Sprintf("%s!C3:AG3", monthSheetName),
	).Context(ctx).Do()

	if err != nil {
		return "", 0, 0, fmt.Errorf("failed to get days: %v", err)
	}

	var targetCol int
	if len(respDays.Values) > 0 {
		for i, cell := range respDays.Values[0] {
			cellStr := fmt.Sprintf("%v", cell) // Convert any type to string
			cellStr = strings.TrimSpace(cellStr)

			cellDay, err := strconv.Atoi(cellStr)
			if err == nil && cellDay == dayToFind {
				targetCol = 3 + i // Column C is index 3
				break
			}
		}
	}

	if targetCol == 0 {
		availableDays := make([]string, 0)
		if len(respDays.Values) > 0 {
			for _, cell := range respDays.Values[0] {
				availableDays = append(availableDays, fmt.Sprintf("%v", cell))
			}
		}
		return "", 0, 0, fmt.Errorf("day %d not found in timesheet. Available days: %v", dayToFind, availableDays)
	}

	colLetter := columnToLetter(targetCol)
	return colLetter, targetRow, targetCol, nil
}

// columnToLetter преобразует номер столбца в буквенное обозначение (например, 1 -> A, 27 -> AA)
func columnToLetter(col int) string {
	letter := ""
	for col > 0 {
		col--
		letter = string(rune('A'+(col%26))) + letter
		col = col / 26
	}
	return letter
}

// appendTimesheetData добавляет данные в табель учета времени
func appendTimesheetData(data TimesheetData) error {
	inputDate, err := time.Parse("2006-01-02", data.Date)
	if err != nil {
		return fmt.Errorf("invalid date format: %v", err)
	}

	monthSheetName := getMonthSheetName(inputDate)

	// Перед записью данных убедимся, что лист видим
	if err := ensureSheetVisible(sheetsService, config.SpreadsheetID, monthSheetName); err != nil {
		return fmt.Errorf("failed to make sheet visible: %v", err)
	}

	colLetter, row, col, err := findTimesheetCell(data)
	if err != nil {
		return fmt.Errorf("failed to find cell: %v", err)
	}

	cell := fmt.Sprintf("%s!%s%d", monthSheetName, colLetter, row)
	log.Printf("Writing to cell %s (row %d, col %d)", cell, row, col)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err = sheetsService.Spreadsheets.Values.Update(
		config.SpreadsheetID,
		cell,
		&sheets.ValueRange{
			Values: [][]interface{}{{data.Hours}},
		},
	).ValueInputOption("USER_ENTERED").Context(ctx).Do()

	if err != nil {
		return fmt.Errorf("failed to update cell: %v", err)
	}

	log.Printf("Successfully wrote hours to %s", cell)
	return nil
}

// ensureSheetVisible делает указанный лист видимым и скрывает предыдущий активный
func ensureSheetVisible(srv *sheets.Service, spreadsheetID string, sheetName string) error {
	// Получаем список всех листов с информацией о видимости
	resp, err := srv.Spreadsheets.Get(spreadsheetID).Fields("sheets(properties(sheetId,title,hidden))").Do()
	if err != nil {
		return fmt.Errorf("failed to get spreadsheet: %v", err)
	}

	var (
		currentActiveTimesheetID int64
		targetSheetID            int64
		requests                 []*sheets.Request
	)

	// Находим текущий активный табельный лист (исключая лист "Выпуск") и нужный нам лист
	for _, sheet := range resp.Sheets {
		// Пропускаем лист "Выпуск" - он никогда не должен скрываться
		if sheet.Properties.Title == config.ProductionSheet {
			continue
		}

		if !sheet.Properties.Hidden && sheet.Properties.Title != sheetName {
			currentActiveTimesheetID = sheet.Properties.SheetId
			log.Printf("Found current active timesheet: %s (ID: %d)", sheet.Properties.Title, sheet.Properties.SheetId)
		}
		if sheet.Properties.Title == sheetName {
			targetSheetID = sheet.Properties.SheetId
			log.Printf("Found target sheet: %s (ID: %d, hidden: %v)",
				sheet.Properties.Title, sheet.Properties.SheetId, sheet.Properties.Hidden)
		}
	}

	if targetSheetID == 0 {
		return fmt.Errorf("target sheet '%s' not found", sheetName)
	}

	// Если текущий активный табельный лист существует и это не наш целевой лист
	if currentActiveTimesheetID != 0 && currentActiveTimesheetID != targetSheetID {
		requests = append(requests, &sheets.Request{
			UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
				Properties: &sheets.SheetProperties{
					SheetId: currentActiveTimesheetID,
					Hidden:  true,
				},
				Fields: "hidden",
			},
		})
		log.Printf("Preparing to hide timesheet ID: %d", currentActiveTimesheetID)
	}

	// Делаем целевой лист видимым и перемещаем его в начало (после листа "Выпуск")
	requests = append(requests, &sheets.Request{
		UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
			Properties: &sheets.SheetProperties{
				SheetId: targetSheetID,
				Hidden:  false,
				Index:   1, // Помещаем после листа "Выпуск" (который должен быть первым)
			},
			Fields: "hidden,index",
		},
	})
	log.Printf("Preparing to show and move to position 1 sheet ID: %d", targetSheetID)

	if len(requests) > 0 {
		batchUpdateRequest := &sheets.BatchUpdateSpreadsheetRequest{
			Requests: requests,
		}

		_, err = srv.Spreadsheets.BatchUpdate(spreadsheetID, batchUpdateRequest).Do()
		if err != nil {
			return fmt.Errorf("failed to switch sheet visibility: %v", err)
		}
		log.Printf("Successfully updated sheet visibility for %s", sheetName)
	} else {
		log.Printf("No visibility changes needed for %s", sheetName)
	}

	return nil
}

// productionHandler обрабатывает запросы на добавление данных о производстве
func productionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var data ProductionData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(data.FullName) == "" || strings.TrimSpace(data.PartAndOperation) == "" || strings.TrimSpace(data.TotalParts) == "" {
		http.Error(w, "Full name, part/operation and total parts are required", http.StatusBadRequest)
		return
	}

	if data.Date == "" {
		data.Date = time.Now().Format("2006-01-02")
	}

	if err := appendProductionData(data); err != nil {
		log.Printf("Error writing production data: %v", err)
		http.Error(w, "Failed to process data", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// timesheetHandler обрабатывает запросы на добавление данных в табель учета времени
func timesheetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var data TimesheetData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(data.FullName) == "" || strings.TrimSpace(data.Hours) == "" {
		http.Error(w, "Full name and hours are required", http.StatusBadRequest)
		return
	}

	if data.Date == "" {
		data.Date = time.Now().Format("2006-01-02")
	} else {
		if _, err := time.Parse("2006-01-02", data.Date); err != nil {
			http.Error(w, "Invalid date format, expected YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}

	inputDate, err := time.Parse("2006-01-02", data.Date)
	if err != nil {
		http.Error(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	// Создаем лист для месяца если его нет
	if err := handleMonthSheet(inputDate); err != nil {
		log.Printf("Error handling month sheet: %v", err)
		http.Error(w, fmt.Sprintf("Failed to prepare timesheet: %v", err), http.StatusInternalServerError)
		return
	}

	if _, err := strconv.ParseFloat(data.Hours, 64); err != nil {
		http.Error(w, "Hours must be a number", http.StatusBadRequest)
		return
	}

	if err := appendTimesheetData(data); err != nil {
		log.Printf("Error writing timesheet data: %v", err)
		http.Error(w, fmt.Sprintf("Failed to process data: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// healthHandler обрабатывает запросы проверки работоспособности сервиса
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Service is healthy"))
}

// enableCORS добавляет заголовки CORS для обработки кросс-доменных запросов
func enableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// CopyAndPrepareSheet копирует лист, переименовывает его и очищает значения
func CopyAndPrepareSheet(srv *sheets.Service, spreadsheetID string, sourceSheetID int64, date time.Time) error {
	// Копируем лист
	copyRequest := &sheets.CopySheetToAnotherSpreadsheetRequest{
		DestinationSpreadsheetId: spreadsheetID,
	}
	copyResponse, err := srv.Spreadsheets.Sheets.CopyTo(spreadsheetID, sourceSheetID, copyRequest).Do()
	if err != nil {
		return fmt.Errorf("failed to copy sheet: %v", err)
	}

	// Переименовываем новый лист
	newSheetID := copyResponse.SheetId
	newSheetName := getMonthSheetName(date)

	// Проверяем, существует ли лист с таким именем
	resp, err := srv.Spreadsheets.Get(spreadsheetID).Fields("sheets(properties(sheetId,title))").Do()
	if err != nil {
		return fmt.Errorf("failed to get spreadsheet: %v", err)
	}

	for _, sheet := range resp.Sheets {
		if sheet.Properties.Title == newSheetName {
			return fmt.Errorf("sheet with name '%s' already exists", newSheetName)
		}
	}

	// Создаем запросы для:
	// 1. Переименования нового листа
	// 2. Скрытия исходного листа-шаблона
	// 3. Добавления заголовка
	requests := []*sheets.Request{
		{
			UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
				Properties: &sheets.SheetProperties{
					SheetId: newSheetID,
					Title:   newSheetName,
				},
				Fields: "title",
			},
		},
		{
			UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
				Properties: &sheets.SheetProperties{
					SheetId: sourceSheetID,
					Hidden:  true,
				},
				Fields: "hidden",
			},
		},
	}

	// Добавляем запрос для установки заголовка
	monthNames := []string{
		"Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
		"Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь",
	}
	month := monthNames[date.Month()-1]
	year := date.Year()
	title := fmt.Sprintf("Табель учета рабочего времени за %s %d год", month, year)

	requests = append(requests, &sheets.Request{
		RepeatCell: &sheets.RepeatCellRequest{
			Range: &sheets.GridRange{
				SheetId:          newSheetID,
				StartRowIndex:    0,
				EndRowIndex:      1,
				StartColumnIndex: 0,
				EndColumnIndex:   26, // Z column
			},
			Cell: &sheets.CellData{
				UserEnteredValue: &sheets.ExtendedValue{
					StringValue: &title,
				},
			},
			Fields: "userEnteredValue",
		},
	})

	batchUpdateRequest := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: requests,
	}

	_, err = srv.Spreadsheets.BatchUpdate(spreadsheetID, batchUpdateRequest).Do()
	if err != nil {
		return fmt.Errorf("failed to rename sheet and hide template: %v", err)
	}

	// Очищаем значения в диапазоне C4:AG17
	clearRange := fmt.Sprintf("%s!C4:AG17", newSheetName)
	clearRequest := &sheets.ClearValuesRequest{}
	_, err = srv.Spreadsheets.Values.Clear(spreadsheetID, clearRange, clearRequest).Do()
	if err != nil {
		return fmt.Errorf("failed to clear values in range %s: %v", clearRange, err)
	}

	// Переключаем видимость листов
	if err := ensureSheetVisible(srv, spreadsheetID, newSheetName); err != nil {
		return fmt.Errorf("failed to switch sheet visibility: %v", err)
	}

	log.Printf("Sheet copied, renamed to '%s', cleared and activated successfully. Template sheet hidden.", newSheetName)
	return nil
}

// ListSheets выводит список листов с их ID
func ListSheets(srv *sheets.Service, spreadsheetID string) ([]*sheets.SheetProperties, error) {
	resp, err := srv.Spreadsheets.Get(spreadsheetID).Fields("sheets(properties(sheetId,title))").Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get spreadsheet: %v", err)
	}

	var sheetProperties []*sheets.SheetProperties
	for _, sheet := range resp.Sheets {
		sheetProperties = append(sheetProperties, sheet.Properties)
	}
	return sheetProperties, nil
}

// getDropdownDataHandler возвращает данные для выпадающего меню
func getDropdownDataHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Укажите диапазон, откуда брать данные для выпадающего меню
	rangeData := "Работники!A1:A" // Данные берутся с листа "Работники", начиная со второй строки столбца A

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := sheetsService.Spreadsheets.Values.Get(config.SpreadsheetID, rangeData).Context(ctx).Do()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get dropdown data: %v", err), http.StatusInternalServerError)
		return
	}

	// Преобразуем данные в массив строк
	var dropdownData []string
	for _, row := range resp.Values {
		if len(row) > 0 {
			dropdownData = append(dropdownData, fmt.Sprintf("%v", row[0]))
		}
	}

	// Возвращаем данные в формате JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dropdownData)
}

// getOperationsDataHandler возвращает данные для выпадающего меню "Название детали и операции"
func getOperationsDataHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Укажите диапазон, откуда брать данные для выпадающего меню
	rangeData := "Норма выпуска!A1:A" // Данные берутся с листа "Норма выпуска", начиная с первой строки столбца A

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := sheetsService.Spreadsheets.Values.Get(config.SpreadsheetID, rangeData).Context(ctx).Do()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get operations data: %v", err), http.StatusInternalServerError)
		return
	}

	// Преобразуем данные в массив строк
	var operationsData []string
	for _, row := range resp.Values {
		if len(row) > 0 {
			operationsData = append(operationsData, fmt.Sprintf("%v", row[0]))
		}
	}

	// Возвращаем данные в формате JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(operationsData)
}
