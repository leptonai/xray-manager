package r2

import (
	"context"
	"time"

	aws_v2 "github.com/aws/aws-sdk-go-v2/aws"
)

const (
	// ref. https://developers.cloudflare.com/r2/reference/data-location/#available-hints
	RegionUS = "wnam"
)

const (
	LocationHintUS   = "WNAM"
	LocationHintAPAC = "APAC"
)

func NewCfg(account, accessKey, accessSecret string, opts ...OpOption) *aws_v2.Config {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := NewAWSCompatibleConfig(ctx, account, accessKey, accessSecret, opts...)
	if err != nil { // don't expect error
		panic(err)
	}

	return &cfg
}
