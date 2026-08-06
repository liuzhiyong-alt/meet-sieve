package speaker

import (
	"fmt"
	"strings"
)

// DisplayName 按参会者、未知聚类、ASR 已稳定的匿名 track 的优先级生成统一用户展示名。
func DisplayName(participantName string, clusterDisplayNo int, trackDisplayNo int) string {
	if name := strings.TrimSpace(participantName); name != "" {
		return name
	}
	if clusterDisplayNo > 0 {
		return fmt.Sprintf("未知说话人 %d", clusterDisplayNo)
	}
	if trackDisplayNo > 0 {
		return fmt.Sprintf("说话人 %d", trackDisplayNo)
	}
	return "未识别说话人"
}
