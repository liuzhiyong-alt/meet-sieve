package volcano

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"meet-sieve/internal/port"
)

// BuildInitialPayload 构造固定 16kHz、16-bit、mono PCM 的初始化请求。
func BuildInitialPayload(request port.RealtimeTranscriptionRequest) ([]byte, error) {
	if request.MeetingID == "" || request.StartSample < 0 || request.Format != (port.AudioFormat{SampleRate: 16000, BitsPerSample: 16, Channels: 1}) {
		return nil, fmt.Errorf("实时转写初始化参数无效")
	}
	payload := initialPayload{
		User:  initialUser{UID: request.MeetingID},
		Audio: initialAudio{Format: "pcm", Codec: "raw", Rate: 16000, Bits: 16, Channel: 1},
		Request: initialRequest{
			ModelName: "bigmodel", EnableNonstream: true, EnableITN: true, EnablePunc: true,
			EnableDDC: false, EnableSpeakerInfo: true, SSDVersion: "200", ShowUtterances: true, ResultType: "full",
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("编码实时转写初始化请求失败：%w", err)
	}
	return data, nil
}

// ParseTranscriptionEvents 把一个服务端 JSON response 转换成不含厂商 JSON 的业务事件。
func ParseTranscriptionEvents(payload []byte, localSessionID string, providerSessionID string, responseSequence int32, inputStartSample int64, lastSentSample int64) ([]port.TranscriptionEvent, error) {
	return parseTranscriptionEvents(payload, localSessionID, providerSessionID, responseSequence, inputStartSample, lastSentSample, 0)
}

// parseTranscriptionEvents 允许 adapter 在已确认写入边界附近容忍 provider 的时间取整误差。
func parseTranscriptionEvents(payload []byte, localSessionID string, providerSessionID string, responseSequence int32, inputStartSample int64, lastSentSample int64, timestampTolerance int64) ([]port.TranscriptionEvent, error) {
	if localSessionID == "" || responseSequence == 0 {
		return nil, fmt.Errorf("实时转写响应上下文无效")
	}
	var response asrResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("解析火山实时转写响应失败：%w", err)
	}
	if len(response.Result.Utterances) == 0 {
		text := strings.TrimSpace(response.Result.Text)
		if text == "" {
			return []port.TranscriptionEvent{}, nil
		}
		return []port.TranscriptionEvent{{Type: port.TranscriptionPartial, Revision: responseRevision(responseSequence), SessionID: localSessionID, ProviderSessionID: providerSessionID, ResultID: "stream", ProviderResultID: strconv.FormatInt(int64(responseSequence), 10), Text: text}}, nil
	}
	events := make([]port.TranscriptionEvent, 0, len(response.Result.Utterances))
	for index, utterance := range response.Result.Utterances {
		event, err := mapUtterance(utterance, localSessionID, providerSessionID, responseSequence, index, inputStartSample, lastSentSample, timestampTolerance)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

type initialPayload struct {
	User    initialUser    `json:"user"`
	Audio   initialAudio   `json:"audio"`
	Request initialRequest `json:"request"`
}

type initialUser struct {
	UID string `json:"uid"`
}

type initialAudio struct {
	Format  string `json:"format"`
	Codec   string `json:"codec"`
	Rate    int    `json:"rate"`
	Bits    int    `json:"bits"`
	Channel int    `json:"channel"`
}

type initialRequest struct {
	ModelName         string `json:"model_name"`
	EnableNonstream   bool   `json:"enable_nonstream"`
	EnableITN         bool   `json:"enable_itn"`
	EnablePunc        bool   `json:"enable_punc"`
	EnableDDC         bool   `json:"enable_ddc"`
	EnableSpeakerInfo bool   `json:"enable_speaker_info"`
	SSDVersion        string `json:"ssd_version"`
	ShowUtterances    bool   `json:"show_utterances"`
	ResultType        string `json:"result_type"`
}

type asrResponse struct {
	Result struct {
		Text       string         `json:"text"`
		Utterances []asrUtterance `json:"utterances"`
	} `json:"result"`
}

type asrUtterance struct {
	Definite  bool         `json:"definite"`
	StartTime int64        `json:"start_time"`
	EndTime   int64        `json:"end_time"`
	Text      string       `json:"text"`
	SpeakerID speakerValue `json:"speaker_id"`
	SpkID     speakerValue `json:"spk_id"`
	Speaker   speakerValue `json:"speaker"`
	Additions asrAdditions `json:"additions"`
}

// asrAdditions 承接优化流式 utterance 的附加说话人字段。
// 火山实际响应可能把 additions 编码为对象或 JSON 字符串。
type asrAdditions struct {
	Speaker     speakerValue
	SpeakerID   speakerValue
	SpkID       speakerValue
	SpeakerInfo struct {
		SpeakerID speakerValue `json:"speaker_id"`
	}
}

// UnmarshalJSON 兼容 additions 的对象与 JSON 字符串形态。
func (additions *asrAdditions) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || string(data) == `""` {
		*additions = asrAdditions{}
		return nil
	}
	var encoded string
	if err := json.Unmarshal(data, &encoded); err == nil {
		data = []byte(encoded)
	}
	type additionsPayload struct {
		Speaker     speakerValue `json:"speaker"`
		SpeakerID   speakerValue `json:"speaker_id"`
		SpkID       speakerValue `json:"spk_id"`
		SpeakerInfo struct {
			SpeakerID speakerValue `json:"speaker_id"`
		} `json:"speaker_info"`
	}
	var payload additionsPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("解析火山 additions 失败：%w", err)
	}
	additions.Speaker = payload.Speaker
	additions.SpeakerID = payload.SpeakerID
	additions.SpkID = payload.SpkID
	additions.SpeakerInfo = payload.SpeakerInfo
	return nil
}

// speakerValue 兼容火山响应中 string 或 number 形态的说话人标识。
type speakerValue string

// UnmarshalJSON 把 string/number 统一成稳定字符串，拒绝其他字段类型。
func (value *speakerValue) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*value = ""
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*value = speakerValue(strings.TrimSpace(text))
		return nil
	}
	var number json.Number
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return fmt.Errorf("火山说话人标识类型无效")
	}
	*value = speakerValue(number.String())
	return nil
}

// mapUtterance 严格验证时间范围，并以服务端 sequence 与条目序号组成稳定 result ID。
func mapUtterance(utterance asrUtterance, localSessionID string, providerSessionID string, responseSequence int32, index int, inputStartSample int64, lastSentSample int64, timestampTolerance int64) (port.TranscriptionEvent, error) {
	text := strings.TrimSpace(utterance.Text)
	if text == "" || utterance.StartTime < 0 || utterance.EndTime <= utterance.StartTime {
		return port.TranscriptionEvent{}, fmt.Errorf("火山实时转写 utterance 字段不完整")
	}
	startSample, err := providerMillisecondsToSampleWithTolerance(inputStartSample, utterance.StartTime, lastSentSample, timestampTolerance)
	if err != nil {
		return port.TranscriptionEvent{}, err
	}
	endSample, err := providerMillisecondsToSampleWithTolerance(inputStartSample, utterance.EndTime, lastSentSample, timestampTolerance)
	if err != nil || endSample <= startSample {
		return port.TranscriptionEvent{}, fmt.Errorf("火山实时转写 utterance 样本范围无效")
	}
	speakerID := normalizedUtteranceSpeaker(utterance)
	resultID := stableUtteranceResultID(speakerID, utterance.StartTime)
	eventType := port.TranscriptionPartial
	providerResultID := fmt.Sprintf("%d:%d", responseSequence, index)
	if utterance.Definite {
		eventType = port.TranscriptionFinal
		// 优化流式端点会在后续响应中重复携带历史 definite，final 必须使用稳定业务键去重。
		providerResultID = resultID
	}
	return port.TranscriptionEvent{Type: eventType, Revision: responseRevision(responseSequence), ResultID: resultID, ProviderResultID: providerResultID, SessionID: localSessionID, ProviderSessionID: providerSessionID, Text: text, SpeakerID: speakerID, SpeakerLabel: speakerID, StartSample: startSample, EndSample: endSample}, nil
}

// normalizedUtteranceSpeaker 优先使用优化流式 additions 字段，再兼容历史根字段。
func normalizedUtteranceSpeaker(utterance asrUtterance) string {
	values := []speakerValue{
		utterance.Additions.Speaker,
		utterance.Additions.SpeakerID,
		utterance.Additions.SpkID,
		utterance.Additions.SpeakerInfo.SpeakerID,
		utterance.SpeakerID,
		utterance.SpkID,
		utterance.Speaker,
	}
	for _, value := range values {
		if normalized := strings.TrimSpace(string(value)); normalized != "" {
			return normalized
		}
	}
	return ""
}

// stableUtteranceResultID 在供应商缺少 speaker 时仍生成可重放的 final 幂等键。
func stableUtteranceResultID(speakerID string, startTime int64) string {
	if speakerID == "" {
		return fmt.Sprintf("unlabeled:%d", startTime)
	}
	return fmt.Sprintf("%s:%d", speakerID, startTime)
}

// responseRevision 把正负服务端 sequence 统一为单调非负 revision。
func responseRevision(sequence int32) int64 {
	value := int64(sequence)
	if value < 0 {
		return -value
	}
	return value
}

// providerMillisecondsToSample 使用整数四舍五入映射，越界时不裁剪。
func providerMillisecondsToSample(inputStartSample int64, providerMS int64, lastSentSample int64) (int64, error) {
	return providerMillisecondsToSampleWithTolerance(inputStartSample, providerMS, lastSentSample, 0)
}

// providerMillisecondsToSampleWithTolerance 将 provider 时间限制在已确认写入 PCM 边界内。
func providerMillisecondsToSampleWithTolerance(inputStartSample int64, providerMS int64, lastSentSample int64, toleranceSamples int64) (int64, error) {
	if inputStartSample < 0 || providerMS < 0 || lastSentSample < inputStartSample {
		return 0, fmt.Errorf("火山实时转写时间映射参数无效")
	}
	if toleranceSamples < 0 || toleranceSamples > maxTimestampToleranceSamples {
		return 0, fmt.Errorf("火山实时转写时间容忍范围无效")
	}
	sample := inputStartSample + (providerMS*16000+500)/1000
	if sample < inputStartSample || sample > lastSentSample+toleranceSamples {
		return 0, fmt.Errorf("火山实时转写时间超出已发送 PCM")
	}
	if sample > lastSentSample {
		return lastSentSample, nil
	}
	return sample, nil
}
