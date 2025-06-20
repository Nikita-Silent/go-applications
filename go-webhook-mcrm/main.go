package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type MCRMResponse struct {
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Phone      string `json:"phone"`
	CardNumber string `json:"card_number"`
	Email      string `json:"email"`
}

type ListmonkCreateResponse struct {
	Data struct {
		ID      int                    `json:"id"`
		Email   string                 `json:"email"`
		Attribs map[string]interface{} `json:"attribs"`
		Phone   string                 `json:"phone"` // Предполагаем, что phone может быть в attribs
		Status  string                 `json:"status"`
		Lists   []struct {
			ID int `json:"id"`
		} `json:"lists"`
	} `json:"data"`
}

type ListmonkGetResponse struct {
	Data struct {
		ID        int                    `json:"id"`
		CreatedAt string                 `json:"created_at"`
		UpdatedAt string                 `json:"updated_at"`
		UUID      string                 `json:"uuid"`
		Email     string                 `json:"email"`
		Name      string                 `json:"name"`
		Attribs   map[string]interface{} `json:"attribs"`
		Status    string                 `json:"status"`
		Lists     []struct {
			SubscriptionStatus    string                 `json:"subscription_status"`
			SubscriptionCreatedAt string                 `json:"subscription_created_at"`
			SubscriptionUpdatedAt string                 `json:"subscription_updated_at"`
			SubscriptionMeta      map[string]interface{} `json:"subscription_meta"`
			ID                    int                    `json:"id"`
			UUID                  string                 `json:"uuid"`
			Name                  string                 `json:"name"`
			Type                  string                 `json:"type"`
			Optin                 string                 `json:"optin"`
			Tags                  []string               `json:"tags"`
			Description           string                 `json:"description"`
			CreatedAt             string                 `json:"created_at"`
			UpdatedAt             string                 `json:"updated_at"`
		} `json:"lists"`
	} `json:"data"`
}

type ListmonkSubscriberListResponse struct {
	Data []struct {
		ID      int                    `json:"id"`
		Email   string                 `json:"email"`
		Attribs map[string]interface{} `json:"attribs"`
		Phone   string                 `json:"phone"`
		Status  string                 `json:"status"`
		Lists   []struct {
			ID int `json:"id"`
		} `json:"lists"`
	} `json:"data"`
}

type LogEntry struct {
	ErrorMessage string `json:"error_message"`
	Timestamp    string `json:"timestamp"`
	Response     string `json:"response"`
}

type RetryEntry struct {
	ID           string `json:"id"` // Изменено на string для соответствия UUID
	Serial       string `json:"serial"`
	Event        string `json:"event"`
	RetryCount   int    `json:"retry_count"`
	ErrorMessage string `json:"error_message"`
	Timestamp    string `json:"timestamp"`
}

type SubscriberEntry struct {
	ID          string `json:"id"`  // Изменено на string для соответствия UUID
	UID         int    `json:"uid"` // Идентификатор из Listmonk
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	BonusStatus bool   `json:"bonus_status"` // Новое поле с значением по умолчанию false
}

func cleanSerial(serial string) string {
	if strings.Contains(serial, "-") {
		return strings.Split(serial, "-")[0]
	}
	return serial
}

func logToPocketBase(pbURL, collection string, data interface{}, updateID string) (string, error) {
	if pbURL == "" {
		return "", fmt.Errorf("POCKETBASE_URL is not set")
	}

	var url string
	var method string
	if updateID == "" {
		url = fmt.Sprintf("%s/api/collections/%s/records", pbURL, collection)
		method = http.MethodPost
	} else {
		url = fmt.Sprintf("%s/api/collections/%s/records/%s", pbURL, collection, updateID)
		method = http.MethodPatch
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(method, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("POCKETBASE_ADMIN_TOKEN"))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error: %d, %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %v", err)
	}
	if id, ok := result["id"].(string); ok {
		return id, nil
	}
	// Если это обновление, возвращаем существующий ID
	if updateID != "" {
		return updateID, nil
	}
	return "", fmt.Errorf("no ID returned from PocketBase, response: %v", result)
}

func updateRetryEntry(pbURL string, entry RetryEntry) error {
	url := fmt.Sprintf("%s/api/collections/retry/records/%s", pbURL, entry.ID)
	jsonData, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("POCKETBASE_ADMIN_TOKEN"))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %d, %s", resp.StatusCode, string(body))
	}

	return nil
}

func processWebhook(c echo.Context) error {
	// Логируем заголовки запроса
	log.Printf("Webhook headers: %v", c.Request().Header)

	// Читаем тело запроса
	bodyBytes, err := io.ReadAll(c.Request().Body)
	if err != nil {
		logError("Failed to read webhook body:", err.Error())
		return c.NoContent(http.StatusInternalServerError)
	}
	// Восстанавливаем тело запроса для дальнейшей обработки
	c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	log.Printf("Received webhook body: %s", string(bodyBytes))

	// Извлечение параметров из query string (на случай, если они там)
	serialFromQuery := c.QueryParam("serial")
	eventFromQuery := c.QueryParam("event")
	log.Printf("Extracted from query: serial=%s, event=%s", serialFromQuery, eventFromQuery)

	// Извлечение параметров из тела как формы
	serial := c.FormValue("serial")
	event := c.FormValue("event")
	cleanedSerial := cleanSerial(serial)
	log.Printf("Extracted from form body: serial=%s, event=%s, cleanedSerial=%s", serial, event, cleanedSerial)

	if serial == "" || event == "" {
		logError("Missing serial or event in webhook", fmt.Sprintf("Query: serial=%s, event=%s, Body: %s", serialFromQuery, eventFromQuery, string(bodyBytes)))
		return c.NoContent(http.StatusBadRequest)
	}

	log.Printf("Received webhook: serial=%s, event=%s, cleanedSerial=%s", serial, event, cleanedSerial)

	mcrmURL := os.Getenv("MCRM_API_URL_USER")
	apiKey := os.Getenv("MCRM_API_KEY")
	if apiKey == "" {
		logError("MCRM API key is not set", "Please set MCRM_API_KEY environment variable")
		return c.NoContent(http.StatusInternalServerError)
	}

	req, err := http.NewRequest("POST", mcrmURL, bytes.NewBufferString(fmt.Sprintf(`{"number":"%s"}`, cleanedSerial)))
	if err != nil {
		logError("MCRM request error:", err.Error())
		return c.NoContent(http.StatusInternalServerError)
	}
	req.Header.Set("x-api-key", apiKey)
	log.Printf("MCRM request - URL: %s, x-api-key set to: %s", mcrmURL, apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logError("MCRM API error:", err.Error())
		addToRetry(os.Getenv("POCKETBASE_URL"), serial, event, err.Error())
		return c.NoContent(http.StatusInternalServerError)
	}
	defer resp.Body.Close()

	mcrmBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logError("Failed to read MCRM response body:", err.Error())
		addToRetry(os.Getenv("POCKETBASE_URL"), serial, event, err.Error())
		return c.NoContent(http.StatusInternalServerError)
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewBuffer(mcrmBody))

	if resp.StatusCode != http.StatusOK {
		logError(fmt.Sprintf("MCRM API error: status %d, response: %s", resp.StatusCode, string(mcrmBody)), "")
		addToRetry(os.Getenv("POCKETBASE_URL"), serial, event, fmt.Sprintf("Status: %d, Response: %s", resp.StatusCode, string(mcrmBody)))
		return c.NoContent(http.StatusInternalServerError)
	}

	var mcrmData MCRMResponse
	if err := json.NewDecoder(io.NopCloser(bytes.NewBuffer(mcrmBody))).Decode(&mcrmData); err != nil {
		logError("MCRM decode error:", fmt.Sprintf("Error: %v, Response: %s", err, string(mcrmBody)))
		addToRetry(os.Getenv("POCKETBASE_URL"), serial, event, fmt.Sprintf("Error: %v, Response: %s", err, string(mcrmBody)))
		return c.NoContent(http.StatusInternalServerError)
	}

	listmonkURL := os.Getenv("LISTMONK_API_URL")
	listID, err := strconv.Atoi(os.Getenv("LIST_ID"))
	if err != nil {
		logError("Invalid LIST_ID:", err.Error())
		return c.NoContent(http.StatusInternalServerError)
	}
	listmonkPayload := map[string]interface{}{
		"email":  mcrmData.Email,
		"name":   fmt.Sprintf("%s %s", mcrmData.FirstName, mcrmData.LastName),
		"status": "enabled",
		"lists":  []int{listID},
		"attribs": map[string]interface{}{
			"phone":       mcrmData.Phone,
			"card_number": mcrmData.CardNumber,
		},
	}
	jsonPayload, _ := json.Marshal(listmonkPayload)

	req, err = http.NewRequest("POST", listmonkURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		logError("Listmonk request error:", err.Error())
		return c.NoContent(http.StatusInternalServerError)
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(os.Getenv("LISTMONK_USERNAME")+":"+os.Getenv("LISTMONK_API_KEY"))))
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		logError("Listmonk API error:", err.Error())
		addToRetry(os.Getenv("POCKETBASE_URL"), serial, event, err.Error())
		return c.NoContent(http.StatusInternalServerError)
	}
	defer resp.Body.Close()

	listmonkBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logError("Failed to read Listmonk response body:", err.Error())
		addToRetry(os.Getenv("POCKETBASE_URL"), serial, event, err.Error())
		return c.NoContent(http.StatusInternalServerError)
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewBuffer(listmonkBody))

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		logError(fmt.Sprintf("Listmonk API error: status %d", resp.StatusCode), string(listmonkBody))
		addToRetry(os.Getenv("POCKETBASE_URL"), serial, event, string(listmonkBody))
		return c.NoContent(http.StatusInternalServerError)
	}

	var listmonkResp ListmonkCreateResponse
	if err := json.NewDecoder(io.NopCloser(bytes.NewBuffer(listmonkBody))).Decode(&listmonkResp); err != nil {
		logError("Listmonk decode error:", fmt.Sprintf("Error: %v, Response: %s", err, string(listmonkBody)))
		addToRetry(os.Getenv("POCKETBASE_URL"), serial, event, fmt.Sprintf("Error: %v, Response: %s", err, string(listmonkBody)))
		return c.NoContent(http.StatusInternalServerError)
	}

	phone := ""
	if attribs, ok := listmonkResp.Data.Attribs["phone"]; ok {
		if phoneStr, ok := attribs.(string); ok {
			phone = phoneStr
		}
	}
	subscriber := SubscriberEntry{
		UID:         listmonkResp.Data.ID,
		Email:       listmonkResp.Data.Email,
		Phone:       phone,
		BonusStatus: false, // Значение по умолчанию
	}
	id, err := logToPocketBase(os.Getenv("POCKETBASE_URL"), "subscribers", subscriber, "")
	if err != nil {
		logError("Subscriber save error:", err.Error())
		return c.NoContent(http.StatusInternalServerError)
	}
	subscriber.ID = id // Сохраняем ID записи в PocketBase

	logEntry := LogEntry{
		ErrorMessage: "Webhook processed successfully",
		Timestamp:    time.Now().Format(time.RFC3339),
		Response:     fmt.Sprintf("Subscriber UID: %d", listmonkResp.Data.ID),
	}
	if _, err := logToPocketBase(os.Getenv("POCKETBASE_URL"), "logs", logEntry, ""); err != nil {
		logError("Log save error:", err.Error())
	}

	go checkSubscriptions(os.Getenv("POCKETBASE_URL"), apiKey)

	return c.NoContent(http.StatusOK)
}

func logError(message, details string) {
	logEntry := LogEntry{
		ErrorMessage: message,
		Timestamp:    time.Now().Format(time.RFC3339),
		Response:     details,
	}
	if _, err := logToPocketBase(os.Getenv("POCKETBASE_URL"), "logs", logEntry, ""); err != nil {
		log.Printf("Failed to log error to PocketBase: %v", err)
	}
}

func addToRetry(pbURL, serial, event, errorMessage string) error {
	retryEntry := RetryEntry{
		Serial:       serial,
		Event:        event,
		RetryCount:   0,
		ErrorMessage: errorMessage,
		Timestamp:    time.Now().Format(time.RFC3339),
	}
	id, err := logToPocketBase(pbURL, "retry", retryEntry, "")
	if err != nil {
		logError("Retry save error:", err.Error())
		return err
	}
	// Сохраняем ID в retryEntry для последующего использования
	retryEntry.ID = id
	return nil
}

func processRetry(pbURL, apiKey string) {
	client := &http.Client{Timeout: 10 * time.Second}
	maxRetries := 5

	for {
		resp, err := http.Get(fmt.Sprintf("%s/api/collections/retry/records", pbURL))
		if err != nil {
			logError("Failed to fetch retry entries:", err.Error())
			time.Sleep(60 * time.Second)
			continue
		}
		defer resp.Body.Close()

		var retryData struct {
			Items []RetryEntry `json:"items"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&retryData); err != nil {
			logError("Failed to decode retry entries:", err.Error())
			time.Sleep(60 * time.Second)
			continue
		}

		for _, entry := range retryData.Items {
			if entry.RetryCount >= maxRetries {
				logError(fmt.Sprintf("Max retries reached for serial %s", entry.Serial), "Removing from retry")
				if err := deleteRetryEntry(pbURL, entry.ID); err != nil {
					logError("Failed to delete retry entry:", err.Error())
				}
				continue
			}

			req, err := http.NewRequest("POST", os.Getenv("MCRM_API_URL_USER"), bytes.NewBufferString(fmt.Sprintf(`{"number":"%s"}`, entry.Serial)))
			if err != nil {
				logError("Retry request error:", err.Error())
				continue
			}
			req.Header.Set("x-api-key", apiKey)
			req.Header.Set("Content-Type", "application/json")

			resp, err = client.Do(req)
			if err != nil || resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				entry.RetryCount++
				logError(fmt.Sprintf("Retry failed for serial %s, attempt %d", entry.Serial, entry.RetryCount), fmt.Sprintf("Status: %d, Body: %s", resp.StatusCode, string(body)))
				if err := updateRetryEntry(pbURL, entry); err != nil {
					logError("Failed to update retry entry:", err.Error())
				}
				time.Sleep(1 * time.Minute)
				continue
			}

			// Читаем тело ответа MCRM
			body, _ := io.ReadAll(resp.Body)
			log.Printf("MCRM response: %s", string(body))

			// Декодируем ответ MCRM
			var mcrmData MCRMResponse
			if err := json.NewDecoder(bytes.NewReader(body)).Decode(&mcrmData); err != nil {
				logError("MCRM decode error:", fmt.Sprintf("Error: %v, Response: %s", err, string(body)))
				entry.RetryCount++
				if err := updateRetryEntry(pbURL, entry); err != nil {
					logError("Failed to update retry entry:", err.Error())
				}
				continue
			}

			// Логика создания записи в Listmonk и PocketBase
			listmonkURL := os.Getenv("LISTMONK_API_URL")
			listID, err := strconv.Atoi(os.Getenv("LIST_ID"))
			if err != nil {
				logError("Invalid LIST_ID:", err.Error())
				continue
			}
			listmonkPayload := map[string]interface{}{
				"email":  mcrmData.Email,
				"name":   fmt.Sprintf("%s %s", mcrmData.FirstName, mcrmData.LastName),
				"status": "enabled",
				"lists":  []int{listID},
				"attribs": map[string]interface{}{
					"phone":       mcrmData.Phone,
					"card_number": mcrmData.CardNumber,
				},
			}
			jsonPayload, _ := json.Marshal(listmonkPayload)

			req, err = http.NewRequest("POST", listmonkURL, bytes.NewBuffer(jsonPayload))
			if err != nil {
				logError("Listmonk request error:", err.Error())
				continue
			}
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(os.Getenv("LISTMONK_USERNAME")+":"+os.Getenv("LISTMONK_API_KEY"))))
			req.Header.Set("Content-Type", "application/json")

			listmonkResp, err := client.Do(req)
			if err != nil || listmonkResp.StatusCode != http.StatusOK && listmonkResp.StatusCode != http.StatusCreated {
				listmonkBody, _ := io.ReadAll(listmonkResp.Body)
				logError(fmt.Sprintf("Listmonk API error: status %d", listmonkResp.StatusCode), string(listmonkBody))
				entry.RetryCount++
				if err := updateRetryEntry(pbURL, entry); err != nil {
					logError("Failed to update retry entry:", err.Error())
				}
				continue
			}

			listmonkBody, _ := io.ReadAll(listmonkResp.Body)
			var listmonkCreateResp ListmonkCreateResponse
			if err := json.NewDecoder(bytes.NewReader(listmonkBody)).Decode(&listmonkCreateResp); err != nil {
				logError("Listmonk decode error:", fmt.Sprintf("Error: %v, Response: %s", err, string(listmonkBody)))
				continue
			}

			phone := ""
			if attribs, ok := listmonkCreateResp.Data.Attribs["phone"]; ok {
				if phoneStr, ok := attribs.(string); ok {
					phone = phoneStr
				}
			}
			subscriber := SubscriberEntry{
				UID:         listmonkCreateResp.Data.ID,
				Email:       listmonkCreateResp.Data.Email,
				Phone:       phone,
				BonusStatus: false,
			}
			id, err := logToPocketBase(pbURL, "subscribers", subscriber, "")
			if err != nil {
				logError("Subscriber save error:", err.Error())
				continue
			}
			subscriber.ID = id

			logEntry := LogEntry{
				ErrorMessage: "Retry processed successfully",
				Timestamp:    time.Now().Format(time.RFC3339),
				Response:     fmt.Sprintf("Serial: %s, Event: %s", entry.Serial, entry.Event),
			}
			if _, err := logToPocketBase(pbURL, "logs", logEntry, ""); err != nil {
				logError("Log save error:", err.Error())
			}

			// Удаляем запись из retry после успешной обработки
			if err := deleteRetryEntry(pbURL, entry.ID); err != nil {
				logError("Failed to delete retry entry after success:", err.Error())
			} else {
				log.Printf("Retry entry deleted for serial %s", entry.Serial)
			}
		}

		time.Sleep(60 * time.Second)
	}
}

func deleteRetryEntry(pbURL, id string) error {
	url := fmt.Sprintf("%s/api/collections/retry/records/%s", pbURL, id)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("POCKETBASE_ADMIN_TOKEN"))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Успешными считаются коды 200 и 204
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %d, %s", resp.StatusCode, string(body))
	}

	return nil
}

func checkSubscriptions(pbURL, apiKey string) {
	if pbURL == "" {
		logError("POCKETBASE_URL is not set", "Cannot proceed with subscriptions check")
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}

	for {
		resp, err := http.Get(fmt.Sprintf("%s/api/collections/subscribers/records", pbURL))
		if err != nil {
			logError("PocketBase fetch subscribers error:", err.Error())
			time.Sleep(5 * time.Minute)
			continue
		}
		defer resp.Body.Close()

		var subscribers struct {
			Items []SubscriberEntry `json:"items"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&subscribers); err != nil {
			logError("PocketBase decode subscribers error:", err.Error())
			time.Sleep(5 * time.Minute)
			continue
		}

		for _, sub := range subscribers.Items {
			if sub.BonusStatus { // Игнорируем записи, где бонус уже начислен
				continue
			}

			listmonkURL := os.Getenv("LISTMONK_API_URL")
			auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(os.Getenv("LISTMONK_USERNAME")+":"+os.Getenv("LISTMONK_API_KEY")))
			getURL := fmt.Sprintf("%s/%d", listmonkURL, sub.UID)
			req, err := http.NewRequest("GET", getURL, nil)
			if err != nil {
				logError("Listmonk GET request error:", err.Error())
				continue
			}
			req.Header.Set("Authorization", auth)

			resp, err = client.Do(req)
			if err != nil || resp.StatusCode != http.StatusOK {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					logError("Failed to read Listmonk response body:", err.Error())
					continue
				}
				logError("Listmonk GET API error:", fmt.Sprintf("Status: %d, Body: %s", resp.StatusCode, string(body)))
				continue
			}

			var listmonkResp ListmonkGetResponse
			if err := json.NewDecoder(resp.Body).Decode(&listmonkResp); err != nil {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					logError("Failed to read Listmonk response body:", err.Error())
					continue
				}
				logError("Listmonk GET decode error:", fmt.Sprintf("Error: %v, Response: %s", err, string(body)))
				continue
			}

			if len(listmonkResp.Data.Lists) > 0 && listmonkResp.Data.Lists[0].SubscriptionStatus == "confirmed" {
				mcrmURL := os.Getenv("MCRM_API_URL_BONUS")
				bonusSum, err := strconv.ParseFloat(os.Getenv("BONUS_SUM"), 64)
				if err != nil {
					logError("Invalid BONUS_SUM:", err.Error())
					continue
				}
				payload := map[string]interface{}{
					"number": sub.Phone,
					"sum":    bonusSum,
				}
				jsonPayload, _ := json.Marshal(payload)

				req, err = http.NewRequest("POST", mcrmURL, bytes.NewBuffer(jsonPayload))
				if err != nil {
					logError("MCRM bonus request error:", err.Error())
					continue
				}
				req.Header.Set("x-api-key", apiKey)
				log.Printf("MCRM bonus request - URL: %s, x-api-key set to: %s", mcrmURL, apiKey)
				req.Header.Set("Content-Type", "application/json")

				resp, err = client.Do(req)
				if err != nil || resp.StatusCode != http.StatusOK {
					body, _ := io.ReadAll(resp.Body)
					logError("MCRM bonus API error:", fmt.Sprintf("Status: %d, Body: %s", resp.StatusCode, string(body)))
					addToRetry(pbURL, sub.Phone, "bonus", fmt.Sprintf("Status: %d, Body: %s", resp.StatusCode, string(body)))
					continue
				}

				// Обновляем только bonus_status для существующей записи
				sub.BonusStatus = true
				if _, err := logToPocketBase(pbURL, "subscribers", sub, sub.ID); err != nil {
					logError("Failed to update subscriber bonus status:", err.Error())
					continue
				}

				logEntry := LogEntry{
					ErrorMessage: "Bonus added successfully",
					Timestamp:    time.Now().Format(time.RFC3339),
					Response:     fmt.Sprintf("Subscriber UID: %d, Phone: %s", sub.UID, sub.Phone),
				}
				if _, err := logToPocketBase(pbURL, "logs", logEntry, ""); err != nil {
					logError("Log save error:", err.Error())
				}
			}
		}

		time.Sleep(5 * time.Minute)
	}
}

func syncListmonkSubscribers(pbURL, listmonkURL, username, apiKey string, listID int) {
	client := &http.Client{Timeout: 10 * time.Second}
	log.Printf("Starting syncListmonkSubscribers with listID: %d", listID)

	// Проверка валидности listID
	if listID <= 0 {
		logError("Invalid listID:", fmt.Sprintf("listID=%d is not a valid identifier", listID))
		return
	}

	// Очистка listmonkURL от /subscribers и завершающих слешей
	cleanedURL := strings.TrimSuffix(listmonkURL, "/")
	cleanedURL = strings.TrimSuffix(cleanedURL, "/subscribers")
	log.Printf("Cleaned LISTMONK_API_URL: %s", cleanedURL)

	for {
		var totalProcessed int

		// Начало с первой страницы
		page := 1
		for {
			log.Printf("Fetching page %d for list_id=%d", page, listID)
			// Запрос списка подписчиков из Listmonk с указанием страницы
			auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+apiKey))
			reqURL := fmt.Sprintf("%s/subscribers?list_id=%d&page=%d", cleanedURL, listID, page)
			log.Printf("Request URL: %s", reqURL)
			req, err := http.NewRequest("GET", reqURL, nil)
			if err != nil {
				logError("Listmonk GET subscribers request error:", err.Error())
				break
			}
			req.Header.Set("Authorization", auth)

			resp, err := client.Do(req)
			log.Printf("Response received: Status=%d, Error=%v", resp.StatusCode, err)
			if err != nil {
				logError("Listmonk GET subscribers API request failed:", err.Error())
				time.Sleep(5 * time.Minute)
				break
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					logError("Failed to read Listmonk response body:", err.Error())
				} else {
					logError("Listmonk GET subscribers API error:", fmt.Sprintf("Status: %d, Body: %s", resp.StatusCode, string(body)))
				}
				time.Sleep(5 * time.Minute)
				break
			}

			var listmonkResp struct {
				Data struct {
					Results []struct {
						ID      int                    `json:"id"`
						Email   string                 `json:"email"`
						Attribs map[string]interface{} `json:"attribs"`
						Phone   string                 `json:"phone"`
						Status  string                 `json:"status"`
						Lists   []struct {
							ID int `json:"id"`
						} `json:"lists"`
					} `json:"results"`
					Total   int `json:"total"`
					PerPage int `json:"per_page"`
					Page    int `json:"page"`
				} `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&listmonkResp); err != nil {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					logError("Failed to read Listmonk response body:", err.Error())
				} else {
					logError("Listmonk GET subscribers decode error:", fmt.Sprintf("Error: %v, Response: %s", err, string(body)))
				}
				time.Sleep(5 * time.Minute)
				break
			}

			log.Printf("Received page %d: total=%d, per_page=%d, results=%d", page, listmonkResp.Data.Total, listmonkResp.Data.PerPage, len(listmonkResp.Data.Results))

			// Получаем текущих подписчиков из PocketBase
			pbResp, err := http.Get(fmt.Sprintf("%s/api/collections/subscribers/records", pbURL))
			if err != nil {
				logError("PocketBase fetch subscribers error:", err.Error())
				time.Sleep(5 * time.Minute)
				break
			}
			defer pbResp.Body.Close()

			var subscribers struct {
				Items []SubscriberEntry `json:"items"`
			}
			if err := json.NewDecoder(pbResp.Body).Decode(&subscribers); err != nil {
				logError("PocketBase decode subscribers error:", err.Error())
				time.Sleep(5 * time.Minute)
				break
			}

			// Мапа для быстрого поиска существующих подписчиков по email
			existingSubscribers := make(map[string]bool)
			for _, sub := range subscribers.Items {
				existingSubscribers[sub.Email] = true
			}

			// Обработка списка из Listmonk
			for _, subscriber := range listmonkResp.Data.Results {
				// Проверяем, подписан ли пользователь на указанный list_id (уже учтено в запросе)
				isInList := false
				for _, list := range subscriber.Lists {
					if list.ID == listID {
						isInList = true
						break
					}
				}
				if !isInList {
					continue
				}

				// Извлекаем phone из attribs, если есть
				phone := ""
				if attribs, ok := subscriber.Attribs["phone"]; ok {
					if phoneStr, ok := attribs.(string); ok {
						phone = phoneStr
					}
				}

				// Проверяем, есть ли пользователь в PocketBase
				if !existingSubscribers[subscriber.Email] {
					newSubscriber := SubscriberEntry{
						UID:         subscriber.ID,
						Email:       subscriber.Email,
						Phone:       phone,
						BonusStatus: false,
					}
					id, err := logToPocketBase(pbURL, "subscribers", newSubscriber, "")
					if err != nil {
						logError("Failed to save new subscriber from Listmonk:", err.Error())
						continue
					}
					newSubscriber.ID = id
					log.Printf("Added new subscriber from Listmonk: UID=%d, Email=%s, Phone=%s", subscriber.ID, subscriber.Email, phone)
					totalProcessed++
				}
			}

			// Проверяем, закончилась ли пагинация
			if len(listmonkResp.Data.Results) == 0 || page*listmonkResp.Data.PerPage >= listmonkResp.Data.Total {
				log.Printf("Processed %d subscribers from Listmonk (total: %d)", totalProcessed, listmonkResp.Data.Total)
				break
			}

			page++
			time.Sleep(1 * time.Second) // Пауза между страницами, чтобы не перегружать API
		}

		time.Sleep(6 * time.Hour) // Синхронизация раз в 10 минут
	}
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}

	e := echo.New()

	e.Use(middleware.BasicAuth(func(username, password string, c echo.Context) (bool, error) {
		validUsername := os.Getenv("WEBHOOK_USERNAME")
		validPassword := os.Getenv("WEBHOOK_PASSWORD")
		authHeader := c.Request().Header.Get("Authorization")
		log.Printf("Auth attempt - Username: %s, Password: %s, Expected Username: %s, Expected Password: %s, Authorization Header: %s",
			username, password, validUsername, validPassword, authHeader)
		if validUsername == "" || validPassword == "" {
			log.Printf("Environment variables WEBHOOK_USERNAME or WEBHOOK_PASSWORD are not set")
		}
		result := username == validUsername && password == validPassword
		log.Printf("Authentication result: %v", result)
		return result, nil
	}))

	e.POST("/webhook", processWebhook)

	go checkSubscriptions(os.Getenv("POCKETBASE_URL"), os.Getenv("MCRM_API_KEY"))
	go processRetry(os.Getenv("POCKETBASE_URL"), os.Getenv("MCRM_API_KEY"))

	// Запуск синхронизации подписчиков из Listmonk
	listIDStr := os.Getenv("LIST_ID")
	log.Printf("LIST_ID from env: %s", listIDStr)
	listID, err := strconv.Atoi(listIDStr)
	if err != nil {
		log.Fatalf("Invalid LIST_ID: %v (value: %s)", err, listIDStr)
	}
	log.Printf("Launching syncListmonkSubscribers with listID: %d", listID)
	go syncListmonkSubscribers(
		os.Getenv("POCKETBASE_URL"),
		os.Getenv("LISTMONK_API_URL"),
		os.Getenv("LISTMONK_USERNAME"),
		os.Getenv("LISTMONK_API_KEY"),
		listID,
	)

	log.Fatal(e.Start(":8080"))
}
