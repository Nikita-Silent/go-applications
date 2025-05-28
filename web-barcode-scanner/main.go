package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"web-barcode-scanner/handlers"
	"web-barcode-scanner/services"

	"github.com/joho/godotenv"
)

func main() {
	// Загрузка .env
	if err := godotenv.Load(); err != nil {
		log.Fatalf("Ошибка загрузки файла .env: %v", err)
	}

	// Получение конфигурации из .env
	config := services.Config{
		APIURL:   os.Getenv("API_URL"),
		APIToken: os.Getenv("API_TOKEN"),
		APIAuth:  os.Getenv("API_AUTH"),
	}
	if config.APIURL == "" || config.APIToken == "" || config.APIAuth == "" {
		log.Fatal("Ошибка: Не указаны API_URL, API_TOKEN или API_AUTH в .env")
	}

	// Создание сервиса
	barcodeService := services.NewBarcodeService(config)

	// Получение путей к сертификатам
	dir, err := os.Getwd()
	if err != nil {
		log.Fatalf("Ошибка получения текущей директории: %v", err)
	}
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	// Проверка сертификатов
	if _, err := os.Stat(certPath); err != nil {
		log.Fatalf("Файл сертификата не найден: %s", certPath)
	}
	if _, err := os.Stat(keyPath); err != nil {
		log.Fatalf("Файл ключа не найден: %s", keyPath)
	}

	// Настройка маршрутов
	handler := handlers.NewHandler(barcodeService)
	http.HandleFunc("/", handler.Index)
	http.HandleFunc("/scan", handler.Scan)

	log.Printf("Сервер запускается на :8080 с HTTPS, сертификат: %s, ключ: %s", certPath, keyPath)
	if err := http.ListenAndServeTLS(":8080", certPath, keyPath, nil); err != nil {
		log.Fatalf("Ошибка запуска сервера: %v", err)
	}
}
