package guest

import (
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

// SelectionReason 说明私有网络选择结果。
type SelectionReason string

const (
	// SelectionReasonRecommended 表示找到明确的私网默认路由。
	SelectionReasonRecommended SelectionReason = "recommended"
	// SelectionReasonManualRequired 表示有私网候选，但需要主持人手动确认。
	SelectionReasonManualRequired SelectionReason = "manual_required"
	// SelectionReasonUnavailable 表示没有可安全绑定的私有 IPv4。
	SelectionReasonUnavailable SelectionReason = "unavailable"
)

// NetworkInterface 是系统网卡的最小安全领域投影。
type NetworkInterface struct {
	ID           string
	Name         string
	Address      string
	Up           bool
	Loopback     bool
	DefaultRoute bool
}

// InterfaceSelection 包含可手动选择的候选和可选推荐项。
type InterfaceSelection struct {
	Recommended *NetworkInterface
	Choices     []NetworkInterface
	Reason      SelectionReason
}

// InterfaceResolution 保留平台枚举结果和默认路由是否可靠解析的事实。
type InterfaceResolution struct {
	Interfaces        []NetworkInterface
	DefaultRouteKnown bool
	Warning           string
}

// SelectPrivateInterfaces 过滤虚拟、隧道和非私有地址，且只在默认路由明确时推荐。
func SelectPrivateInterfaces(interfaces []NetworkInterface) InterfaceSelection {
	choices := make([]NetworkInterface, 0, len(interfaces))
	for _, candidate := range interfaces {
		if isSafePrivateInterface(candidate) {
			choices = append(choices, candidate)
		}
	}
	sort.SliceStable(choices, func(i int, j int) bool {
		if choices[i].DefaultRoute != choices[j].DefaultRoute {
			return choices[i].DefaultRoute
		}
		if choices[i].Name != choices[j].Name {
			return choices[i].Name < choices[j].Name
		}
		return choices[i].Address < choices[j].Address
	})

	selection := InterfaceSelection{Choices: choices, Reason: SelectionReasonUnavailable}
	if len(choices) == 0 {
		return selection
	}
	selection.Reason = SelectionReasonManualRequired
	if choices[0].DefaultRoute {
		recommended := choices[0]
		selection.Recommended = &recommended
		selection.Reason = SelectionReasonRecommended
	}
	return selection
}

// StableInterfaceID 对平台网卡标识做稳定摘要，避免 DTO 暴露 MAC 等系统信息。
func StableInterfaceID(index int, name string, hardwareAddress string) string {
	identity := strconv.Itoa(index) + "\x00" + name + "\x00" + hardwareAddress
	digest := sha256.Sum256([]byte(identity))
	return "lan-" + hex.EncodeToString(digest[:8])
}

// IsPrivateBindAddress 判断地址是否为可显式绑定的 RFC 1918 IPv4。
func IsPrivateBindAddress(value string) bool {
	address, err := netip.ParseAddr(value)
	return err == nil && address.Is4() && !address.IsLoopback() && !address.IsLinkLocalUnicast() && isRFC1918(address)
}

// isSafePrivateInterface 判断网卡是否可用于明确 IP 绑定。
func isSafePrivateInterface(candidate NetworkInterface) bool {
	if !candidate.Up || candidate.Loopback || isVirtualInterfaceName(candidate.Name) {
		return false
	}
	address, err := netip.ParseAddr(candidate.Address)
	if err != nil || !address.Is4() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	return isRFC1918(address)
}

// isRFC1918 只接受 RFC 1918 IPv4，不把其他特殊地址静默当作可信 LAN。
func isRFC1918(address netip.Addr) bool {
	return address.Is4() && (address.IsPrivate())
}

// isVirtualInterfaceName 过滤常见 VPN、容器、虚拟机和隧道网卡。
func isVirtualInterfaceName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	prefixes := []string{
		"utun", "tun", "tap", "ipsec", "ppp", "gif", "stf",
		"docker", "br-", "virbr", "vbox", "vmnet", "vethernet", "wsl",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}
