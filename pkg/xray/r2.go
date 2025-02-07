package xray

import (
	"bytes"
	"context"
	"encoding/json"

	aws_v2 "github.com/aws/aws-sdk-go-v2/aws"
	aws_s3_v2_types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	xrayconfig "github.com/xtls/xray-core/infra/conf"

	"github.com/leptonai/xray-manager/pkg/s3"
	"github.com/leptonai/xray-manager/pkg/util"
)

var (
	xrayBucketName = "xray"
)

// SaveOutboundDetourConfig saves an outbound detour config.
func SaveOutboundDetourConfig(ctx context.Context, name string, oc xrayconfig.OutboundDetourConfig, objectStoreCfg aws_v2.Config) error {
	o, err := json.Marshal(oc)
	if err != nil {
		return nil
	}
	// xray doesn't add omitempty to its types, and cause issues when client picks it up
	output, err := util.RemoveNullFieldsFromJSON(string(o))
	if err != nil {
		return nil
	}

	err = s3.PutObjectIOReader(ctx, objectStoreCfg, bytes.NewBufferString(output), xrayBucketName, name)
	return err
}

func DecodeJSONFromObject[T any](ctx context.Context, key string, objectStoreCfg aws_v2.Config) (*T, error) {
	r, err := s3.GetObjectIOReader(ctx, objectStoreCfg, xrayBucketName, key)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var obj T
	err = json.NewDecoder(r).Decode(&obj)
	if err != nil {
		return nil, err
	}
	return &obj, nil
}

func ForEachObjects(ctx context.Context, objectStoreCfg aws_v2.Config, fn func(object aws_s3_v2_types.Object) (bool, error)) error {
	objects, err := s3.ListObjects(ctx, objectStoreCfg, xrayBucketName)
	if err != nil {
		return err
	}

	for _, object := range objects.Objects {
		beContinue, err := fn(object)
		if err != nil {
			return err
		}
		if !beContinue {
			break
		}
	}

	return nil
}

// DeleteOutboundDetourConfig removes an outbound detour config.
func DeleteOutboundDetourConfig(ctx context.Context, name string, objectStoreCfg aws_v2.Config) error {
	err := s3.DeleteObject(ctx, objectStoreCfg, xrayBucketName, name)
	return err
}

// GetOutboundDetourConfig gets an outbound detour config for a give name.
func GetOutboundDetourConfig(ctx context.Context, name string, objectStoreCfg aws_v2.Config) (*xrayconfig.OutboundDetourConfig, error) {
	r, err := s3.GetObjectIOReader(ctx, objectStoreCfg, name, xrayBucketName)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	oc := xrayconfig.OutboundDetourConfig{}
	if err := json.NewDecoder(r).Decode(&oc); err != nil {
		return nil, err
	}

	return &oc, nil
}

// ListServerNames lists all server names.
func ListServerNames(ctx context.Context, objectStoreCfg aws_v2.Config) ([]string, error) {
	objects, err := s3.ListObjects(ctx, objectStoreCfg, xrayBucketName)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, object := range objects.Objects {
		names = append(names, *object.Key)
	}

	return names, nil
}
