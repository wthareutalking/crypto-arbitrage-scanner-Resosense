package binance

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wthareutalking/crypto-arbitrage/internal/exchange" // Импорт интерфейса
	"go.uber.org/zap"
)

const (
	readTimeout = 15 * time.Second
)

type BinanceBookTicker struct {
	UpdateID int64  `json:"u"`
	Symbol   string `json:"s"` //пара
	BidPrice string `json:"b"` //лучшая цена покупки
	BidQty   string `json:"B"`
	AskPrice string `json:"a"` //лучшая цена продажи
	AskQty   string `json:"A"`
}

type Binance struct {
	//реализация интерфейса exchange
	conn   *websocket.Conn
	url    string
	out    chan exchange.PriceUpdate //канал для отправки данных наружу
	logger *zap.Logger
	stop   chan struct{} //канал для остановки
	mtx    sync.Mutex
}

func NewBinance(url string, logger *zap.Logger) *Binance {
	return &Binance{
		url:    url,
		out:    make(chan exchange.PriceUpdate, 100),
		logger: logger,
		stop:   make(chan struct{}),
	}
}

func (b *Binance) Connect(pairs []string) error {
	streams := make([]string, len(pairs))
	for i, p := range pairs {
		streams[i] = strings.ToLower(p) + "@bookTicker"
	}
	streamString := strings.Join(streams, "/")
	fullUrl := fmt.Sprintf("%s/stream?streams=%s", b.url, streamString)

	b.logger.Info("connecting to Binance WS", zap.String("url", fullUrl))

	c, _, err := websocket.DefaultDialer.Dial(fullUrl, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to binance ws: %w", err)
	}

	b.conn = c

	go b.readLoop()
	go b.keepAlive()

	return nil
}

func (b *Binance) StreamPrices() <-chan exchange.PriceUpdate {
	return b.out
}

func (b *Binance) Close() error {
	close(b.stop)
	return b.conn.Close()
}

func (b *Binance) keepAlive() {
	ticker := time.NewTicker(readTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.mtx.Lock()
			err := b.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second))
			b.mtx.Unlock()

			if err != nil {
				b.logger.Error("Ping error", zap.Error(err))
				return
			}
		case <-b.stop:
			return
		}
	}
}

func (b *Binance) readLoop() {
	defer close(b.out)
	b.conn.SetReadDeadline(time.Now().Add(readTimeout))
	b.conn.SetPongHandler(func(string) error {
		b.conn.SetReadDeadline(time.Now().Add(readTimeout))
		return nil
	})

	for {
		select {
		case <-b.stop:
			return
		default:
			_, message, err := b.conn.ReadMessage()
			if err != nil {
				b.logger.Error("Read error (connection lost)", zap.Error(err))
				return
			}

			b.conn.SetReadDeadline(time.Now().Add(readTimeout))

			var response struct {
				Data BinanceBookTicker `json:"data"`
			}
			if err := json.Unmarshal(message, &response); err != nil {
				b.logger.Error("JSON error", zap.Error(err))
				continue
			}
			bid, err1 := strconv.ParseFloat(response.Data.BidPrice, 64)
			ask, err2 := strconv.ParseFloat(response.Data.AskPrice, 64)
			if err1 != nil || err2 != nil {
				continue
			}

			update := exchange.PriceUpdate{
				Exchange:  "binance",
				Symbol:    response.Data.Symbol,
				Bid:       bid,
				Ask:       ask,
				Timestamp: time.Now().UnixMilli(),
			}

			select {
			case b.out <- update:
			default:
			}
		}
	}
}

// writeJSON безопасно отправляет данные в сокет, блокируя доступ для других горутин
func (b *Binance) writeJSON(v interface{}) error {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	if b.conn == nil {
		return fmt.Errorf("Connection is nil")
	}
	return b.conn.WriteJSON(v)
}

// SubscribeDynamic позволяет подписаться на новые пары без переподключения
func (b *Binance) SubscribeDynamic(pairs []string) error {
	if len(pairs) == 0 {
		return nil
	}

	b.logger.Info("Dynamically subscribing to new pairs", zap.Strings("pairs", pairs))

	var params []string
	for _, p := range pairs {
		symbol := strings.ToLower(p) + "@bookTicker"
		params = append(params, symbol)
	}

	subscribeMsg := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": params,
		"id":     time.Now().Unix(), //уникальный id запроса
	}

	return b.writeJSON(subscribeMsg)
}
