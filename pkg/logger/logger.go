package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func InitLogger(env string) *zap.Logger {
	var config zap.Config

	if env == "prod" {
		config = zap.NewProductionConfig()
		config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	} else {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	logger, err := config.Build()
	if err != nil {
		panic(err)
	}
	return logger
}
