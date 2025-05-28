package models

type ItemResponse struct {
	Name           string      `json:"name"`
	Characteristic string      `json:"characteristic"`
	Price          float64     `json:"price"`
	Stock          []StockItem `json:"stock"`
}

type StockItem struct {
	Storage string  `json:"storage"`
	Series  string  `json:"series"`
	Count   float64 `json:"count"`
}
