# Используем Go для сборки
FROM golang:1.23-alpine AS builder

# Устанавливаем рабочую директорию
WORKDIR /app

# Копируем go.mod и go.sum
COPY go.mod ./
COPY go.sum ./

# Загружаем зависимости
RUN go mod download

# Копируем исходный код
COPY main.go ./
COPY services/ ./services/
COPY handlers/ ./handlers/
COPY templates/ ./templates/

# Компилируем приложение
RUN go build -o web-barcode-scanner main.go

# Финальный минимальный образ
FROM alpine:3.20

# Устанавливаем рабочую директорию
WORKDIR /app

# Копируем бинарник из builder
COPY --from=builder /app/web-barcode-scanner ./

# Копируем шаблоны, .env и сертификаты
COPY templates/ ./templates/
COPY .env ./
COPY cert.pem ./
COPY key.pem ./

# Открываем порт
EXPOSE 8080

# Запускаем приложение
CMD ["./web-barcode-scanner"]