# 🚀 Crypto Arbitrage Scanner & SaaS Bot

![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?style=flat&logo=postgresql)
![Redis](https://img.shields.io/badge/Redis-7.0-DC382D?style=flat&logo=redis)
![Kafka](https://img.shields.io/badge/Kafka-7.4-231F20?style=flat&logo=apachekafka)
![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?style=flat&logo=docker)

Enterprise-grade сервис для поиска межбиржевого крипто-арбитража в реальном времени. Система подключается к биржам (Binance, Bybit, OKX) по WebSockets, анализирует стаканы с задержкой <100мс и рассылает торговые сигналы пользователям в Telegram.

Проект спроектирован с упором на **HighLoad, отказоустойчивость и чистую архитектуру**.

## 🧠 Архитектура и Решения (Under the Hood)

Вместо монолитного спагетти-кода, система разделена на логические слои:

*   **Data Ingestion (Сбор данных):** Независимые горутины держат постоянные WSS-соединения с биржами. Реализован паттерн *Graceful Degradation* (авто-реконнект при обрывах) и Thread Safety (Mutex) для конкурентной отправки пингов и подписок.
*   **In-Memory Storage:** Цены мгновенно оседают в **Redis (HSET)** с коротким TTL (5 секунд). Это исключает чтение устаревших данных (stale data), если WSS-поток биржи завис.
*   **Event-Driven Messaging (Kafka):** Компаратор не блокируется на отправку сообщений в Telegram. При нахождении спреда он сериализует событие (`ArbitrageSignal`) и кидает его в топик **Apache Kafka** (`LeastBytes` balancer, партиционирование по ключу монеты).
*   **Smart Anti-Spam (Redis Pipelining):** Чтобы не зафлудить пользователей сигналами по одной и той же монете, перед отправкой пуша Consumer делает пакетный запрос `SetNX` в Redis через **Pipeline**. Это позволяет проверить сотни пользователей за 1 сетевой вызов (O(1) Network I/O).
*   **Control Plane (Pub/Sub):** При добавлении юзером новой торговой пары в Telegram-боте, система не перезагружается. Событие транслируется через **Redis Pub/Sub**, и WSS-воркеры динамически (на лету) обновляют свои подписки.
*   **Stateful Bot (FSM):** Телеграм-бот является *Stateless* на уровне сервиса. Состояния конечного автомата (FSM) при диалогах хранятся в Redis.

## 🛠 Технологический Стек

*   **Язык:** Golang (Clean Architecture)
*   **Брокер сообщений:** Apache Kafka (segmentio/kafka-go)
*   **Кэш / Pub-Sub / Locks:** Redis (redis/go-redis/v9)
*   **База данных (Persistent):** PostgreSQL + pgx/v5 (использование JSONB для гибких настроек юзеров)
*   **Инфраструктура:** Docker, Docker Compose, Makefile
*   **Мониторинг:** Prometheus (Promauto Counters/Gauges), Grafana, uber-go/zap (Structured JSON Logging)

## 🚀 Быстрый старт (Quick Start)

1. Клонируйте репозиторий:
    ```bash
    git clone https://github.com/wthareutalking/crypto-arbitrage-scanner.git
    cd crypto-arbitrage-scanner

2. Скопируйте пример конфигурации и заполните свои данные (Telegram Token, пароли БД):
    ```bash
    cp config/config.example.yaml config/config.yaml
    cp .env.example .env

3. Запустите весь кластер одной командой (БД, Кафка, Redis, Мониторинг, Бот):
    ```bash
    make run

4. Остановка и просмотр логов:
    ``` bash
    make logs   # стрим логов
    make stop   # аккуратная остановка контейнеров

📊 Мониторинг
Метрики бизнес-логики отдаются на порту :2112/metrics. Доступны:
```
resosense_price_updates_total — интенсивность WSS-потоков.

resosense_arbitrage_found_total — счетчик найденных вилок.

resosense_active_users — Gauge активных подписок.

go_goroutines - сколько горутин работает прямо сейчас.

go_gc_duration_seconds_sum - сколько времени (в секундах) приложение потратило на работу Сборщика мусора (GC) с момента запуска.
