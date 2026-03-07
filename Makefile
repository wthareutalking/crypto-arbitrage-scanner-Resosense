.PHONY: run restart build stop logs clean

# поднимаем проект в фоновом режиме
run:
	docker-compose up -d --build

# останавливаем все
stop:
	docker-compose down

# логи бота в реальном времени
logs:
	docker-compose logs -f app

# рестарт 
restart: stop run

# локальная сборка бинарника (проверка что все нормально компилируется)
build:
	go build -o bot cmd/bot/main.go