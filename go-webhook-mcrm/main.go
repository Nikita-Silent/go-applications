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
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/net/context"
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
		Phone   string                 `json:"phone"`
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
	ID           string `json:"id"`
	Serial       string `json:"serial"`
	Event        string `json:"event"`
	RetryCount   int    `json:"retry_count"`
	ErrorMessage string `json:"error_message"`
	Timestamp    string `json:"timestamp"`
}

type SubscriberEntry struct {
	ID          string `json:"id"`
	UID         int    `json:"uid"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	BonusStatus bool   `json:"bonus_status"`
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

	log.Printf("Updated retry entry in PocketBase: ID=%s, Serial=%s, RetryCount=%d", entry.ID, entry.Serial, entry.RetryCount)
	return nil
}

func processWebhook(c echo.Context) error {
	bodyBytes, err := io.ReadAll(c.Request().Body)
	if err != nil {
		logError("Failed to read webhook body:", err.Error())
		return c.NoContent(http.StatusInternalServerError)
	}
	c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	serial := c.FormValue("serial")
	event := c.FormValue("event")
	cleanedSerial := cleanSerial(serial)

	if serial == "" || event == "" {
		logError("Missing serial or event in webhook", fmt.Sprintf("Body: %s", string(bodyBytes)))
		return c.NoContent(http.StatusBadRequest)
	}

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
		logError("MCRM API error:", fmt.Sprintf("Status: %d, Response: %s", resp.StatusCode, string(mcrmBody)))
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
		logError("Listmonk API error:", fmt.Sprintf("Status: %d, Response: %s", resp.StatusCode, string(listmonkBody)))
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
		BonusStatus: false,
	}
	id, err := logToPocketBase(os.Getenv("POCKETBASE_URL"), "subscribers", subscriber, "")
	if err != nil {
		logError("Subscriber save error:", err.Error())
		return c.NoContent(http.StatusInternalServerError)
	}
	subscriber.ID = id
	log.Printf("Saved subscriber to PocketBase: UID=%d, Email=%s, Phone=%s", subscriber.UID, subscriber.Email, subscriber.Phone)

	logEntry := LogEntry{
		ErrorMessage: "Webhook processed successfully",
		Timestamp:    time.Now().Format(time.RFC3339),
		Response:     fmt.Sprintf("Subscriber UID: %d", listmonkResp.Data.ID),
	}
	if _, err := logToPocketBase(os.Getenv("POCKETBASE_URL"), "logs", logEntry, ""); err != nil {
		logError("Log save error:", err.Error())
	} else {
		log.Printf("Logged webhook success: Subscriber UID=%d", listmonkResp.Data.ID)
	}

	go checkSubscriptions(os.Getenv("POCKETBASE_URL"), os.Getenv("MCRM_API_KEY"))

	return c.NoContent(http.StatusOK)
}

func logError(message, details string, uid ...int) {
	var uidStr string
	if len(uid) > 0 {
		uidStr = fmt.Sprintf(" UID: %d", uid[0])
	}
	logEntry := LogEntry{
		ErrorMessage: message + uidStr,
		Timestamp:    time.Now().Format(time.RFC3339),
		Response:     details,
	}
	if _, err := logToPocketBase(os.Getenv("POCKETBASE_URL"), "logs", logEntry, ""); err != nil {
		log.Printf("Failed to log error to PocketBase: %v", err)
	} else {
		log.Printf("Logged error to PocketBase: %s%s, Details: %s", message, uidStr, details)
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
	retryEntry.ID = id
	log.Printf("Added retry entry to PocketBase: Serial=%s, Event=%s, ID=%s", serial, event, id)
	return nil
}

func processRetry(pbURL, apiKey string) {
	client := &http.Client{Timeout: 10 * time.Second}
	maxRetries := 5

	for {
		resp, err := client.Get(fmt.Sprintf("%s/api/collections/retry/records", pbURL))
		if err != nil {
			logError("Failed to fetch retry entries:", err.Error())
			time.Sleep(30 * time.Second)
			continue
		}
		defer resp.Body.Close()

		var retryData struct {
			Items []RetryEntry `json:"items"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&retryData); err != nil {
			logError("Failed to decode retry entries:", err.Error())
			time.Sleep(30 * time.Second)
			continue
		}

		for _, entry := range retryData.Items {
			if entry.RetryCount >= maxRetries {
				logError("Max retries reached for serial:", entry.Serial)
				if err := deleteRetryEntry(pbURL, entry.ID); err != nil {
					logError("Failed to delete retry entry:", err.Error())
				} else {
					log.Printf("Deleted retry entry from PocketBase: ID=%s, Serial=%s", entry.ID, entry.Serial)
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

			resp, err := client.Do(req)
			if err != nil || resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				entry.RetryCount++
				logError("Retry failed for serial:", fmt.Sprintf("Serial: %s, Attempt: %d, Status: %d, Body: %s", entry.Serial, entry.RetryCount, resp.StatusCode, string(body)))
				if err := updateRetryEntry(pbURL, entry); err != nil {
					logError("Failed to update retry entry:", err.Error())
				}
				time.Sleep(1 * time.Minute)
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			var mcrmData MCRMResponse
			if err := json.NewDecoder(bytes.NewReader(body)).Decode(&mcrmData); err != nil {
				logError("MCRM decode error:", fmt.Sprintf("Error: %v, Response: %s", err, string(body)))
				entry.RetryCount++
				if err := updateRetryEntry(pbURL, entry); err != nil {
					logError("Failed to update retry entry:", err.Error())
				}
				continue
			}

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
			if err != nil || (listmonkResp.StatusCode != http.StatusOK && listmonkResp.StatusCode != http.StatusCreated) {
				listmonkBody, _ := io.ReadAll(listmonkResp.Body)
				logError("Listmonk API error:", fmt.Sprintf("Status: %d, Response: %s", listmonkResp.StatusCode, string(listmonkBody)))
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
			log.Printf("Saved subscriber to PocketBase: UID=%d, Email=%s, Phone=%s", subscriber.UID, subscriber.Email, subscriber.Phone)

			logEntry := LogEntry{
				ErrorMessage: "Retry processed successfully",
				Timestamp:    time.Now().Format(time.RFC3339),
				Response:     fmt.Sprintf("Serial: %s, Event: %s", entry.Serial, entry.Event),
			}
			if _, err := logToPocketBase(pbURL, "logs", logEntry, ""); err != nil {
				logError("Log save error:", err.Error())
			} else {
				log.Printf("Logged retry success: Serial=%s, Event=%s", entry.Serial, entry.Event)
			}

			if err := deleteRetryEntry(pbURL, entry.ID); err != nil {
				logError("Failed to delete retry entry:", err.Error())
			} else {
				log.Printf("Deleted retry entry from PocketBase: ID=%s, Serial=%s", entry.ID, entry.Serial)
			}
		}

		time.Sleep(30 * time.Second)
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %d, %s", resp.StatusCode, string(body))
	}

	log.Printf("Deleted retry entry from PocketBase: ID=%s", id)
	return nil
}

func checkSubscriptions(pbURL, apiKey string) {
	if pbURL == "" {
		logError("POCKETBASE_URL is not set", "Cannot proceed with subscriptions check")
		return
	}
	client := &http.Client{Timeout: 30 * time.Second}

	const workerCount = 10
	var wg sync.WaitGroup
	taskChan := make(chan SubscriberEntry, 2000)
	processedUIDs := make(map[int]bool)

	listIDStr := os.Getenv("LIST_ID")
	listID, err := strconv.Atoi(listIDStr)
	if err != nil {
		logError("Invalid LIST_ID:", fmt.Sprintf("value: %s, error: %v", listIDStr, err))
		return
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker(client, pbURL, apiKey, taskChan, &wg, listID)
	}

	for {
		var allSubscribers []SubscriberEntry
		page := 1
		perPage := 30

		for {
			resp, err := client.Get(fmt.Sprintf("%s/api/collections/subscribers/records?page=%d&perPage=%d", pbURL, page, perPage))
			if err != nil {
				logError("PocketBase fetch subscribers error:", err.Error())
				break
			}
			defer resp.Body.Close()

			var subscribers struct {
				Items      []SubscriberEntry `json:"items"`
				Total      int               `json:"totalItems"`
				Page       int               `json:"page"`
				TotalPages int               `json:"totalPages"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&subscribers); err != nil {
				logError("PocketBase decode subscribers error:", err.Error())
				break
			}

			allSubscribers = append(allSubscribers, subscribers.Items...)
			if page >= subscribers.TotalPages {
				break
			}
			page++
		}

		// Инициализируем прогресс-бар
		totalSubscribers := len(allSubscribers)
		bar := progressbar.Default(int64(totalSubscribers), "Processing subscribers")
		defer bar.Close()

		processedUIDs = make(map[int]bool)
		for _, sub := range allSubscribers {
			if !sub.BonusStatus && !processedUIDs[sub.UID] {
				taskChan <- sub
				processedUIDs[sub.UID] = true
				bar.Add(1) // Обновляем прогресс-бар для каждого отправленного подписчика
			}
		}

		// Завершаем прогресс-бар перед синхронизацией
		bar.Finish()

		syncListmonkSubscribers(pbURL, os.Getenv("LISTMONK_API_URL"), os.Getenv("LISTMONK_USERNAME"), os.Getenv("LISTMONK_API_KEY"), listID)

		time.Sleep(15 * time.Second)
	}
}

func worker(client *http.Client, pbURL, apiKey string, taskChan <-chan SubscriberEntry, wg *sync.WaitGroup, expectedListID int) {
	defer wg.Done()

	for sub := range taskChan {
		listmonkURL := os.Getenv("LISTMONK_API_URL")
		auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(os.Getenv("LISTMONK_USERNAME")+":"+os.Getenv("LISTMONK_API_KEY")))
		getURL := fmt.Sprintf("%s/%d", listmonkURL, sub.UID)
		req, err := http.NewRequest("GET", getURL, nil)
		if err != nil {
			logError("Listmonk GET request error:", err.Error(), sub.UID)
			addToRetry(pbURL, sub.Phone, "check_subscription", err.Error())
			continue
		}
		req.Header.Set("Authorization", auth)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		req = req.WithContext(ctx)
		resp, err := client.Do(req)
		if err != nil {
			body := ""
			if resp != nil {
				bodyBytes, _ := io.ReadAll(resp.Body)
				body = string(bodyBytes)
				resp.Body.Close()
			}
			logError("Listmonk GET API error:", fmt.Sprintf("UID: %d, Status: %d, Error: %v, Body: %s", sub.UID, resp.StatusCode, err, body), sub.UID)
			addToRetry(pbURL, sub.Phone, "check_subscription", fmt.Sprintf("Error: %v, Status: %d, Body: %s", err, resp.StatusCode, body))
			cancel()
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			logError("Listmonk GET API error:", fmt.Sprintf("UID: %d, Status: %d, Body: %s", sub.UID, resp.StatusCode, string(bodyBytes)), sub.UID)
			addToRetry(pbURL, sub.Phone, "check_subscription", fmt.Sprintf("Status: %d, Body: %s", resp.StatusCode, string(bodyBytes)))
			cancel()
			continue
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			logError("Failed to read Listmonk response body:", err.Error(), sub.UID)
			addToRetry(pbURL, sub.Phone, "check_subscription", err.Error())
			cancel()
			continue
		}

		var listmonkResp ListmonkGetResponse
		if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&listmonkResp); err != nil {
			logError("Listmonk GET decode error:", fmt.Sprintf("UID: %d, Error: %v, Response: %s", sub.UID, err, string(bodyBytes)), sub.UID)
			addToRetry(pbURL, sub.Phone, "check_subscription", fmt.Sprintf("Error: %v, Response: %s", err, string(bodyBytes)))
			continue
		}

		isConfirmedForExpectedList := false
		for _, list := range listmonkResp.Data.Lists {
			if list.ID == expectedListID && list.SubscriptionStatus == "confirmed" {
				isConfirmedForExpectedList = true
				break
			}
		}

		if isConfirmedForExpectedList {
			mcrmURL := os.Getenv("MCRM_API_URL_BONUS")
			bonusSum, err := strconv.ParseFloat(os.Getenv("BONUS_SUM"), 64)
			if err != nil {
				logError("Invalid BONUS_SUM:", err.Error(), sub.UID)
				continue
			}
			payload := map[string]interface{}{
				"number": sub.Phone,
				"sum":    bonusSum,
			}
			jsonPayload, _ := json.Marshal(payload)

			req, err = http.NewRequest("POST", mcrmURL, bytes.NewBuffer(jsonPayload))
			if err != nil {
				logError("MCRM bonus request error:", err.Error(), sub.UID)
				continue
			}
			req.Header.Set("x-api-key", apiKey)
			req.Header.Set("Content-Type", "application/json")

			ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
			req = req.WithContext(ctx)
			resp, err = client.Do(req)
			if err != nil || resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				logError("MCRM bonus API error:", fmt.Sprintf("UID: %d, Status: %d, Body: %s", sub.UID, resp.StatusCode, string(body)), sub.UID)
				addToRetry(pbURL, sub.Phone, "bonus", fmt.Sprintf("Status: %d, Body: %s", resp.StatusCode, string(body)))
				cancel()
				continue
			}
			cancel()

			sub.BonusStatus = true
			if _, err := logToPocketBase(pbURL, "subscribers", sub, sub.ID); err != nil {
				logError("Failed to update subscriber bonus status:", err.Error(), sub.UID)
				continue
			}
			log.Printf("Updated subscriber in PocketBase: UID=%d, BonusStatus=true", sub.UID)
		}
	}
}

func syncListmonkSubscribers(pbURL, listmonkURL, username, apiKey string, listID int) {
	client := &http.Client{Timeout: 30 * time.Second}

	if listID <= 0 {
		logError("Invalid listID:", fmt.Sprintf("listID=%d is not a valid identifier", listID))
		return
	}

	cleanedURL := strings.TrimSuffix(listmonkURL, "/")
	cleanedURL = strings.TrimSuffix(cleanedURL, "/subscribers")

	const perPage = 1000

	page := 1
	for {
		auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+apiKey))
		reqURL := fmt.Sprintf("%s/subscribers?list_id=%d&page=%d&per_page=%d", cleanedURL, listID, page, perPage)
		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			logError("Listmonk GET subscribers request error:", err.Error())
			break
		}
		req.Header.Set("Authorization", auth)

		resp, err := client.Do(req)
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
			body, _ := io.ReadAll(resp.Body)
			logError("Listmonk GET subscribers decode error:", fmt.Sprintf("Error: %v, Response: %s", err, string(body)))
			time.Sleep(5 * time.Minute)
			break
		}

		var allSubscribers []SubscriberEntry
		pagePB := 1
		for {
			pbResp, err := client.Get(fmt.Sprintf("%s/api/collections/subscribers/records?page=%d&perPage=100", pbURL, pagePB))
			if err != nil {
				logError("Ошибка загрузки из PocketBase:", err.Error())
				break
			}
			defer pbResp.Body.Close()

			var subscribersPage struct {
				Items []SubscriberEntry `json:"items"`
				Total int               `json:"totalPages"`
			}
			if err := json.NewDecoder(pbResp.Body).Decode(&subscribersPage); err != nil {
				logError("Ошибка декодирования данных из PocketBase:", err.Error())
				break
			}

			allSubscribers = append(allSubscribers, subscribersPage.Items...)
			if pagePB >= subscribersPage.Total {
				break
			}
			pagePB++
		}

		existingSubscribers := make(map[int]SubscriberEntry)
		for _, sub := range allSubscribers {
			existingSubscribers[sub.UID] = sub
		}

		for _, subscriber := range listmonkResp.Data.Results {
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

			phone := ""
			if attribs, ok := subscriber.Attribs["phone"]; ok {
				if phoneStr, ok := attribs.(string); ok {
					phone = phoneStr
				}
			}

			newSubscriber := SubscriberEntry{
				UID:         subscriber.ID,
				Email:       subscriber.Email,
				Phone:       phone,
				BonusStatus: false,
			}

			if existingSub, exists := existingSubscribers[subscriber.ID]; exists {
				if existingSub.Email != subscriber.Email || existingSub.Phone != phone {
					existingSub.Email = subscriber.Email
					existingSub.Phone = phone
					if _, err := logToPocketBase(pbURL, "subscribers", existingSub, existingSub.ID); err != nil {
						logError("Ошибка обновления подписчика:", err.Error())
						continue
					}
					log.Printf("Updated subscriber in PocketBase: UID=%d, Email=%s, Phone=%s", subscriber.ID, subscriber.Email, phone)
				}
			} else {
				id, err := logToPocketBase(pbURL, "subscribers", newSubscriber, "")
				if err != nil {
					logError("Ошибка сохранения нового подписчика:", err.Error())
					continue
				}
				newSubscriber.ID = id
				log.Printf("Saved new subscriber to PocketBase: UID=%d, Email=%s, Phone=%s", subscriber.ID, subscriber.Email, phone)
			}
		}

		if len(listmonkResp.Data.Results) == 0 || page*listmonkResp.Data.PerPage >= listmonkResp.Data.Total {
			break
		}

		page++
		time.Sleep(1 * time.Second)
	}

	time.Sleep(1 * time.Hour)
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
		return username == validUsername && password == validPassword, nil
	}))

	e.POST("/webhook", processWebhook)

	go checkSubscriptions(os.Getenv("POCKETBASE_URL"), os.Getenv("MCRM_API_KEY"))
	go processRetry(os.Getenv("POCKETBASE_URL"), os.Getenv("MCRM_API_KEY"))

	log.Fatal(e.Start(":8080"))
}
