// Package network 把操作系统网卡和默认路由投影为 Guest 领域模型。
package network

import (
	"bufio"
	"bytes"
	"net/netip"
	"strconv"
	"strings"
)

// parseDarwinDefaultInterface 从 `route -n get default` 输出中读取网卡名。
func parseDarwinDefaultInterface(output []byte) (string, bool) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		key, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(key) != "interface" {
			continue
		}
		name := strings.TrimSpace(value)
		return name, name != ""
	}
	return "", false
}

// parseWindowsDefaultInterfaceAddress 从 IPv4 route table 中返回 metric 最小的默认路由接口地址。
func parseWindowsDefaultInterfaceAddress(output []byte) (string, bool) {
	bestAddress := ""
	bestMetric := int(^uint(0) >> 1)
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 || fields[0] != "0.0.0.0" || fields[1] != "0.0.0.0" {
			continue
		}
		address, err := netip.ParseAddr(fields[3])
		metric, metricErr := strconv.Atoi(fields[4])
		if err != nil || !address.Is4() || metricErr != nil || metric >= bestMetric {
			continue
		}
		bestAddress = address.String()
		bestMetric = metric
	}
	return bestAddress, bestAddress != ""
}
