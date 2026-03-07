package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type Storage struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func NewStorage(dsn string, logger *zap.Logger) (*Storage, error) {
	//dsn - data source name - строка подключения
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}
	return &Storage{
		pool:   pool,
		logger: logger,
	}, nil
}

func (s *Storage) Close() {
	s.pool.Close()
}

func (s *Storage) InitSchema(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS users(
		id BIGINT PRIMARY KEY, -- Telegram ID
		username TEXT,
		is_admin BOOLEAN DEFAULT FALSE,
		subscription_end TIMESTAMP,
		created_at TIMESTAMP DEFAULT NOW()
	);
	`
	_, err := s.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("Create table failed: %w", err)
	}

	queryAlter := `
		ALTER TABLE users
		ADD COLUMN IF NOT EXISTS settings JSONB DEFAULT '{}'::jsonb;
	`

	_, err = s.pool.Exec(ctx, queryAlter)
	if err != nil {
		return fmt.Errorf("alter table failed: %w", err)
	}
	return nil
}
