package kafka

import (
	"context"
	"encoding/json"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

// Producer - обертка над писателем Кафки
type Producer struct {
	writer *kafka.Writer
	logger *zap.Logger
}

func NewProducer(brokers []string, topic string, logger *zap.Logger) *Producer {
	w := &kafka.Writer{
		Addr:  kafka.TCP(brokers...),
		Topic: topic,
		// LeastBytes балансирует нагрузку между брокерами Кафки (отправляет туда, где меньше трафика)
		Balancer: &kafka.LeastBytes{},
	}

	return &Producer{
		writer: w,
		logger: logger,
	}
}

// SendMessage принимает интерфейс (любую структуру), сериализует в JSON и шлет в Кафку
func (p *Producer) SendMessage(ctx context.Context, key string, msg interface{}) error {
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	//формируем сообщение для кафки
	kafkaMsg := kafka.Message{
		Key:   []byte(key),
		Value: msgBytes,
	}

	//отправляем в брокер
	err = p.writer.WriteMessages(ctx, kafkaMsg)
	if err != nil {
		p.logger.Error("Failed to write message to Kafka", zap.Error(err))
		return err
	}

	p.logger.Info("Signal sent to Kafka", zap.String("key", key))
	return nil
}

func (p *Producer) Close() error {
	return p.writer.Close()
}
