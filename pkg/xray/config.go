package xray

import (
	"fmt"

	xrayconfig "github.com/xtls/xray-core/infra/conf"
)

const (
	protocolVless = "vless"
)

// ServerConfigToOutboundDetourConfig converts a server config to an outbound detour config.
// We assume that
//   - the inbound config protocol is neither "freedom" nor "dokodemo-door".
//   - the server config contains only one inbound config, which is the corresponding outbount config of the client.
func ServerConfigToClientOutboundDetourConfig(serverName, publicKey string, serverConfig xrayconfig.Config, weight int, vlessPath string) (*xrayconfig.OutboundDetourConfig, error) {
	var oc *xrayconfig.OutboundDetourConfig
	var err error

	for _, inbound := range serverConfig.InboundConfigs {
		if inbound.Protocol == "freedom" || inbound.Protocol == "dokodemo-door" {
			continue
		}

		switch inbound.Protocol {
		case protocolVless:
			if inbound.StreamSetting.Network != nil && *inbound.StreamSetting.Network == "ws" {
				oc, err = VLessAndWSInboundToClientOutBound(inbound, vlessPath)
				if err != nil {
					return nil, fmt.Errorf("failed to create vless + WS client outbound: %w", err)
				}
			}
			if inbound.StreamSetting.Network != nil && *inbound.StreamSetting.Network == "tcp" &&
				inbound.StreamSetting.Security == "reality" {
				oc, err = VLessAndRealityInboundToClientOutBound(serverName, publicKey, inbound, weight)
				if err != nil {
					return nil, fmt.Errorf("failed to create vless + TCP client outbound: %w", err)
				}
			}
		default:
			return nil, fmt.Errorf("protocol %s not supported", inbound.Protocol)
		}
		return oc, nil // only one inbound config is expected
	}

	return nil, fmt.Errorf("no eligible inbound config in server config")
}

func getServerConfigBase() xrayconfig.Config {
	return xrayconfig.Config{
		LogConfig: &xrayconfig.LogConfig{
			AccessLog: "/var/log/v2ray/access.log",
			ErrorLog:  "/var/log/v2ray/v2ray.log",
			LogLevel:  "info",
		},
		API: &xrayconfig.APIConfig{
			Tag: "api",
			Services: []string{
				"HandlerService",
				"LoggerService",
				"StatsService",
			},
		},
		InboundConfigs: []xrayconfig.InboundDetourConfig{},
		OutboundConfigs: []xrayconfig.OutboundDetourConfig{
			{
				Protocol: "freedom",
			},
		},
	}
}
