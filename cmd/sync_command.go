package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	aws_v2 "github.com/aws/aws-sdk-go-v2/aws"
	aws_s3_v2_types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/spf13/cobra"
	xrayservice "github.com/xtls/xray-core/app/proxyman/command"
	xrayconfig "github.com/xtls/xray-core/infra/conf"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	. "github.com/leptonai/xray-manager/pkg/logger"
	"github.com/leptonai/xray-manager/pkg/s3/r2"
	"github.com/leptonai/xray-manager/pkg/util"
	"github.com/leptonai/xray-manager/pkg/xray"
)

const (
	defaultSyncInterval = 30 * time.Second
	defaultAPITimeout   = 30 * time.Second
)

type clientSyncCommand struct {
	configDir         string
	serverAddr        string
	syncInterval      time.Duration
	runOnce           bool
	updateToXRay      bool
	r2AccountID       string
	r2AccessKeyID     string
	r2AccessKeySecret string
	r2Region          string
}

func NewClientSyncCommand() *cobra.Command {
	syncCmd := &clientSyncCommand{
		syncInterval: defaultSyncInterval,
		updateToXRay: true,
	}
	cmd := &cobra.Command{
		Use:        "sync",
		Short:      "Implements xray-manager sync sub-command",
		SuggestFor: []string{"sync"},
		Run: func(cmd *cobra.Command, args []string) {
			syncCmd.Run()
		},
	}
	flags := cmd.PersistentFlags()
	flags.StringVar(&syncCmd.configDir, "xray-confdir", "", "The XRay's config directory with multiple json config")
	flags.StringVar(&syncCmd.serverAddr, "xray-server", "", "XRay server address")
	flags.DurationVar(&syncCmd.syncInterval, "sync-internal", syncCmd.syncInterval, "Sync configs interval")
	flags.BoolVar(&syncCmd.runOnce, "run-once", syncCmd.runOnce, "Only run once if enabled")
	flags.BoolVar(&syncCmd.updateToXRay, "update-to-xray", syncCmd.updateToXRay, "Sync files to local only")
	flags.StringVarP(&r2AccountID, "r2-account-id", "", "", "Account ID of the R2 object store")
	flags.StringVarP(&r2AccessKeyID, "r2-access-key-id", "", "", "Access key ID of the R2 object store")
	flags.StringVarP(&r2AccessKeySecret, "r2-access-key-secret", "", "", "Access key secret of the R2 object store")
	flags.StringVarP(&r2Region, "r2-region", "", r2.RegionUS, "Region of the R2 object store")
	return cmd
}

func (cmd *clientSyncCommand) Run() {
	if cmd.configDir == "" {
		Logger.Infof("please set --xray-confdir")
		os.Exit(1)
	}
	if cmd.updateToXRay && cmd.serverAddr == "" {
		Logger.Infof("please set --xray-server")
		os.Exit(1)
	}
	if cmd.syncInterval <= defaultSyncInterval {
		cmd.syncInterval = defaultSyncInterval
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	for {
		cmd.worker()
		if cmd.runOnce {
			return
		}

		select {
		case sig := <-sigCh:
			Logger.Infof("got exist signal %v", sig)
			return
		case <-time.After(cmd.syncInterval):
		}
	}
}

func (cmd *clientSyncCommand) worker() {
	localOCS, err := loadConfigsFromDir[xrayconfig.OutboundDetourConfig](cmd.configDir, "outbounds")
	if err != nil {
		Logger.Errorf("failed to load outbound configs: %v from dir %s", err, cmd.configDir)
		return
	}

	ctx := context.WithoutCancel(context.Background())
	objectStoreCfg := *r2.NewCfg(r2AccountID, r2AccessKeyID, r2AccessKeySecret, r2.WithRegion(r2Region))
	remoteOCS, err := pullXRayObjectsFromR2[xrayconfig.OutboundDetourConfig](ctx, "outbounds", objectStoreCfg)
	if err != nil {
		Logger.Errorf("failed to sync remote r2, err: %v", err)
		return
	}

	// Prevent the risk of xray service not functioning properly
	// because someone deletes all R2 files by mistake
	if len(remoteOCS) == 0 {
		Logger.Infof("there are no remote objects to sync")
		return
	}

	same, toBeAdded, toBeDeleted := diffXRayObjects(remoteOCS, localOCS)
	logDiffResult(same, toBeAdded, toBeDeleted)

	if len(toBeAdded) == 0 && len(toBeDeleted) == 0 {
		Logger.Infof("all objects are same, skip sync")
		return
	}

	//
	// The purpose of updating TAG is to avoid the following scenarios
	// 1. Create an outbound config in R2, named oc, version v1
	// 2. Update oc1 with some changes, version v2
	// 3. We can directly remove v1 oc from xray server,
	//    but if the v2 oc is illegal, there are no outbound objects,
	//    all traffic only be redirected to freedom.
	// 4. But we try adding the v2 oc1 with new tag to xray server,
	//    if it fails, there is no impact.
	// 5. Then we can remove the v1 oc from xray server safely.
	//
	updateOutboundObjectTags(toBeAdded)

	var xrayClient *XRayServiceClient
	if cmd.updateToXRay {
		xrayClient, err = NewXRayServiceClient(cmd.serverAddr)
		if err != nil {
			Logger.Errorf("failed to NewXRayServiceClient, err: %v", err)
			return
		}
	}

	if cmd.updateToXRay {
		if err := operationWithTimeout(ctx, defaultAPITimeout, func(ctx context.Context) error {
			return xrayClient.AddOutboundObjects(ctx, toBeAdded)
		}); err != nil {
			Logger.Errorf("failed to add outbound objects from beAdded, err: %v", err)
			return
		}
		Logger.Infof("successfully add outbound objects from beAdded, count: %d", len(toBeAdded))
	}

	if err := syncRemoteObjectsToFiles(cmd.configDir, toBeAdded); err != nil {
		Logger.Errorf("failed to sync remote objects to files, err: %v", err)
		return
	}
	Logger.Infof("successfully sync remote objects to files, count: %d", len(toBeAdded))

	if cmd.updateToXRay {
		if err := operationWithTimeout(ctx, defaultAPITimeout, func(ctx context.Context) error {
			return xrayClient.RemoveObjects(ctx, toBeDeleted)
		}); err != nil {
			Logger.Errorf("failed to remove outbound objects from beDeleted, err: %v", err)
			return
		}
		Logger.Infof("successfully remove outbound objects from beDeleted, count: %d", len(toBeDeleted))
	}

	if err := deleteXRayObjectsFromLocal(cmd.configDir, toBeDeleted); err != nil {
		Logger.Errorf("failed to delete local, err: %v", err)
		return
	}
	Logger.Infof("synchronization task completed successfully")
}

type XRayConfigObject[T any] struct {
	// Name represents the R2 object name
	// and also used as a part of the local file
	Name string
	// ETag comes from the R2 object,
	// and we use this value to determine whether the object has changed.
	ETag string
	// Type is a part of the local file
	Type   string
	Object T
}

func (obj *XRayConfigObject[T]) String() string {
	return fmt.Sprintf("%s.%s.%s", obj.Name, obj.Type, obj.ETag)
}

func makeJSONFileName[T any](obj *XRayConfigObject[T]) string {
	return fmt.Sprintf("%s.json", obj.String())
}

func loadConfigsFromDir[T any](configDir string, objectType string) (map[string]*XRayConfigObject[T], error) {
	confs, err := os.ReadDir(configDir)
	if err != nil {
		return nil, err
	}

	//
	// The file name contains some necessary information, such as R2 object name, R2 object E-Tag
	// and the XRay object type corresponding to the file (outbounds, inbounds, rules etc.).
	// We can use regexp to extract this necessary information from the file name.
	//
	reg, err := regexp.Compile(fmt.Sprintf("(.*).%s.(.*).json", objectType))
	if err != nil {
		return nil, err
	}
	m := make(map[string]*XRayConfigObject[T])
	for _, v := range confs {
		submatch := reg.FindStringSubmatch(v.Name())
		if len(submatch) < 3 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(configDir, v.Name()))
		if err != nil {
			return nil, err
		}

		obj, err := unmarshalXRayObject[T](data)
		if err != nil {
			return nil, err
		}

		name := submatch[1]
		etag := submatch[2]
		configObj := &XRayConfigObject[T]{
			Name:   name,
			ETag:   etag,
			Type:   objectType,
			Object: *obj,
		}
		m[configObj.String()] = configObj
	}
	return m, nil
}

func unmarshalXRayObject[T any](data []byte) (*T, error) {
	m := map[string][]T{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	var obj *T
	for _, v := range m {
		if len(v) > 0 {
			obj = &v[0]
			break
		}
	}
	if obj == nil {
		return nil, fmt.Errorf("failed to unmarshal T")
	}
	return obj, nil
}

func marshalXRayObject[T any](obj *XRayConfigObject[T]) ([]byte, error) {
	m := map[string]interface{}{
		obj.Type: []T{obj.Object},
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	// The XRay configuration object does not use omitempty to define the json tag.
	// Therefore, the result after marshal contains many null fields, it may cause xray to fail when restarting.
	// Related code https://github.com/XTLS/Xray-core/blob/07350533488193175764a43f7e0bfe5c1efabed2/infra/conf/transport_internet.go#L484
	s, err := util.RemoveNullFieldsFromJSON(string(data))
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

func syncRemoteObjectsToFiles[T any](configDir string, objects []*XRayConfigObject[T]) error {
	for _, obj := range objects {
		data, err := marshalXRayObject(obj)
		if err != nil {
			return fmt.Errorf("failed to marshal object for %s, err: %w", obj.Name, err)
		}

		file := filepath.Join(configDir, makeJSONFileName(obj))
		err = util.WriteFileAtomic(file, data)
		if err != nil {
			return fmt.Errorf("failed to write object for %s, err: %w", file, err)
		}
		Logger.Infof("successfully write object for %s to %s", obj.Name, file)
	}
	return nil
}

func deleteXRayObjectsFromLocal[T any](configDir string, toBeDeletedObjects []*XRayConfigObject[T]) error {
	for _, obj := range toBeDeletedObjects {
		file := filepath.Join(configDir, makeJSONFileName(obj))
		err := os.Remove(file)
		if err != nil {
			return fmt.Errorf("failed to delete object for %s, err: %w", obj.Name, err)
		}
		Logger.Infof("successfully delete object for %s from %s", obj.Name, file)
	}
	return nil
}

func pullXRayObjectsFromR2[T any](ctx context.Context, objectType string, objectStoreCfg aws_v2.Config) (map[string]*XRayConfigObject[T], error) {
	// Ensure sync all outbound objects, either everything is synchronized, or everything fails.
	// This simplifies the entire implementation
	m := map[string]*XRayConfigObject[T]{}
	err := xray.ForEachObjects(ctx, objectStoreCfg, func(object aws_s3_v2_types.Object) (bool, error) {
		oc, err := xray.DecodeJSONFromObject[T](ctx, *object.Key, objectStoreCfg)
		if err != nil {
			Logger.Errorf("failed to DecodeJSONFromObject from r2, key: %s, err: %v", *object.Key, err)
			return false, err
		}

		etag := strings.Replace(*object.ETag, `"`, "", -1)
		name := *object.Key
		configObj := &XRayConfigObject[T]{
			Name:   name,
			ETag:   etag,
			Type:   objectType,
			Object: *oc,
		}
		m[configObj.String()] = configObj
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

func diffXRayObjects[T any](remote, local map[string]*XRayConfigObject[T]) (same, toBeAdded, toBeDeleted []*XRayConfigObject[T]) {
	for k, remoteObj := range remote {
		localObj := local[k]
		if localObj == nil {
			toBeAdded = append(toBeAdded, remoteObj)
		} else if localObj.ETag != remoteObj.ETag {
			toBeAdded = append(toBeAdded, remoteObj)
			// here we cannot remove the object from local since we will remove them later.
		} else {
			same = append(same, localObj)
			delete(local, k)
		}
	}
	for _, v := range local {
		toBeDeleted = append(toBeDeleted, v)
	}
	return
}

func operationWithTimeout(ctx context.Context, timeout time.Duration, fn func(ctx context.Context) error) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return fn(timeoutCtx)
}

func updateOutboundObjectTags(objects []*XRayConfigObject[xrayconfig.OutboundDetourConfig]) []string {
	var oldTags []string
	for _, v := range objects {
		oldTags = append(oldTags, v.Object.Tag)
		v.Object.Tag = strings.Join([]string{v.Object.Tag, v.Name, v.ETag}, ".")
	}
	return oldTags
}

func logDiffResult[T any](same, tobeAdded, toBeDeleted []*XRayConfigObject[T]) {
	for _, v := range same {
		Logger.Infof("%s is not changed, skip it", v.Name)
	}
	for _, v := range tobeAdded {
		Logger.Infof("%s should be added", v.Name)
	}
	for _, v := range toBeDeleted {
		Logger.Infof("%s should be deleted", v.Name)
	}
}

type XRayServiceClient struct {
	conn   *grpc.ClientConn
	client xrayservice.HandlerServiceClient
}

func NewXRayServiceClient(serverAddr string) (*XRayServiceClient, error) {
	conn, err := dialAPIServer(serverAddr)
	if err != nil {
		return nil, err
	}
	return &XRayServiceClient{
		conn:   conn,
		client: xrayservice.NewHandlerServiceClient(conn),
	}, nil
}

func (c *XRayServiceClient) Close() error {
	return c.conn.Close()
}

func (c *XRayServiceClient) AddOutboundObjects(ctx context.Context, objects []*XRayConfigObject[xrayconfig.OutboundDetourConfig]) error {
	for _, outObj := range objects {
		Logger.Infof("adding outbound object, %s", outObj.Object.Tag)
		o, err := outObj.Object.Build()
		if err != nil {
			Logger.Errorf("failed to build object, err: %v, name: %s, tag: %s", err, outObj.Name, outObj.Object.Tag)
			return err
		}
		r := &xrayservice.AddOutboundRequest{
			Outbound: o,
		}
		resp, err := c.client.AddOutbound(ctx, r)
		if err != nil {
			if strings.Contains(err.Error(), "existing tag found") {
				Logger.Warnf("try adding existing tag, name: %s, tag: %s", outObj.Name, outObj.Object.Tag)
				continue
			}
			Logger.Errorf("failed to add outbound, err: %v, name: %s, tag: %s", err, outObj.Name, outObj.Object.Tag)
			return err
		}
		Logger.Infof("successfully add outbound %s to xray server", outObj.Name)
		showJSONResponse(resp)
	}
	return nil
}

func (c *XRayServiceClient) RemoveObjects(ctx context.Context, toBeDeletedObjects []*XRayConfigObject[xrayconfig.OutboundDetourConfig]) error {
	for _, object := range toBeDeletedObjects {
		Logger.Infof("removing outbound %s", object.Object.Tag)
		r := &xrayservice.RemoveOutboundRequest{
			Tag: object.Object.Tag,
		}
		resp, err := c.client.RemoveOutbound(ctx, r)
		if err != nil {
			Logger.Errorf("failed to remove outbound %s, err: %v", object.Object.Tag, err)
			return err
		}
		Logger.Infof("successfully remove outbound %s from xray server", object.Object.Tag)
		showJSONResponse(resp)
	}
	return nil
}

func dialAPIServer(serverAddr string) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultAPITimeout)
	defer cancel()
	return grpc.DialContext(ctx, serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
}

func protoToJSONString(m proto.Message, prefix, indent string) (string, error) {
	return strings.TrimSpace(protojson.MarshalOptions{Indent: indent}.Format(m)), nil
}

func showJSONResponse(m proto.Message) {
	if Logger.Level() != zapcore.DebugLevel {
		return
	}
	if m == nil {
		return
	}
	output, err := protoToJSONString(m, "", "    ")
	if err != nil {
		Logger.Errorf("failed to convert to json, err: %v", err)
		return
	}
	Logger.Info(output)
}
