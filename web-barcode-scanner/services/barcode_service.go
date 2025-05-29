package services

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"web-barcode-scanner/models"
)

type BarcodeService struct {
	client  *http.Client
	baseURL string
	token   string
	auth    string
}

func NewBarcodeService(baseURL, token, auth string) *BarcodeService {
	return &BarcodeService{
		client:  &http.Client{},
		baseURL: baseURL,
		token:   token,
		auth:    auth,
	}
}

func (s *BarcodeService) FetchBarcodeData(barcode string) (*models.ItemResponse, error) {
	url := fmt.Sprintf("%s?barcode=%s", s.baseURL, barcode)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("[FetchBarcodeData] Ошибка создания запроса: %v", err)
		return nil, fmt.Errorf("ошибка запроса: %v", err)
	}

	req.Header.Set("Token", s.token)
	req.Header.Set("Authorization", fmt.Sprintf("Basic %s", s.auth))

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("[FetchBarcodeData] Ошибка выполнения запроса: %v", err)
		return nil, fmt.Errorf("ошибка выполнения: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[FetchBarcodeData] Ошибка чтения ответа: %v", err)
		return nil, fmt.Errorf("ошибка чтения: %v", err)
	}

	body := string(bodyBytes)
	log.Printf("[FetchBarcodeData] Ответ API: %s", body)

	if resp.StatusCode != http.StatusOK {
		log.Printf("[FetchBarcodeData] Неверный статус: %d, тело: %s", resp.StatusCode, body)
		if strings.Contains(body, "Не найден товар") {
			return nil, fmt.Errorf("товар не найден по штрихкоду: %s", barcode)
		}
		return nil, fmt.Errorf("ошибка API: статус %d, ответ: %s", resp.StatusCode, body)
	}

	var itemResponse models.ItemResponse
	if err := json.Unmarshal(bodyBytes, &itemResponse); err != nil {
		log.Printf("[FetchBarcodeData] Ошибка разбора JSON: %v, тело: %s", err, body)
		if strings.Contains(body, "Не найден товар") {
			return nil, fmt.Errorf("товар не найден по штрихкоду: %s", barcode)
		}
		return nil, fmt.Errorf("ошибка разбора JSON: %v", err)
	}

	return &itemResponse, nil
}
