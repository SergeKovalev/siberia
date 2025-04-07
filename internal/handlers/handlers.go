package handlers

import (
	"net/http"

	"google.golang.org/api/sheets/v4"

	"github.com/sergekovalev/siberia/internal/config"
	"github.com/sergekovalev/siberia/internal/utils"
)

func InitHandlers(srv *sheets.Service, cfg config.Config) {
	http.HandleFunc("/submit-production", utils.EnableCORS(ProductionHandler(srv, cfg)))
	http.HandleFunc("/submit-timesheet", utils.EnableCORS(TimesheetHandler(srv, cfg)))
	http.HandleFunc("/health", HealthHandler)
}
