package kafka

import (
	"context"
	"encoding/json"

	"github.com/segmentio/kafka-go"
	"github.com/wthareutalking/crypto-arbitrage/internal/models"
	"go.uber.org/zap"
)

type Consumer struct {
	reader *kafka.Reader
	logger *zap.Logger
}

func NewConsumer(brokers []string, topic, groupID string, logger *zap.Logger) *Consumer {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  brokers,
		Topic:    topic,
		GroupID:  groupID, // имя группы. кафка запоминает, что эта группа уже прочитала
		MinBytes: 10e3,    // 10KB - минимальная пачка данных для чтения
		MaxBytes: 10e3,    // 10KB - максимальная пачка
	})

	return &Consumer{
		reader: r,
		logger: logger,
	}
}

// Start запускает бесконечный цикл чтения.
func (c *Consumer) Start(ctx context.Context, handler func(signal models.ArbitrageSignal)) {
	c.logger.Info("Starting Kafka Consumer...")

	for {
		// Читаем сообщение из топика (блокирующий вызов, ждет пока не появится новое)
		m, err := c.reader.ReadMessage(ctx)
		if err != nil {
			// Если контекст отменили, выходим из цикла
			if ctx.Err() != nil {
				return
			}
			c.logger.Error("Failed to read message from Kafka", zap.Error(err))
			continue
		}
		// Распаковываем JSON обратно в структуру Go
		var signal models.ArbitrageSignal
		if err := json.Unmarshal(m.Value, &signal); err != nil {
			c.logger.Error("Failed to unmarshall signal JSON", zap.Error(err))
			continue
		}
		handler(signal)
	}
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}
