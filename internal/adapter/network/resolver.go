package network

import (
	"context"
	"fmt"
	"net"
	"net/netip"

	guestdomain "meet-sieve/internal/domain/guest"
)

const defaultRouteUnknown = "default_route_unknown"

// Resolver 枚举当前系统网卡，不开启任何 Listener。
type Resolver struct{}

// NewResolver 创建无状态的平台网卡解析器。
func NewResolver() *Resolver {
	return &Resolver{}
}

// Resolve 返回 IPv4 候选；默认路由解析失败时保留候选供主持人手动选择。
func (resolver *Resolver) Resolve(ctx context.Context) (guestdomain.InterfaceResolution, error) {
	systemInterfaces, err := net.Interfaces()
	if err != nil {
		return guestdomain.InterfaceResolution{}, fmt.Errorf("枚举系统网卡: %w", err)
	}
	defaultName, defaultAddress, known := resolveDefaultRoute(ctx)
	resolution := guestdomain.InterfaceResolution{DefaultRouteKnown: known}
	if !known {
		resolution.Warning = defaultRouteUnknown
	}
	resolution.Interfaces = projectInterfaces(systemInterfaces, defaultName, defaultAddress)
	return resolution, nil
}

// projectInterfaces 将系统网卡显式投影为不包含 MAC 的领域值。
func projectInterfaces(interfaces []net.Interface, defaultName string, defaultAddress string) []guestdomain.NetworkInterface {
	result := make([]guestdomain.NetworkInterface, 0, len(interfaces))
	for _, systemInterface := range interfaces {
		addresses, err := systemInterface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ipv4, ok := extractIPv4(address.String())
			if !ok {
				continue
			}
			result = append(result, guestdomain.NetworkInterface{
				ID:           guestdomain.StableInterfaceID(systemInterface.Index, systemInterface.Name, systemInterface.HardwareAddr.String()),
				Name:         systemInterface.Name,
				Address:      ipv4,
				Up:           systemInterface.Flags&net.FlagUp != 0,
				Loopback:     systemInterface.Flags&net.FlagLoopback != 0,
				DefaultRoute: systemInterface.Name == defaultName || ipv4 == defaultAddress,
			})
		}
	}
	return result
}

// extractIPv4 从网卡 CIDR 或单地址中提取标准 IPv4。
func extractIPv4(value string) (string, bool) {
	if prefix, err := netip.ParsePrefix(value); err == nil && prefix.Addr().Is4() {
		return prefix.Addr().String(), true
	}
	address, err := netip.ParseAddr(value)
	if err != nil || !address.Is4() {
		return "", false
	}
	return address.String(), true
}
