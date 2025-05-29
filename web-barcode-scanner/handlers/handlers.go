package handlers

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"

	"web-barcode-scanner/models"
	"web-barcode-scanner/services"
)

type Handler struct {
	barcodeService *services.BarcodeService
	templates      *template.Template
}

type ResultData struct {
	Item  *models.ItemResponse
	Error string
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func NewHandler(barcodeService *services.BarcodeService) *Handler {
	tmpl := template.Must(template.ParseFiles("templates/index.html", "templates/result.html"))
	return &Handler{barcodeService: barcodeService, templates: tmpl}
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("[Index] Запрос: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
	if err := h.templates.ExecuteTemplate(w, "index.html", nil); err != nil {
		log.Printf("[Index] Ошибка выполнения шаблона: %v", err)
		http.Error(w, "Ошибка сервера", http.StatusInternalServerError)
	}
}

func (h *Handler) Scan(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		log.Printf("[Scan] Неподдерживаемый метод: %s from %s", r.Method, r.RemoteAddr)
		http.Error(w, "Метод не разрешен", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		log.Printf("[Scan] Ошибка разбора формы: %v", err)
		sendErrorJSON(w, "Ошибка формы", http.StatusBadRequest)
		return
	}

	barcode := r.FormValue("barcode")
	if barcode == "" {
		log.Printf("[Scan] Штрихкод не предоставлен from %s", r.RemoteAddr)
		sendErrorJSON(w, "Штрихкод не предоставлен", http.StatusBadRequest)
		return
	}

	log.Printf("[Scan] Получен штрихкод: %s from %s", barcode, r.RemoteAddr)

	item, err := h.barcodeService.FetchBarcodeData(barcode)
	data := &ResultData{}
	if err != nil {
		log.Printf("[Scan] Ошибка получения данных штрихкода: %v", err)
		data.Error = err.Error()
	} else {
		data.Item = item
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, "result.html", data); err != nil {
		log.Printf("[Scan] Ошибка выполнения шаблона result: %v", err)
		sendErrorJSON(w, "Ошибка сервера", http.StatusInternalServerError)
	}
}

func sendErrorJSON(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	errResp := ErrorResponse{Error: message}
	if err := json.NewEncoder(w).Encode(errResp); err != nil {
		log.Printf("[sendErrorJSON] Ошибка кодирования JSON: %v", err)
	}
}
