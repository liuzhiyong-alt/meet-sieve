package meeting

import (
	"errors"
	"strings"

	peopledomain "meet-sieve/internal/domain/people"
)

// ParticipantInput 是创建会议时选择的成员或临时参会者输入。
type ParticipantInput struct {
	// MemberID 非空时引用活动成员；临时参会者保持为空。
	MemberID string
	// DisplayName 是写入本场不可变快照的展示名称。
	DisplayName string
}

// ParticipantSnapshot 是不受后续成员资料变化影响的本场参会者事实。
type ParticipantSnapshot struct {
	MemberID    string
	Kind        string
	DisplayName string
	SortOrder   int
}

// BuildParticipantSnapshots 按首次出现顺序去重真实成员并生成连续排序快照。
func BuildParticipantSnapshots(inputs []ParticipantInput) ([]ParticipantSnapshot, error) {
	if len(inputs) == 0 {
		return nil, errors.New("创建会议至少需要一位参会者")
	}
	seenMembers := make(map[string]struct{}, len(inputs))
	seenNames := make(map[string]struct{}, len(inputs))
	result := make([]ParticipantSnapshot, 0, len(inputs))
	for _, input := range inputs {
		name := strings.TrimSpace(input.DisplayName)
		if name == "" {
			return nil, errors.New("参会者名称不能为空")
		}
		if input.MemberID != "" {
			if _, exists := seenMembers[input.MemberID]; exists {
				continue
			}
			seenMembers[input.MemberID] = struct{}{}
		}
		normalizedName, err := peopledomain.NormalizeName(name)
		if err != nil {
			return nil, err
		}
		if _, exists := seenNames[normalizedName]; exists {
			return nil, errors.New("参会者名称重复")
		}
		seenNames[normalizedName] = struct{}{}
		if input.MemberID == "" {
			result = append(result, ParticipantSnapshot{
				Kind: "temporary", DisplayName: name, SortOrder: len(result),
			})
			continue
		}
		result = append(result, ParticipantSnapshot{
			MemberID: input.MemberID, Kind: "member", DisplayName: name, SortOrder: len(result),
		})
	}
	return result, nil
}
