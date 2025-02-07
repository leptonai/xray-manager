package cmd

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	xrayconfig "github.com/xtls/xray-core/infra/conf"
)

func Test_marshalXRayObject(t *testing.T) {
	type testCase struct {
		name    string
		obj     *XRayConfigObject[xrayconfig.OutboundDetourConfig]
		want    []byte
		wantErr bool
	}
	tests := []testCase{
		{
			name: "should marshal outbound detour object",
			obj: &XRayConfigObject[xrayconfig.OutboundDetourConfig]{
				Type: "outbounds",
				Object: xrayconfig.OutboundDetourConfig{
					Tag:      "proxy",
					Protocol: "socks",
					Settings: func() *json.RawMessage {
						data, err := json.Marshal(&xrayconfig.REALITYConfig{})
						assert.NoError(t, err)
						return (*json.RawMessage)(&data)
					}(),
				},
			},
			// original marshal result:
			// {"outbounds":[{"protocol":"socks","sendThrough":null,"tag":"proxy","settings":{"show":false,"masterKeyLog":"","dest":null,"type":"","xver":0,"serverNames":null,"privateKey":"","minClientVer":"","maxClientVer":"","maxTimeDiff":0,"shortIds":null,"fingerprint":"","serverName":"","publicKey":"","shortId":"","spiderX":""},"streamSettings":null,"proxySettings":null,"mux":null}]}
			want:    []byte(`{"outbounds":[{"protocol":"socks","settings":{"fingerprint":"","masterKeyLog":"","maxClientVer":"","maxTimeDiff":0,"minClientVer":"","privateKey":"","publicKey":"","serverName":"","shortId":"","show":false,"spiderX":"","type":"","xver":0},"tag":"proxy"}]}`),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := marshalXRayObject(tt.obj)
			if (err != nil) != tt.wantErr {
				t.Errorf("marshalXRayObject() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("marshalXRayObject() got = %v, want %v", got, tt.want)
			}
		})
	}
}
