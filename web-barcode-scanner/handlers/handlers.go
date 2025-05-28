package handlers

import (
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

func NewHandler(barcodeService *services.BarcodeService) *Handler {
	tmpl := template.Must(template.ParseFiles("templates/index.html", "templates/result.html"))
	return &Handler{barcodeService: barcodeService, templates: tmpl}
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	if err := h.templates.ExecuteTemplate(w, "index.html", nil); err != nil {
		log.Printf("Ошибка выполнения шаблона index: %v", err)
		http.Error(w, "Ошибка сервера", http.StatusInternalServerError)
	}
}

func (h *Handler) Scan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не разрешен", http.StatusMethodNotAllowed)
		return
	}

	barcode := r.FormValue("barcode")
	if barcode == "" {
		http.Error(w, "Штрихкод не предоставлен", http.StatusBadRequest)
		return
	}

	log.Printf("Получен штрихкод: %s", barcode)

	item, err := h.barcodeService.FetchBarcodeData(barcode)
	data := &ResultData{}
	if err != nil {
		log.Printf("Ошибка получения данных штрихкода: %v", err)
		data.Error = err.Error() // Передаём ошибку как строку
	} else {
		data.Item = item
	}

	if err := h.templates.ExecuteTemplate(w, "result.html", data); err != nil {
		log.Printf("Ошибка выполнения шаблона result: %v", err)
		http.Error(w, "Ошибка сервера", http.StatusInternalServerError)
	}
}
