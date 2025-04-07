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

type ProductionData struct {
	Date             string `json:"date"`
	FullName         string `json:"fullName"`
	PartAndOperation string `json:"partAndOperation"`
	TotalParts       string `json:"totalParts"`
	Defective        string `json:"defective"`
	GoodParts        string `json:"goodParts"`
	Notes            string `json:"notes"`
}

func AppendProductionData(srv *sheets.Service, cfg config.Config, data ProductionData) error {
	lastRow, err := findLastNonEmptyRow(srv, cfg.SpreadsheetID, cfg.ProductionSheet)
	if err != nil {
		return fmt.Errorf("failed to find last row: %v", err)
	}

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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rangeData := fmt.Sprintf("%s!A%d:G%d", cfg.ProductionSheet, targetRow, targetRow)
	_, err = srv.Spreadsheets.Values.Update(
		cfg.SpreadsheetID,
		rangeData,
		&sheets.ValueRange{Values: values},
	).ValueInputOption("USER_ENTERED").Context(ctx).Do()

	if err != nil {
		return fmt.Errorf("failed to update sheet: %v", err)
	}

	log.Printf("Production data written to row %d", targetRow)
	return nil
}

func findLastNonEmptyRow(srv *sheets.Service, spreadsheetID, sheetName string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := srv.Spreadsheets.Values.Get(
		spreadsheetID,
		fmt.Sprintf("%s!A:A", sheetName),
	).Context(ctx).Do()

	if err != nil {
		return 0, fmt.Errorf("failed to get sheet data: %v", err)
	}

	lastNonEmpty := 0
	for i, row := range resp.Values {
		if len(row) > 0 && strings.TrimSpace(row[0].(string)) != "" {
			lastNonEmpty = i + 1
		}
	}

	return lastNonEmpty, nil
}
