package xray

import (
	"encoding/json"
	"fmt"

	"github.com/samber/lo"
	xrayconfig "github.com/xtls/xray-core/infra/conf"
)

const (
	vlessWSPort = 443
)

// Fields like Port and Path are hard coded since they are related to the nginx server in front of vless, hince not kept in server config
func VLessAndWSInboundToClientOutBound(inboundConfig xrayconfig.InboundDetourConfig, vlessPath string) (*xrayconfig.OutboundDetourConfig, error) {
	vlessSetting := xrayconfig.VLessInboundConfig{}
	if inboundConfig.Settings == nil {
		return nil, fmt.Errorf("inboundConfig settings is nil")
	}
	err := json.Unmarshal(*inboundConfig.Settings, &vlessSetting)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal vless setting %w", err)
	}

	convertedClients, err := convertVlessUserToInboundFormat(vlessSetting.Clients)
	if err != nil {
		return nil, fmt.Errorf("failed to add encryption setting to user: %w", err)
	}
	users, err := json.Marshal(convertedClients)
	if err != nil {
		return nil, fmt.Errorf("failed marshal clients object: %w", err)
	}
	settings := []byte(fmt.Sprintf(`{"vnext":[{"address":"%s","port":%d,"users":%s}]}`, inboundConfig.Tag, vlessWSPort, string(users)))

	return &xrayconfig.OutboundDetourConfig{
		Protocol: protocolVless,
		Tag:      "proxy-" + inboundConfig.Tag,
		Settings: lo.ToPtr(json.RawMessage(settings)),
		StreamSetting: &xrayconfig.StreamConfig{
			Network:  lo.ToPtr(xrayconfig.TransportProtocol("ws")),
			Security: "tls",
			TLSSettings: &xrayconfig.TLSConfig{
				ServerName: inboundConfig.Tag,
			},
			WSSettings: &xrayconfig.WebSocketConfig{
				// Hard coding it since there's no easy way to pass from vless server(the config is kept in nginx in the front)
				Path: vlessPath,
			},
			SocketSettings: &xrayconfig.SocketConfig{
				Mark: 255,
			},
		},
	}, nil
}

func VLessAndRealityInboundToClientOutBound(serverName string, publicKey string, inboundConfig xrayconfig.InboundDetourConfig, weight int) (*xrayconfig.OutboundDetourConfig, error) {
	vlessSetting := xrayconfig.VLessInboundConfig{}
	if inboundConfig.Settings == nil {
		return nil, fmt.Errorf("inboundConfig settings is nil")
	}
	if inboundConfig.PortList == nil || len(inboundConfig.PortList.Range) == 0 {
		return nil, fmt.Errorf("inboundConfig portlist is nil")
	}
	err := json.Unmarshal(*inboundConfig.Settings, &vlessSetting)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal vless setting %w", err)
	}

	convertedClients, err := convertVlessUserToInboundFormat(vlessSetting.Clients)
	if err != nil {
		return nil, fmt.Errorf("failed to add encryption setting to user: %w", err)
	}
	users, err := json.Marshal(convertedClients)
	if err != nil {
		return nil, fmt.Errorf("failed marshal clients object: %w", err)
	}
	settings := []byte(fmt.Sprintf(`{"vnext":[{"address":"%s","port":%d,"users":%s}]}`, serverName, inboundConfig.PortList.Range[0].From, string(users)))

	odc := &xrayconfig.OutboundDetourConfig{
		Protocol:      protocolVless,
		Tag:           clientOutboundTag(inboundConfig, weight),
		Settings:      lo.ToPtr(json.RawMessage(settings)),
		StreamSetting: inboundConfig.StreamSetting,
	}

	odc.StreamSetting.REALITYSettings.Fingerprint = "chrome"
	odc.StreamSetting.REALITYSettings.SpiderX = "/"
	odc.StreamSetting.SocketSettings = &xrayconfig.SocketConfig{
		Mark: 255,
	}

	if len(odc.StreamSetting.REALITYSettings.ServerNames) == 0 {
		return nil, fmt.Errorf("no server names in reality settings")
	}
	odc.StreamSetting.REALITYSettings.ServerName = odc.StreamSetting.REALITYSettings.ServerNames[0]
	odc.StreamSetting.REALITYSettings.ServerNames = nil

	if len(odc.StreamSetting.REALITYSettings.ShortIds) == 0 {
		return nil, fmt.Errorf("no short ids in reality settings")
	}
	odc.StreamSetting.REALITYSettings.ShortId = odc.StreamSetting.REALITYSettings.ShortIds[0]
	odc.StreamSetting.REALITYSettings.Dest = nil
	odc.StreamSetting.REALITYSettings.ShortIds = nil

	odc.StreamSetting.REALITYSettings.PublicKey = publicKey

	return odc, nil
}

func clientOutboundTag(inboundConfig xrayconfig.InboundDetourConfig, weight int) string {
	return "proxy-" + inboundConfig.Tag + "-weight" + fmt.Sprintf("%d", weight)
}

// to work around messy type configurations of xray
// e.g. vless server client encryption field should not be set; but the field is required in client config
type VlessOutboundUser struct {
	ID         string `json:"id,omitempty"`
	Flow       string `json:"flow,omitempty"`
	Encryption string `json:"encryption,omitempty"`
	Level      int    `json:"level,omitempty"`
}

func convertVlessUserToInboundFormat(users []json.RawMessage) ([]json.RawMessage, error) {
	converted := []json.RawMessage{}
	for _, u := range users {
		user := VlessOutboundUser{}
		if err := json.Unmarshal(u, &user); err != nil {
			return nil, fmt.Errorf("invalid user %s: %w", string(u), err)
		}
		if user.Encryption == "" {
			user.Encryption = "none"
		}
		output, err := json.Marshal(user)
		if err != nil {
			return nil, fmt.Errorf("invalid converted user %v: %s", user, err)
		}
		converted = append(converted, json.RawMessage(output))
	}
	return converted, nil
}
