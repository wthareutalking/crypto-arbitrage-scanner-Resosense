package comparator

import (
	"context"
	"fmt"
	"time"

	"github.com/wthareutalking/crypto-arbitrage/internal/kafka"
	"github.com/wthareutalking/crypto-arbitrage/internal/metrics"
	"github.com/wthareutalking/crypto-arbitrage/internal/models"
	"github.com/wthareutalking/crypto-arbitrage/pkg/storage"
	"go.uber.org/zap"
)

type Compatator struct {
	storage   *storage.Storage
	logger    *zap.Logger
	pairs     []string
	producer  *kafka.Producer
	exchanges []string
}

func NewComparator(storage *storage.Storage, logger *zap.Logger, pairs []string, producer *kafka.Producer) *Compatator {
	return &Compatator{
		storage:   storage,
		logger:    logger,
		pairs:     pairs,
		producer:  producer,
		exchanges: []string{"binance", "bybit", "okx"},
	}
}

func (c *Compatator) Start(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkArbitrage(ctx)
		}
	}
}

func (c *Compatator) checkArbitrage(ctx context.Context) {
	for _, pair := range c.pairs {
		// Собираем актуальные цены со ВСЕХ бирж для текущей пары
		// Ключ - название биржи, а значение - ее цена
		prices := make(map[string]*storage.PriceData)

		for _, exchName := range c.exchanges {
			price, err := c.storage.GetPrice(ctx, exchName, pair)
			if err != nil && price != nil {
				prices[exchName] = price
			}
		}
		if len(prices) < 2 {
			continue
		}
		// Двойной цикл: сравниваем каждую биржу с каждой
		for buyExchange, buyPriceData := range prices {
			for sellExchange, sellPriceData := range prices {
				//не сравниваем одно и то же
				if buyExchange == sellExchange {
					continue
				}

				if buyPriceData.Ask <= 0 || sellPriceData.Bid <= 0 {
					continue
				}

				//высчитывание профита
				spread := (sellPriceData.Bid - buyPriceData.Ask) / buyPriceData.Ask * 100

				if spread > 0 {
					metrics.ArbitrageFound.Inc()

					direction := fmt.Sprintf("%s -> %s", buyExchange, sellExchange)

					signal := models.ArbitrageSignal{
						Pair:      pair,
						Direction: direction,
						Spread:    spread,
						BuyPrice:  buyPriceData.Ask,
						SellPrice: sellPriceData.Bid,
						Timestamp: time.Now().Unix(),
					}
					c.producer.SendMessage(ctx, pair, signal)
				}
			}
		}
	}
}
