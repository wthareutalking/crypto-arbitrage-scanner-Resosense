package postgres

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"
)

type User struct {
	ID              int64
	Username        string
	IsAdmin         bool
	SubscriptionEnd time.Time
	Settings        UserSettings
}

type UserSettings struct {
	MinSpread float64  `json:"min_spread"`
	Pairs     []string `json:"pairs"`
	Language  string   `json:"language"`
}

func (s *Storage) SaveUser(ctx context.Context, id int64, username string) error {
	query := `
	INSERT INTO users (id,username, created_at)
	VALUES ($1, $2, NOW())
	ON CONFLICT (id) DO UPDATE
	SET username = $2;
	`
	_, err := s.pool.Exec(ctx, query, id, username)
	return err
}

func (s *Storage) GetUser(ctx context.Context, id int64) (*User, error) {
	query := `SELECT id, username, is_admin, subscription_end, settings FROM users WHERE id = $1`

	var user User
	var subEnd *time.Time
	var settingsBytes []byte

	err := s.pool.QueryRow(ctx, query, id).Scan(&user.ID, &user.Username, &user.IsAdmin, &subEnd, &settingsBytes)
	if err != nil {
		return nil, err
	}

	if subEnd != nil {
		user.SubscriptionEnd = *subEnd
	}

	//парсинг json -> структура
	if len(settingsBytes) > 0 {
		if err := json.Unmarshal(settingsBytes, &user.Settings); err != nil {
			s.logger.Warn("Failed to unmarshal settings", zap.Int64("uid", id))
		}
	}
	return &user, nil
}

func (s *Storage) GetUsers(ctx context.Context) ([]User, error) {
	query := `SELECT id, username, is_admin, subscription_end, settings FROM users`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		var subEnd *time.Time
		var settingsBytes []byte
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin, &subEnd, &settingsBytes); err != nil {
			return nil, err
		}

		if subEnd != nil {
			u.SubscriptionEnd = *subEnd
		}

		//парсинг json -> структура
		if len(settingsBytes) > 0 {
			json.Unmarshal(settingsBytes, &u.Settings)
		}

		users = append(users, u)
	}
	return users, nil
}

func (s *Storage) UpdateSettings(ctx context.Context, id int64, settings UserSettings) error {
	//структура -> json
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	query := `UPDATE users SET settings = $1 WHERE id = $2`
	_, err = s.pool.Exec(ctx, query, data, id)
	return err
}
