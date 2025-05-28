package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"

	"web-barcode-scanner/models"
)

type Config struct {
	APIURL   string
	APIToken string
	APIAuth  string
}

type BarcodeService struct {
	config Config
}

func NewBarcodeService(config Config) *BarcodeService {
	return &BarcodeService{config: config}
}

func (s *BarcodeService) FetchBarcodeData(barcode string) (*models.ItemResponse, error) {
	// Формируем команду curl
	url := fmt.Sprintf("%s?barcode=%s", s.config.APIURL, barcode)
	cmd := exec.Command("curl",
		"-H", fmt.Sprintf("Token: %s", s.config.APIToken),
		"-H", fmt.Sprintf("Authorization: %s", s.config.APIAuth), // Исправлено: добавлено "Authorization:"
		url)

	log.Printf("Выполняется запрос: curl -H 'Token: %s' -H 'Authorization: %s' %s", s.config.APIToken, s.config.APIAuth, url)

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Printf("Ошибка cURL (stderr: %s)", stderr.String())
		return nil, fmt.Errorf("ошибка cURL: %v", err)
	}

	// Проверяем пустой ответ
	if out.String() == "" {
		log.Printf("API вернул пустой ответ для штрихкода: %s", barcode)
		return nil, fmt.Errorf("API вернул пустой ответ")
	}

	log.Printf("Ответ API: %s", out.String()) // Логируем ответ API

	var item models.ItemResponse
	if err := json.Unmarshal(out.Bytes(), &item); err != nil {
		log.Printf("Ошибка разбора JSON: %v", err)
		return nil, fmt.Errorf("ошибка разбора JSON: %v", err)
	}

	// Проверка на пустой ответ
	if item.Name == "" && len(item.Stock) == 0 {
		log.Printf("Штрихкод %s не найден в API", barcode)
		return nil, fmt.Errorf("штрихкод %s не найден", barcode)
	}

	return &item, nil
}
