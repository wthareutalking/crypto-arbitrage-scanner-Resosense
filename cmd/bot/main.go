package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/wthareutalking/crypto-arbitrage/internal/binance"
	"github.com/wthareutalking/crypto-arbitrage/internal/bybit"
	"github.com/wthareutalking/crypto-arbitrage/internal/comparator"
	"github.com/wthareutalking/crypto-arbitrage/internal/exchange"
	"github.com/wthareutalking/crypto-arbitrage/internal/kafka"
	"github.com/wthareutalking/crypto-arbitrage/internal/metrics"
	"github.com/wthareutalking/crypto-arbitrage/internal/notifier"
	"github.com/wthareutalking/crypto-arbitrage/internal/okx"
	"github.com/wthareutalking/crypto-arbitrage/internal/storage/postgres"
	"github.com/wthareutalking/crypto-arbitrage/pkg/config"
	"github.com/wthareutalking/crypto-arbitrage/pkg/logger"
	"github.com/wthareutalking/crypto-arbitrage/pkg/storage"
	"go.uber.org/zap"
)

// startExchangeService — это менеджер жизненного цикла биржи.
// Он запускает процесс подключения в бесконечном цикле.
// Если соединение падает, он создает нового клиента и пробует снова.
func startExchangeService(
	ctx context.Context,
	name string,
	ex exchange.Exchange,
	pairs []string,
	storage *storage.Storage,
	log *zap.Logger,
) {
	go func() {
		for {
			if err := ex.Connect(pairs); err != nil {
				log.Error("Failed to connect. Retrying...",
					zap.String("exchange", name),
					zap.Error(err),
				)
				time.Sleep(5 * time.Second)
				continue
			}
			log.Info("Exchange connected", zap.String("exchange", name))

			for update := range ex.StreamPrices() {
				metrics.PriceUpdates.WithLabelValues(name, update.Symbol).Inc()
				err := storage.SavePrice(ctx, update.Exchange, update.Symbol, update.Bid, update.Ask)
				if err != nil {
					log.Error("Redis save failed", zap.Error(err))
				}
			}
			log.Warn("Connection lost! Restarting...", zap.String("exchange", name))
			time.Sleep(1 * time.Second)
		}
	}()
}

func main() {
	cfg, err := config.LoadConfig("./config")
	if err != nil {
		panic(fmt.Sprintf("Failed to load config:%v", err))
	}
	log := logger.InitLogger(cfg.Env)
	defer log.Sync()

	storageClient := storage.NewStorage(cfg.Redis.Address, cfg.Redis.Password, cfg.Redis.DB, log)

	ctx := context.Background()
	if err := storageClient.Ping(ctx); err != nil {
		log.Fatal("Failed to connect Redis", zap.Error(err))
	}
	log.Info("Connected to Redis successfully")

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		cfg.Database.User, cfg.Database.Password, cfg.Database.Host, cfg.Database.Port, cfg.Database.Name,
	)

	pgStorage, err := postgres.NewStorage(dsn, log)
	if err != nil {
		log.Fatal("Failed to init postgres", zap.Error(err))
	}
	defer pgStorage.Close()

	if err := pgStorage.InitSchema(ctx); err != nil {
		log.Fatal("Failed to init schema", zap.Error(err))
	}

	log.Info("Connected to Postgres successfully")

	log.Info("Starting Crypto Arbitrage Bot",
		zap.String("env", cfg.Env),
		zap.String("binance_url", cfg.Exchanges.BinanceUrl),
	)

	//инициализация бота
	notif, err := notifier.NewNotifier(cfg.Telegram.Token, cfg.Telegram.AdminID, pgStorage, storageClient, cfg.Pairs, log)
	if err != nil {
		log.Fatal("Bot inititalization failed", zap.Error(err))
	}

	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				users, err := pgStorage.GetUsers(ctx)
				if err != nil {
					log.Error("Failed to update user metrics", zap.Error(err))
					continue
				}

				metrics.ActiveUsers.Set(float64(len(users)))
			}
		}
	}()

	go notif.Start(ctx)

	var activeExchanges []exchange.Exchange

	binanceEx := binance.NewBinance(cfg.Exchanges.BinanceUrl, log)
	bybitEx := bybit.NewBybit(cfg.Exchanges.BybitUrl, log)
	okxEx := okx.NewOkx(cfg.Exchanges.OkxUrl, log)

	activeExchanges = append(activeExchanges, binanceEx, bybitEx, okxEx)

	go storageClient.ListenForNewPairs(ctx, activeExchanges)

	//запуск binance (c реконнектом)
	go startExchangeService(ctx, "binance", binanceEx, cfg.Pairs, storageClient, log)

	//запуск bybit (c реконнектом)
	go startExchangeService(ctx, "bybit", bybitEx, cfg.Pairs, storageClient, log)

	//запуск okx (с реконнектом)
	go startExchangeService(ctx, "okx", okxEx, cfg.Pairs, storageClient, log)

	//настройки кафки
	// "kafka:29092" - это внутренний адрес брокера внутри сети Docker Compose
	kafkaBroker := "resosense-kafka:29092" //используем строку для ф-ии initTopic
	kafkaBrokers := []string{kafkaBroker}  // используем слайс для producer
	kafkaTopic := "arbitrage-signals"
	kafkaGroupID := "telegram-notifier-group"

	// инициализируем топик
	log.Info("Waiting for Kafka to start...")
	time.Sleep(5 * time.Second)
	if err := kafka.InitTopic(kafkaBroker, kafkaTopic, log); err != nil {
		log.Fatal("Failed to init Kafka topic", zap.Error(err))
	}

	// создаем читателя
	cons := kafka.NewConsumer(kafkaBrokers, kafkaTopic, kafkaGroupID, log)
	defer cons.Close()

	go cons.Start(ctx, notif.BroadcastSignal)

	prod := kafka.NewProducer(kafkaBrokers, kafkaTopic, log)
	defer prod.Close()

	//запуск аналитики
	comp := comparator.NewComparator(storageClient, log, cfg.Pairs, prod)
	go comp.Start(ctx)

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	log.Info("Bot is running. Press CTRL + C to stop.")

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		log.Info("Starting metrics server on :2112")
		if err := http.ListenAndServe(":2112", nil); err != nil {
			log.Error("Metrics server failed", zap.Error(err))
		}
	}()

	<-stopChan

	log.Info("Shutting down...")

	log.Info("Bot stopped.")

}
