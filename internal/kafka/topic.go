package kafka

import (
	"fmt"
	"net"
	"strconv"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

// InitTopic проверяет существование топика и создает его, если нужно
func InitTopic(brokerAddress, topic string, logger *zap.Logger) error {
	// 1. Подключаемся к любому брокеру, чтобы узнать топологию кластера
	conn, err := kafka.Dial("tcp", brokerAddress)
	if err != nil {
		return fmt.Errorf("Failed to dial broker: %w", err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("Failed to get controller: %w", err)
	}
	// Формируем адрес контроллера и подключаемся к нему
	controllerConn, err := kafka.Dial("tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		return fmt.Errorf("Failed to dial controller: %w", err)
	}
	defer controllerConn.Close()
	// 4. Описываем конфигурацию нашего топика
	topicConfigs := []kafka.TopicConfig{
		{
			Topic:             topic,
			NumPartitions:     1,
			ReplicationFactor: 1,
		},
	}
	// Отправляем запрос на создание
	err = controllerConn.CreateTopics(topicConfigs...)
	//если топик уже существует, Kafka вернет ошибку kafka.TopicAlreadyExists.
	// для нас это НЕ ошибка (топик есть, всё ок), поэтому мы её игнорируем.
	if err != nil && err != kafka.TopicAlreadyExists {
		return fmt.Errorf("Failed to create topic: %w", err)
	}

	logger.Info("Kafka topic initializied successfully", zap.String("topic", topic))
	return nil
}
