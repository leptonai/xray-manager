package logger

import (
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	Logger *zap.SugaredLogger
)

func init() {
	c := zap.NewProductionConfig()
	c.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.RFC3339)
	l, err := c.Build()
	if err != nil {
		panic(err)
	}

	Logger = l.Sugar()
}
