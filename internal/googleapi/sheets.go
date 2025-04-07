package googleapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"project/internal/config"
)

func InitSheetsService(cfg config.Config) (*sheets.Service, error) {
	creds, err := loadCredentials()
	if err != nil {
		return nil, fmt.Errorf("failed to load credentials: %v", err)
	}

	conf, err := google.JWTConfigFromJSON(creds, sheets.SpreadsheetsScope)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials: %v", err)
	}

	ctx := context.Background()
	sheetsService, err := sheets.NewService(ctx, option.WithHTTPClient(conf.Client(ctx)))
	if err != nil {
		return nil, fmt.Errorf("failed to create sheets service: %v", err)
	}

	return sheetsService, nil
}

func VerifyAccess(service *sheets.Service, spreadsheetID string) error {
	_, err := service.Spreadsheets.Get(spreadsheetID).Do()
	if err != nil {
		return fmt.Errorf("failed to access spreadsheet: %v", err)
	}
	log.Println("Spreadsheet access verified successfully")
	return nil
}

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
