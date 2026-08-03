// Package minutes 编排纪要事实冻结、provider 执行、版本和文件投影。
package minutes

import (
	"encoding/json"
	"fmt"

	domainminutes "meet-sieve/internal/domain/minutes"
)

// BuildProviderInput 把严格白名单事实编码成确定性的 provider 输入。
func BuildProviderInput(context domainminutes.Context) ([]byte, domainminutes.ValidationContext, error) {
	factText := make(map[int64]string, len(context.Facts))
	factAnchor := make(map[int64]string, len(context.Facts))
	resourceSeq := make(map[int64]struct{})
	for _, fact := range context.Facts {
		if fact.Seq <= 0 || fact.Seq > context.Meeting.CutoffSeq || fact.Text == "" {
			return nil, domainminutes.ValidationContext{}, fmt.Errorf("纪要事实范围无效")
		}
		if _, exists := factText[fact.Seq]; exists {
			return nil, domainminutes.ValidationContext{}, fmt.Errorf("纪要事实 seq 重复")
		}
		factText[fact.Seq] = fact.Text
		factAnchor[fact.Seq] = formatAnchor(fact)
		if fact.Kind == domainminutes.FactResource {
			resourceSeq[fact.Seq] = struct{}{}
		}
	}
	payload, err := json.Marshal(context)
	if err != nil {
		return nil, domainminutes.ValidationContext{}, fmt.Errorf("编码纪要白名单事实失败：%w", err)
	}
	validation := domainminutes.ValidationContext{FactText: factText, FactAnchor: factAnchor, ResourceSeq: resourceSeq, GapNotice: append([]domainminutes.GapNotice(nil), context.Gaps...)}
	return payload, validation, nil
}

// formatAnchor 使用样本点或事件序号生成稳定来源锚点。
func formatAnchor(fact domainminutes.Fact) string {
	if fact.Kind != domainminutes.FactUtterance {
		return fmt.Sprintf("#%d", fact.Seq)
	}
	seconds := fact.StartSample / 16000
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}
