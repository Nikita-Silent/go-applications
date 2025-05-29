package main

import (
	"log"
	"net/http"
	"os"

	"web-barcode-scanner/handlers"
	"web-barcode-scanner/services"

	"github.com/joho/godotenv"
)

func main() {
	log.Println("[Main] Запуск приложения")

	// Загрузка .env файла
	if err := godotenv.Load(); err != nil {
		log.Printf("[Main] Не удалось загрузить .env файл: %v, продолжаем с переменными окружения", err)
	}

	// Чтение переменных окружения
	baseURL := os.Getenv("BASE_URL")
	token := os.Getenv("TOKEN")
	auth := os.Getenv("AUTH")

	// Проверка наличия переменных
	if baseURL == "" || token == "" || auth == "" {
		log.Fatal("[Main] Ошибка: отсутствуют необходимые переменные окружения (BASE_URL, TOKEN, AUTH)")
	}

	log.Printf("[Main] Конфигурация: BASE_URL=%s, TOKEN=%s, AUTH=%s", baseURL, token, auth)

	// Инициализация сервиса
	barcodeService := services.NewBarcodeService(baseURL, token, auth)
	handler := handlers.NewHandler(barcodeService)

	// Настройка маршрутов
	http.HandleFunc("/", handler.Index)
	http.HandleFunc("/scan", handler.Scan)

	// Запуск HTTPS-сервера
	log.Println("[Main] Запуск сервера на :8080")
	if err := http.ListenAndServeTLS(":8080", "cert.pem", "key.pem", nil); err != nil {
		log.Fatalf("[Main] Ошибка запуска сервера: %v", err)
	}
}
