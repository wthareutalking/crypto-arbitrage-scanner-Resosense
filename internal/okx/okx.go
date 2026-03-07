package okx

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

// Структуры для парсинга JSON ответа
type OkxResponse struct {
	Arg  OkxArg    `json:"arg"`
	Data []OkxData `json:"data"`
}

type OkxArg struct {
	InstId string `json:"instId"`
}

type OkxData struct {
	AskPx string `json:"askPx"` //цена продажи
	BidPx string `json:"bidPx"` // цена покупки
}

type Okx struct {
	conn   *websocket.Conn
	url    string
	out    chan exchange.PriceUpdate
	logger *zap.Logger
	stop   chan struct{}

	mtx sync.Mutex
}

func NewOkx(url string, logger *zap.Logger) *Okx {
	return &Okx{
		url:    url,
		out:    make(chan exchange.PriceUpdate, 100),
		logger: logger,
		stop:   make(chan struct{}),
	}
}

func (o *Okx) Connect(pairs []string) error {
	o.logger.Info("Connecting to OKX WS", zap.String("url", o.url))
	c, _, err := websocket.DefaultDialer.Dial(o.url, nil)
	if err != nil {
		return fmt.Errorf("Failed to connect to OKX: %w", err)
	}
	o.conn = c
	//подготовка подписки
	var args []map[string]string
	for _, p := range pairs {
		//нормализация:вставляем дефис
		okxPair := strings.Replace(p, "USDT", "-USDT", 1)
		args = append(args, map[string]string{
			"channel": "tickers",
			"instId":  okxPair,
		})
	}

	subscribeMsg := map[string]interface{}{
		"op":   "subscribe",
		"args": args,
	}

	if err := o.writeJSON(subscribeMsg); err != nil {
		return fmt.Errorf("Failed to subscribe OKX: %w", err)
	}

	go o.readLoop()
	go o.keepAlive()
	return nil
}

func (o *Okx) StreamPrices() <-chan exchange.PriceUpdate {
	return o.out
}

func (o *Okx) Close() error {
	close(o.stop)
	if o.conn != nil {
		return o.conn.Close()
	}
	return nil
}

func (o *Okx) keepAlive() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// OKX просит отправлять текстовое сообщение "ping"
			if err := o.writeMessage(websocket.TextMessage, []byte("ping")); err != nil {
				return
			}
		case <-o.stop:
			return
		}
	}
}

func (o *Okx) readLoop() {
	defer close(o.out)
	o.conn.SetReadDeadline(time.Now().Add(readTimeout))
	// Обработка текстового "pong" от сервера
	o.conn.SetPongHandler(func(string) error {
		o.conn.SetReadDeadline(time.Now().Add(readTimeout))
		return nil
	})

	for {
		select {
		case <-o.stop:
			return
		default:
			_, message, err := o.conn.ReadMessage()
			if err != nil {
				o.logger.Error("OKX read error", zap.Error(err))
				return
			}
			o.conn.SetReadDeadline(time.Now().Add(readTimeout))
			// Игнорируем ответы на пинги и события подписки
			if string(message) == "pong" || strings.Contains(string(message), "event") {
				continue
			}

			var resp OkxResponse
			if err := json.Unmarshal(message, &resp); err != nil {
				continue
			}

			if len(resp.Data) == 0 {
				continue
			}
			// Парсинг цен
			bid, err1 := strconv.ParseFloat(resp.Data[0].BidPx, 64)
			ask, err2 := strconv.ParseFloat(resp.Data[0].AskPx, 64)
			if err1 != nil || err2 != nil {
				continue
			}
			// Нормализация имени пары обратно к стандарту системы: BTC-USDT -> BTCUSDT
			normalizedSymbol := strings.Replace(resp.Arg.InstId, "-", "", 1)

			update := exchange.PriceUpdate{
				Exchange:  "okx",
				Symbol:    normalizedSymbol,
				Bid:       bid,
				Ask:       ask,
				Timestamp: time.Now().UnixMilli(),
			}
			select {
			case o.out <- update:
			default:
			}
		}
	}
}

// writeJSON безопасно отправляет данные в сокет, блокируя доступ для других горутин
func (o *Okx) writeJSON(v interface{}) error {
	o.mtx.Lock()
	defer o.mtx.Unlock()

	if o.conn == nil {
		return fmt.Errorf("Connection is nil")
	}
	return o.conn.WriteJSON(v)
}

// writeMessage безопасно отправляет сырые текстовые/бинарные сообщения
func (o *Okx) writeMessage(messageType int, data []byte) error {
	o.mtx.Lock()
	defer o.mtx.Unlock()

	if o.conn == nil {
		return fmt.Errorf("Connection is nil")
	}
	return o.conn.WriteMessage(messageType, data)
}

// SubscribeDynamic позволяет подписаться на новые пары без переподключения
func (o *Okx) SubscribeDynamic(pairs []string) error {
	if len(pairs) == 0 {
		return nil
	}

	o.logger.Info("Dynamically subscribing OKX to new pairs", zap.Strings("pairs", pairs))

	var args []map[string]string

	for _, p := range pairs {
		okxPair := strings.Replace(p, "USDT", "-USDT", 1)

		args = append(args, map[string]string{
			"channel": "tickers",
			"instId":  okxPair,
		})
	}

	subsribeMsg := map[string]interface{}{
		"op":   "subscribe",
		"args": args,
	}
	return o.writeJSON(subsribeMsg)
}
