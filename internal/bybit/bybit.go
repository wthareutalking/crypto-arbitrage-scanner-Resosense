package bybit

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wthareutalking/crypto-arbitrage/internal/exchange"
	"go.uber.org/zap"
)

const (
	pingInterval = 20 * time.Second
	readTimeout  = 30 * time.Second
)

type BybitResponse struct {
	Topic string `json:"topic"`
	Data  struct {
		Bids      [][]string `json:"b"`
		Asks      [][]string `json:"a"`
		Timestamp int64      `json:"ts"`
	} `json:"data"`
}

type Bybit struct {
	conn   *websocket.Conn
	url    string
	out    chan exchange.PriceUpdate
	logger *zap.Logger
	stop   chan struct{}

	mtx sync.Mutex
}

func NewBybit(url string, logger *zap.Logger) *Bybit {
	return &Bybit{
		url:    url,
		out:    make(chan exchange.PriceUpdate, 100),
		logger: logger,
		stop:   make(chan struct{}),
	}
}

func (b *Bybit) Connect(pairs []string) error {
	b.logger.Info("Connecting to Bybit WS", zap.String("url", b.url))

	c, _, err := websocket.DefaultDialer.Dial(b.url, nil)
	if err != nil {
		return fmt.Errorf("Failed to connect to Bybit: %w", err)
	}
	b.conn = c

	// отправка подписки (получение цен)
	args := make([]string, len(pairs))

	for i, p := range pairs {
		args[i] = fmt.Sprintf("orderbook.1.%s", strings.ToUpper(p))
	}

	//шлем json
	subscribeMsg := map[string]interface{}{
		"op":   "subscribe",
		"args": args,
	}

	if err := b.writeJSON(subscribeMsg); err != nil {
		return fmt.Errorf("failed to subscribe to pairs: %w", err)
	}
	go b.readLoop()
	go b.keepAlive()

	return nil
}

func (b *Bybit) StreamPrices() <-chan exchange.PriceUpdate {
	return b.out
}

func (b *Bybit) Close() error {
	close(b.stop)
	return b.conn.Close()
}

func (b *Bybit) readLoop() {
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
				b.logger.Error("Bybit read error", zap.Error(err))
				return
			}
			b.conn.SetReadDeadline(time.Now().Add(readTimeout))

			var resp BybitResponse
			if err := json.Unmarshal(message, &resp); err != nil {
				continue
			}
			if len(resp.Data.Bids) == 0 || len(resp.Data.Asks) == 0 {
				continue
			}

			//парсинг цены
			parts := strings.Split(resp.Topic, ".")
			if len(parts) < 3 {
				continue
			}
			symbol := parts[2]

			bidPriceStr := resp.Data.Bids[0][0]
			bid, err := strconv.ParseFloat(bidPriceStr, 64)
			if err != nil {
				continue
			}

			askPriceStr := resp.Data.Asks[0][0]
			ask, err := strconv.ParseFloat(askPriceStr, 64)
			if err != nil {
				continue
			}

			update := exchange.PriceUpdate{
				Exchange:  "bybit",
				Symbol:    symbol,
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

func (b *Bybit) keepAlive() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ping := map[string]string{"op": "ping"}
			if err := b.conn.WriteJSON(ping); err != nil {
				b.logger.Error("Bybit KeepAlive error", zap.Error(err))
			}
		case <-b.stop:
			return
		}
	}
}

// writeJSON безопасно отправляет данные в сокет, блокируя доступ для других горутин
func (b *Bybit) writeJSON(v interface{}) error {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	if b.conn == nil {
		return fmt.Errorf("Connection is nil")
	}
	return b.conn.WriteJSON(v)
}

// SubscribeDynamic позволяет подписаться на новые пары без переподключения
func (b *Bybit) SubscribeDynamic(pairs []string) error {
	if len(pairs) == 0 {
		return nil
	}

	b.logger.Info("Dunamically subscribing to new pairs", zap.Strings("pairs", pairs))
	// Формируем аргументы в формате Bybit: "orderbook.1.BTCUSDT"
	args := make([]string, len(pairs))
	for i, p := range pairs {
		args[i] = fmt.Sprintf("orderbook.1.%s", strings.ToUpper(p))
	}

	subscribeMsg := map[string]interface{}{
		"op":   "subscribe",
		"args": args,
	}
	return b.writeJSON(subscribeMsg)
}
