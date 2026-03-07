package models

// ArbitrageSignal - это структура сообщения, которое будет летать через Kafka

type ArbitrageSignal struct {
	Pair      string  `json:"pair"`
	Direction string  `json:"direction"`
	Spread    float64 `json:"spread"`
	BuyPrice  float64 `json:"buy_price"`
	SellPrice float64 `json:"sell_price"`
	Timestamp int64   `json:"timestamp"`
}
