package models

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/sheets/v4"
)

type TimesheetData struct {
	Date     string `json:"date"`
	FullName string `json:"fullName"`
	Hours    string `json:"hours"`
}

func AppendTimesheetData(srv *sheets.Service, spreadsheetID string, data TimesheetData) error {
	colLetter, row, col, err := findTimesheetCell(srv, spreadsheetID, data)
	if err != nil {
		return fmt.Errorf("failed to find cell: %v", err)
	}

	cell := fmt.Sprintf("Табель!%s%d", colLetter, row)
	log.Printf("Writing to cell %s (row %d, col %d)", cell, row, col)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err = srv.Spreadsheets.Values.Update(
		spreadsheetID,
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

func findTimesheetCell(srv *sheets.Service, spreadsheetID string, data TimesheetData) (string, int, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Parse input date to extract day
	inputDate, err := time.Parse("2006-01-02", data.Date)
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid date format, expected YYYY-MM-DD: %v", err)
	}
	dayToFind := inputDate.Day()

	// Get names from column B (rows 4-12)
	respNames, err := srv.Spreadsheets.Values.Get(
		spreadsheetID,
		"Табель!B4:B12",
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
		return "", 0, 0, fmt.Errorf("full name '%s' not found in timesheet", data.FullName)
	}

	// Get dates (day numbers) from row 3 (columns C:AG)
	respDays, err := srv.Spreadsheets.Values.Get(
		spreadsheetID,
		"Табель!C3:AG3",
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

	colLetter := utils.ColumnToLetter(targetCol)
	return colLetter, targetRow, targetCol, nil
}
