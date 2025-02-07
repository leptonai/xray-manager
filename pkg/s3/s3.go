package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	aws_s3_v2 "github.com/aws/aws-sdk-go-v2/service/s3"
	aws_s3_v2_types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/dustin/go-humanize"
	"github.com/olekukonko/tablewriter"

	. "github.com/leptonai/xray-manager/pkg/logger"
)

type Bucket struct {
	Name    string
	Created time.Time

	// Path-style bucket URL.
	// ref. https://docs.aws.amazon.com/AmazonS3/latest/userguide/VirtualHosting.html#path-style-access
	URL string
}

type Buckets []Bucket

func (buckets Buckets) String() string {
	sort.SliceStable(buckets, func(i, j int) bool {
		return buckets[i].Name < buckets[j].Name
	})

	rows := make([][]string, 0, len(buckets))
	for _, v := range buckets {
		row := []string{
			v.Name,
			v.Created.String(),
			v.URL,
		}
		rows = append(rows, row)
	}

	buf := bytes.NewBuffer(nil)
	tb := tablewriter.NewWriter(buf)
	tb.SetAutoWrapText(false)
	tb.SetAlignment(tablewriter.ALIGN_LEFT)
	tb.SetCenterSeparator("*")
	tb.SetHeader([]string{"name", "created", "url"})
	tb.AppendBulk(rows)
	tb.Render()

	return buf.String()
}

// DeleteObject deletes an object.
func DeleteObject(ctx context.Context, cfg aws.Config, bucketName string, s3Key string) error {
	Logger.Infow("deleting object", "bucket", bucketName, "s3Key", s3Key)
	cli := aws_s3_v2.NewFromConfig(cfg)
	_, err := cli.DeleteObject(ctx, &aws_s3_v2.DeleteObjectInput{
		Bucket: &bucketName,
		Key:    &s3Key,
	})
	if err != nil {
		return err
	}

	Logger.Infow("successfully deleted object", "bucket", bucketName, "s3Key", s3Key)
	return nil
}

type Objects struct {
	Objects []aws_s3_v2_types.Object

	// NextContinuationToken is sent when isTruncated is true, which means there are
	// more keys in the bucket that can be listed. The next list requests to Amazon S3
	// can be continued with this NextContinuationToken. NextContinuationToken is
	// obfuscated and is not a real key.
	// ref. https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListObjectsV2.html
	NextContinuationToken string
}

// ListObjects lists all objects in a bucket.
// ref. https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListObjectsV2.html.
func ListObjects(ctx context.Context, cfg aws.Config, bucketName string, opts ...OpOption) (Objects, error) {
	options := &Op{}
	options.applyOpts(opts)

	Logger.Infow("listing objects in bucket", "bucket", bucketName, "maxKeys", options.limit, "prefix", options.prefix)
	cli := aws_s3_v2.NewFromConfig(cfg)

	objects := make([]aws_s3_v2_types.Object, 0)
	token := options.nextContinuationToken
	for {
		input := &aws_s3_v2.ListObjectsV2Input{
			Bucket: &bucketName,
		}
		if options.prefix != "" {
			input.Prefix = &options.prefix
		}
		if token != "" {
			input.ContinuationToken = &token
		}

		out, err := cli.ListObjectsV2(ctx, input)
		if err != nil {
			return Objects{}, err
		}
		Logger.Infow("listed objects", "maxKeys", out.MaxKeys, "truncated", out.IsTruncated, "contents", len(out.Contents))

		if out.IsTruncated != nil && *out.IsTruncated && out.NextContinuationToken != nil && *out.NextContinuationToken != "" {
			token = *out.NextContinuationToken
			Logger.Infow("list has more objects, received non-empty continuation token")
		} else {
			token = ""
		}

		if len(out.Contents) == 0 {
			break
		}

		objects = append(objects, out.Contents...)
		if options.limit > 0 && len(objects) >= options.limit {
			Logger.Infow("received enough objects -- truncating", "limit", options.limit, "totalObjects", len(objects))
			objects = objects[:options.limit]
			break
		}

		if token == "" {
			Logger.Infow("no next page")
			break
		}
	}

	if len(objects) > 1 {
		sort.SliceStable(objects, func(i, j int) bool {
			return (*objects[i].Key) < (*objects[j].Key)
		})
	}

	Logger.Infow("successfully listed bucket", "bucket", bucketName, "objects", len(objects))
	return Objects{
		Objects:               objects,
		NextContinuationToken: token,
	}, nil
}

// PutObjectIOReader uploads a object to a bucket from an io.Reader.
func PutObjectIOReader(ctx context.Context, cfg aws.Config, r io.Reader, bucketName string, s3Key string, opts ...OpOption) error {
	ret := &Op{}
	ret.applyOpts(opts)

	input := &aws_s3_v2.PutObjectInput{
		Bucket:   &bucketName,
		Key:      &s3Key,
		Body:     r,
		Metadata: ret.metadata,
	}
	if ret.objectACL != nil {
		Logger.Infow("putting object with acl", "bucket", bucketName, "s3Key", s3Key, "acl", *ret.objectACL)
		input.ACL = *ret.objectACL
	}
	cli := aws_s3_v2.NewFromConfig(cfg)
	_, err := cli.PutObject(ctx, input)
	if err != nil {
		return err
	}

	if ret.objectACL != nil {
		Logger.Infow("applying put object acl", "bucket", bucketName, "s3Key", s3Key, "acl", *ret.objectACL)
		_, err = cli.PutObjectAcl(ctx, &aws_s3_v2.PutObjectAclInput{
			Bucket: &bucketName,
			Key:    &s3Key,
			ACL:    *ret.objectACL,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// ObjectExists checks if an object exists.
func ObjectExists(ctx context.Context, cfg aws.Config, bucketName string, s3Key string) (*aws_s3_v2.HeadObjectOutput, error) {
	Logger.Infow("checking if s3 key exists", "bucket", bucketName, "s3Key", s3Key)

	cli := aws_s3_v2.NewFromConfig(cfg)
	out, err := cli.HeadObject(ctx, &aws_s3_v2.HeadObjectInput{
		Bucket: &bucketName,
		Key:    &s3Key,
	})
	if err != nil {
		if strings.Contains(err.Error(), "NotFound") {
			return nil, nil
		}
		return nil, err
	}

	Logger.Infow("successfully confirmed that s3 key exists", "bucket", bucketName, "s3Key", s3Key)
	return out, nil
}

// GetObjectIOReader returns an io.Reader for a object in a bucket.
func GetObjectIOReader(ctx context.Context, cfg aws.Config, bucketName string, s3Key string) (io.ReadCloser, error) {
	headOut, err := ObjectExists(ctx, cfg, bucketName, s3Key)
	if err != nil {
		return nil, err
	}
	if headOut == nil {
		return nil, fmt.Errorf("object does not exist: %s/%s", bucketName, s3Key)
	}
	size := int64(0)
	if headOut.ContentLength != nil && *headOut.ContentLength > 0 {
		size = *headOut.ContentLength
	}
	Logger.Infow("downloading file",
		"bucket", bucketName,
		"s3Key", s3Key,
		"size", humanize.Bytes(uint64(size)),
	)

	cli := aws_s3_v2.NewFromConfig(cfg)
	out, err := cli.GetObject(ctx, &aws_s3_v2.GetObjectInput{
		Bucket: &bucketName,
		Key:    &s3Key,
	})
	if err != nil {
		return nil, err
	}

	return out.Body, nil
}

type Op struct {
	limit                 int
	prefix                string
	nextContinuationToken string

	bucketRegion string

	bucketACL       *aws_s3_v2_types.BucketCannedACL
	objectACL       *aws_s3_v2_types.ObjectCannedACL
	objectOwnership *aws_s3_v2_types.ObjectOwnership

	// does not work for Cloudflare R2
	bucketPolicy                string
	bucketBlockPublicACLs       *bool
	bucketBlockPublicPolicy     *bool
	bucketIgnorePublicACLs      *bool
	bucketRestrictPublicBuckets *bool

	serverSideEncryption bool

	// map prefix to expiration days
	// works for Cloudflare R2
	lifecycle map[string]int32

	metadata map[string]string

	preSignDuration time.Duration

	skipBucketPolicy bool
}

type OpOption func(*Op)

func (op *Op) applyOpts(opts []OpOption) {
	for _, opt := range opts {
		opt(op)
	}
}
