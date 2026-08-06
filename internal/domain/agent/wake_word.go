package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	minWakeWordRunes     = 3
	maxWakeWordRunes     = 16
	maxLeadingSeparators = 6
)

// WakeWord 是规范化后可持久化的句首唤醒词。
type WakeWord struct {
	Value string
	Hash  string
}

// NormalizeWakeWord 执行 NFKC、空白折叠和字符范围校验。
func NormalizeWakeWord(value string) (WakeWord, error) {
	normalized := norm.NFKC.String(value)
	for _, current := range normalized {
		if unicode.IsControl(current) {
			return WakeWord{}, fmt.Errorf("唤醒词不能包含控制字符")
		}
	}
	normalized = strings.Join(strings.Fields(normalized), " ")
	count := utf8.RuneCountInString(normalized)
	if count < minWakeWordRunes || count > maxWakeWordRunes {
		return WakeWord{}, fmt.Errorf("唤醒词必须为 %d～%d 个字符", minWakeWordRunes, maxWakeWordRunes)
	}
	hasLetterOrNumber := false
	for _, current := range normalized {
		if unicode.IsLetter(current) || unicode.IsNumber(current) {
			hasLetterOrNumber = true
			continue
		}
		if current != ' ' && !isCommonPunctuation(current) {
			return WakeWord{}, fmt.Errorf("唤醒词包含不支持的字符")
		}
	}
	if !hasLetterOrNumber {
		return WakeWord{}, fmt.Errorf("唤醒词不能只有标点")
	}
	digest := sha256.Sum256([]byte(normalized))
	return WakeWord{Value: normalized, Hash: hex.EncodeToString(digest[:])}, nil
}

// WakeMatcher 对持久化 ASR final 执行句首精确匹配。
type WakeMatcher struct {
	wake WakeWord
}

// NewWakeMatcher 创建只匹配给定规范唤醒词的 matcher。
func NewWakeMatcher(wake WakeWord) *WakeMatcher {
	return &WakeMatcher{wake: wake}
}

// Match 返回唤醒词后的非空问题；不匹配时返回空字符串。
func (matcher *WakeMatcher) Match(finalText string) string {
	matched, question := matcher.MatchPrefix(finalText)
	if !matched {
		return ""
	}
	return question
}

// MatchPrefix 匹配句首唤醒词，并允许 ASR 在唤醒词内部增删空白或常见标点。
// 返回 matched=true、question="" 表示只识别到了唤醒词，调用方可以继续等待后续 final。
func (matcher *WakeMatcher) MatchPrefix(finalText string) (matched bool, question string) {
	if matcher == nil || matcher.wake.Value == "" {
		return false, ""
	}
	input := []rune(norm.NFKC.String(finalText))
	wakeKey := significantRunes(matcher.wake.Value)
	position := skipLeadingSeparators(input)
	for _, expected := range wakeKey {
		for position < len(input) && isSeparator(input[position]) {
			position++
		}
		if position >= len(input) || unicode.ToLower(input[position]) != unicode.ToLower(expected) {
			return false, ""
		}
		position++
	}
	if position < len(input) && !isSeparator(input[position]) {
		return false, ""
	}
	for position < len(input) && isSeparator(input[position]) {
		position++
	}
	return true, strings.TrimSpace(string(input[position:]))
}

// MatchWakeOnly 判断 final 是否只包含句首唤醒词和允许的首尾分隔符。
// 该入口仅用于设置页真实三次测试，不会放宽会中提问必须含非空问题的规则。
func (matcher *WakeMatcher) MatchWakeOnly(finalText string) bool {
	matched, question := matcher.MatchPrefix(finalText)
	return matched && question == ""
}

// significantRunes 生成仅用于比较的唤醒 key，不改变用户保存和页面展示的原值。
func significantRunes(value string) []rune {
	result := make([]rune, 0, utf8.RuneCountInString(value))
	for _, current := range []rune(norm.NFKC.String(value)) {
		if !isSeparator(current) {
			result = append(result, current)
		}
	}
	return result
}

// skipLeadingSeparators 最多跳过六个句首空白或常见标点。
func skipLeadingSeparators(runes []rune) int {
	index := 0
	for index < len(runes) && index < maxLeadingSeparators && isSeparator(runes[index]) {
		index++
	}
	return index
}

// isSeparator 判断唤醒词边界允许的空白或常见标点。
func isSeparator(value rune) bool {
	return unicode.IsSpace(value) || isCommonPunctuation(value)
}

// isCommonPunctuation 限定可预测的中英文会议口语标点。
func isCommonPunctuation(value rune) bool {
	return strings.ContainsRune("，。！？、；：,.!?;:（）()【】[]《》<>“”\"'—-", value)
}
