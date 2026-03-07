package config

import (
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Port      string         `mapstructure:"port"`
	Env       string         `mapstructure:"env"`
	Pairs     []string       `mapstructure:"pairs"`
	Exchanges ExchangeConfig `mapstructure:"exchanges"`
	Redis     RedisConfig    `mapstructure:"redis"`
	Telegram  TelegramConfig `mapstructure:"telegram"`
	Database  DatabaseConfig `mapstructure:"database"`
}

type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     string `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
}

type TelegramConfig struct {
	Token   string `mapstructure:"token"`
	AdminID int64  `mapstructure:"admin_id"`
}

type RedisConfig struct {
	Address  string `mapstructure:"address"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type ExchangeConfig struct {
	BinanceUrl string `mapstructure:"binance_url"`
	BybitUrl   string `mapstructure:"bybit_url"`
	OkxUrl     string `mapstructure:"okx_url"`
}

func LoadConfig(path string) (config Config, err error) {
	viper.AddConfigPath(path)
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	viper.AutomaticEnv()

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	err = viper.ReadInConfig()
	if err != nil {
		return
	}

	err = viper.Unmarshal(&config)
	return

}
