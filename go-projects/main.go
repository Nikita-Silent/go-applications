package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/joho/godotenv"
)

type ItemResponse struct {
	Name           string  `json:"name"`
	Characteristic string  `json:"characteristic"`
	Price          float64 `json:"price"`
	Stock          []struct {
		Storage string  `json:"storage"`
		Series  string  `json:"series"`
		Count   float64 `json:"count"`
	} `json:"stock"`
}

const htmlTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>Сканер штрихкодов</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        video { width: 100%; max-width: 500px; background: #000; }
        table { border-collapse: collapse; width: 100%; max-width: 500px; margin-top: 20px; }
        th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
        th { background-color: #f2f2f2; }
        #result { margin-top: 20px; color: red; }
        #fileInput { margin-top: 10px; }
        #resultContainer { margin-top: 20px; }
        button { margin-top: 10px; padding: 8px 16px; }
    </style>
</head>
<body>
    <h2>Сканировать штрихкод</h2>
    <video id="video" autoplay playsinline muted></video>
    <div id="result">Инициализация сканера...</div>
    <input type="file" id="fileInput" accept="image/*" style="display: none;">
    <button onclick="document.getElementById('fileInput').click();">Загрузить изображение штрихкода</button>
    <div id="resultContainer"></div>

    <script src="https://unpkg.com/@zxing/library@0.20.0/umd/index.min.js"></script>
    <script>
        let codeReader = null;

        async function startScanner() {
            const resultDiv = document.getElementById('result');
            const video = document.getElementById('video');
            codeReader = new ZXing.BrowserMultiFormatReader();

            try {
                console.log('Перечисление устройств...');
                const devices = await navigator.mediaDevices.enumerateDevices();
                const videoDevices = devices.filter(device => device.kind === 'videoinput');
                console.log('Видеоустройства:', videoDevices);
                const rearCamera = videoDevices.find(device => device.label.toLowerCase().includes('back')) || videoDevices[0];

                if (!rearCamera) {
                    resultDiv.innerHTML = 'Камера не найдена';
                    console.error('Камера не найдена');
                    return;
                }

                console.log('Запуск видеопотока с устройством:', rearCamera.label);
                codeReader.decodeFromVideoDevice(
                    rearCamera.deviceId,
                    'video',
                    (result, err) => {
                        if (result) {
                            resultDiv.innerHTML = 'Обнаружен штрихкод: ' + result.text;
                            console.log('Штрихкод обнаружен:', result.text);
                            codeReader.reset();
                            fetchData(result.text);
                        }
                        if (err && !(err instanceof ZXing.NotFoundException)) {
                            resultDiv.innerHTML = 'Ошибка сканера: ' + err;
                            console.error('Ошибка сканера:', err);
                        }
                    }
                );
                resultDiv.innerHTML = 'Сканер инициализирован, отсканируйте штрихкод';
                console.log('Сканер инициализирован');
            } catch (err) {
                resultDiv.innerHTML = 'Ошибка инициализации сканера: ' + err;
                console.error('Ошибка инициализации:', err);
            }
        }

        // Обработчик загрузки изображений
        document.getElementById('fileInput').addEventListener('change', async (event) => {
            const file = event.target.files[0];
            if (!file) return;
            const resultDiv = document.getElementById('result');
            const codeReader = new ZXing.BrowserMultiFormatReader();

            try {
                const img = new Image();
                img.src = URL.createObjectURL(file);
                await img.decode();
                const canvas = document.createElement('canvas');
                canvas.width = img.width;
                canvas.height = img.height;
                const ctx = canvas.getContext('2d');
                ctx.drawImage(img, 0, 0);
                const imageData = ctx.getImageData(0, 0, canvas.width, canvas.height);
                const result = await codeReader.decodeFromImageData(imageData);
                resultDiv.innerHTML = 'Обнаружен штрихкод из изображения: ' + result.text;
                console.log('Штрихкод из изображения:', result.text);
                fetchData(result.text);
            } catch (err) {
                resultDiv.innerHTML = 'Ошибка сканирования изображения: ' + err;
                console.error('Ошибка сканирования изображения:', err);
            }
        });

        // Функция для получения данных
        function fetchData(barcode) {
            const resultContainer = document.getElementById('resultContainer');
            fetch('/scan', {
                method: 'POST',
                headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                body: 'barcode=' + encodeURIComponent(barcode)
            })
            .then(response => response.text())
            .then(html => {
                resultContainer.innerHTML = html;
                document.getElementById('video').style.display = 'none';
                document.getElementById('fileInput').style.display = 'none';
                document.querySelector('button[onclick*="fileInput"]').style.display = 'none';
            })
            .catch(err => {
                document.getElementById('result').innerHTML = 'Ошибка получения данных: ' + err;
                console.error('Ошибка fetch:', err);
            });
        }

        // Обработчик кнопки повторного сканирования
        function resetScanner() {
            const resultContainer = document.getElementById('resultContainer');
            resultContainer.innerHTML = '';
            document.getElementById('video').style.display = 'block';
            document.getElementById('fileInput').style.display = 'none';
            document.querySelector('button[onclick*="fileInput"]').style.display = 'block';
            document.getElementById('result').innerHTML = 'Инициализация сканера...';
            startScanner();
        }

        if (location.protocol !== 'https:' && location.hostname !== 'localhost') {
            document.getElementById('result').innerHTML = 'Для доступа к камере требуется HTTPS. Используйте HTTPS или localhost.';
        } else {
            startScanner();
        }
    </script>
</body>
</html>
`

const resultTemplate = `
    {{if .}}
    <table>
        <caption>Остатки по товару</caption>
        <tr><td colspan="3">{{.Name}} / {{.Characteristic}}</td></tr>
        <tr><td colspan="3">Цена товара: {{printf "%.2f" .Price}} ₽</td></tr>
        <tr><th>Магазин</th><th>Серия</th><th>Остаток</th></tr>
        {{range .Stock}}
        <tr><td>{{.Storage}}</td><td>{{.Series}}</td><td>{{printf "%.0f" .Count}}</td></tr>
        {{end}}
    </table>
    <button onclick="resetScanner()">Сканировать ещё раз</button>
    {{end}}
`

func main() {
	// Загрузка переменных из .env
	if err := godotenv.Load(); err != nil {
		fmt.Printf("Ошибка загрузки файла .env: %v\n", err)
		os.Exit(1)
	}

	// Получение абсолютных путей к сертификатам
	dir, err := os.Getwd()
	if err != nil {
		fmt.Printf("Ошибка получения текущей директории: %v\n", err)
		os.Exit(1)
	}
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	// Проверка наличия сертификатов
	if _, err := os.Stat(certPath); err != nil {
		fmt.Printf("Файл сертификата не найден: %s\n", certPath)
		os.Exit(1)
	}
	if _, err := os.Stat(keyPath); err != nil {
		fmt.Printf("Файл ключа не найден: %s\n", keyPath)
		os.Exit(1)
	}

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/scan", handleScan)
	fmt.Printf("Сервер запускается на :8080 с HTTPS, сертификат: %s, ключ: %s\n", certPath, keyPath)
	if err := http.ListenAndServeTLS(":8080", certPath, keyPath, nil); err != nil {
		fmt.Printf("Ошибка запуска сервера: %v\n", err)
		os.Exit(1)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.New("index").Parse(htmlTemplate)
	if err != nil {
		http.Error(w, "Ошибка шаблона", http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, nil); err != nil {
		http.Error(w, "Ошибка выполнения шаблона", http.StatusInternalServerError)
		return
	}
}

func handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не разрешен", http.StatusMethodNotAllowed)
		return
	}

	barcode := r.FormValue("barcode")
	if barcode == "" {
		http.Error(w, "Штрихкод не предоставлен", http.StatusBadRequest)
		return
	}

	fmt.Printf("Получен штрихкод: %s\n", barcode) // Отладочный лог

	// Получение конфигурации из .env
	apiURL := os.Getenv("API_URL")
	token := os.Getenv("API_TOKEN")
	auth := os.Getenv("API_AUTH")
	if apiURL == "" || token == "" || auth == "" {
		fmt.Println("Ошибка: Не указаны API_URL, API_TOKEN или API_AUTH в .env")
		http.Error(w, "Ошибка конфигурации сервера", http.StatusInternalServerError)
		return
	}

	// Выполнение команды curl
	cmd := exec.Command("curl",
		"-H", fmt.Sprintf("Token: %s", token),
		"-H", fmt.Sprintf("Authorization: %s", auth),
		fmt.Sprintf("%s?barcode=%s", apiURL, barcode))

	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		fmt.Printf("Ошибка cURL: %v\n", err) // Отладочный лог
		http.Error(w, fmt.Sprintf("Ошибка cURL: %v", err), http.StatusInternalServerError)
		return
	}

	fmt.Printf("Ответ API: %s\n", out.String()) // Отладочный лог

	var item ItemResponse
	if err := json.Unmarshal(out.Bytes(), &item); err != nil {
		fmt.Printf("Ошибка разбора JSON: %v\n", err) // Отладочный лог
		http.Error(w, fmt.Sprintf("Ошибка разбора JSON: %v", err), http.StatusInternalServerError)
		return
	}

	tmpl, err := template.New("result").Parse(resultTemplate)
	if err != nil {
		http.Error(w, "Ошибка шаблона", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.Execute(w, item); err != nil {
		http.Error(w, "Ошибка выполнения шаблона", http.StatusInternalServerError)
		return
	}
}
