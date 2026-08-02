// Package people 定义成员和小组资料的领域值与规则。
package people

import (
	"fmt"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// NormalizeName 将成员或小组展示名称转换为活动名称唯一约束使用的稳定键。
// 返回值会执行 NFKC、Unicode 空白折叠与 Unicode case folding，空结果会被拒绝。
func NormalizeName(name string) (string, error) {
	normalized := norm.NFKC.String(name)
	normalized = strings.Join(strings.Fields(normalized), " ")
	normalized = cases.Fold().String(normalized)
	if normalized == "" {
		return "", fmt.Errorf("名称不能为空")
	}
	return normalized, nil
}
