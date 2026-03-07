package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/wthareutalking/crypto-arbitrage/internal/exchange"
	"go.uber.org/zap"
)

type Storage struct {
	client *redis.Client
	logger *zap.Logger
}

func NewStorage(addr, password string, db int, logger *zap.Logger) *Storage {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	return &Storage{
		client: client,
		logger: logger,
	}
}

func (s *Storage) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *Storage) SavePrice(ctx context.Context, exchange, symbol string, bid, ask float64) error {
	key := fmt.Sprintf("price:%s:%s", exchange, symbol)

	err := s.client.HSet(ctx, key, map[string]interface{}{
		"bid":  bid,
		"ask":  ask,
		"time": time.Now().UnixMilli(),
	}).Err()

	if err != nil {
		return fmt.Errorf("failed to save price: %w", err)
	}

	s.client.Expire(ctx, key, 5*time.Second)
	return nil
}

type PriceData struct {
	Bid       float64 `redis:"bid"`
	Ask       float64 `redis:"ask"`
	Timestamp int64   `redis:"time"`
}

func (s *Storage) GetPrice(ctx context.Context, exchange, symbol string) (*PriceData, error) {
	key := fmt.Sprintf("price:%s:%s", exchange, symbol)

	var price PriceData
	err := s.client.HGetAll(ctx, key).Scan(&price)
	if err != nil {
		return nil, err
	}

	if price.Timestamp == 0 {
		return nil, fmt.Errorf("price not found")
	}

	return &price, nil
}

// сохранение состояния пользователя (fsm)
func (s *Storage) SetStage(ctx context.Context, userID int64, state string) error {
	key := fmt.Sprintf("fsm:%d", userID)
	return s.client.Set(ctx, key, state, 10*time.Minute).Err() //ttl 10 min
}

// получает текущее состояние
func (s *Storage) GetState(ctx context.Context, userID int64) (string, error) {
	key := fmt.Sprintf("fsm:%d", userID)
	val, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

// сбрасывает состояние
func (s *Storage) DelState(ctx context.Context, userID int64) error {
	key := fmt.Sprintf("fsm:%d", userID)
	return s.client.Del(ctx, key).Err()
}

// SetAlertLock устанавливает блокировку на рассылку для конкретной пары.
// Возвращает true, если блокировка успешно установлена (можно слать сигнал).
// Возвращает false, если блокировка уже существует (надо проигнорировать сигнал).
func (s *Storage) SetAlertLock(ctx context.Context, userID int64, pair string, ttl time.Duration) (bool, error) {
	key := fmt.Sprintf("alert_lock:%d:%s", userID, pair)

	//set if not exist, она записывает значение ТОЛЬКО если ключа еще нет.
	ok, err := s.client.SetNX(ctx, key, true, ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// SetAlertLocksBulk устанавливает замки сразу для массива пользователей за 1 Pipeline
// Возвращает map, где ключ - это ID юзера, а значение - true (можно слать) или false (замок уже висел)
func (s *Storage) SetAlertLocksBulk(ctx context.Context, userIDs []int64, pair string, ttl time.Duration) (map[int64]bool, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	pipe := s.client.Pipeline()

	// Словарь для хранения результатов команд
	cmds := make(map[int64]*redis.BoolCmd)
	for _, id := range userIDs {
		key := fmt.Sprintf("alert_lock:%d:%s", id, pair)
		// Кладем команду в пайплайн, но ОНА ЕЩЕ НЕ ОТПРАВЛЕНА в базу
		cmds[id] = pipe.SetNX(ctx, key, true, ttl)
	}
	// Отправляем всю пачку команд в Redis одним TCP-пакетом
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("pipeline execution failed: %w", err)
	}

	results := make(map[int64]bool)
	for id, cmd := range cmds {
		results[id] = cmd.Val() // Вытаскиваем true/false для каждого юзера
	}
	return results, nil
}

// listenForNewPairs подписывается на Redis Pub/Sub и ждет новых пар от Telegram-бота
func (s *Storage) ListenForNewPairs(ctx context.Context, exchanges []exchange.Exchange) {
	//подключаемся к чистоте new_pairs
	pubsub := s.client.Subscribe(ctx, "new_pairs")
	defer pubsub.Close()

	ch := pubsub.Channel()

	s.logger.Info("Started listening for new pairs via Redis Pub/Sub...")

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			// В msg.Payload прилетает текст сообщения. Мы ожидаем, что это будет название монеты (например, "DOGEUSDT")
			newPair := msg.Payload
			s.logger.Info("Received new pair from Redis!", zap.String("pair", newPair))

			pairsToSubscribe := []string{newPair}

			for _, ex := range exchanges {
				err := ex.SubscribeDynamic(pairsToSubscribe)
				if err != nil {
					s.logger.Error("Failed to dynamically subscribe exchange", zap.Error(err), zap.String("pair", newPair))
				}
			}
		}
	}
}

// PublishNewPair отправляет название новой монеты в канал Redis Pub/Sub
func (s *Storage) PuiblishNewPair(ctx context.Context, pair string) error {
	// Publish принимает контекст, имя канала ("new_pairs") и само сообщение (название монеты)
	err := s.client.Publish(ctx, "new_pairs", pair).Err()
	if err != nil {
		s.logger.Error("Failed to publish new pair to Redis", zap.Error(err), zap.String("pair", pair))
		return err
	}
	return nil
}
