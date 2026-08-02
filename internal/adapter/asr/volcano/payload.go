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
		User:    initialUser{UID: request.MeetingID},
		Audio:   initialAudio{Format: "pcm", Codec: "raw", Rate: 16000, Bits: 16, Channel: 1},
		Request: initialRequest{ModelName: "bigmodel", EnableITN: true, EnablePunc: true, ShowUtterances: true, ResultType: "full"},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("编码实时转写初始化请求失败：%w", err)
	}
	return data, nil
}

// ParseTranscriptionEvents 把一个服务端 JSON response 转换成不含厂商 JSON 的业务事件。
func ParseTranscriptionEvents(payload []byte, localSessionID string, providerSessionID string, responseSequence int32, inputStartSample int64, lastSentSample int64) ([]port.TranscriptionEvent, error) {
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
		event, err := mapUtterance(utterance, localSessionID, providerSessionID, responseSequence, index, inputStartSample, lastSentSample)
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
	ModelName      string `json:"model_name"`
	EnableITN      bool   `json:"enable_itn"`
	EnablePunc     bool   `json:"enable_punc"`
	ShowUtterances bool   `json:"show_utterances"`
	ResultType     string `json:"result_type"`
}

type asrResponse struct {
	Result struct {
		Text       string         `json:"text"`
		Utterances []asrUtterance `json:"utterances"`
	} `json:"result"`
}

type asrUtterance struct {
	Definite  bool   `json:"definite"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	Text      string `json:"text"`
	SpeakerID string `json:"speaker_id"`
}

// mapUtterance 严格验证时间范围，并以服务端 sequence 与条目序号组成稳定 result ID。
func mapUtterance(utterance asrUtterance, localSessionID string, providerSessionID string, responseSequence int32, index int, inputStartSample int64, lastSentSample int64) (port.TranscriptionEvent, error) {
	text := strings.TrimSpace(utterance.Text)
	if text == "" || utterance.StartTime < 0 || utterance.EndTime <= utterance.StartTime {
		return port.TranscriptionEvent{}, fmt.Errorf("火山实时转写 utterance 字段不完整")
	}
	startSample, err := providerMillisecondsToSample(inputStartSample, utterance.StartTime, lastSentSample)
	if err != nil {
		return port.TranscriptionEvent{}, err
	}
	endSample, err := providerMillisecondsToSample(inputStartSample, utterance.EndTime, lastSentSample)
	if err != nil || endSample <= startSample {
		return port.TranscriptionEvent{}, fmt.Errorf("火山实时转写 utterance 样本范围无效")
	}
	providerResultID := fmt.Sprintf("%d:%d", responseSequence, index)
	resultID := fmt.Sprintf("%s:%d", utterance.SpeakerID, utterance.StartTime)
	eventType := port.TranscriptionPartial
	if utterance.Definite {
		eventType = port.TranscriptionFinal
	}
	return port.TranscriptionEvent{Type: eventType, Revision: responseRevision(responseSequence), ResultID: resultID, ProviderResultID: providerResultID, SessionID: localSessionID, ProviderSessionID: providerSessionID, Text: text, SpeakerID: utterance.SpeakerID, SpeakerLabel: utterance.SpeakerID, StartSample: startSample, EndSample: endSample}, nil
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
	if inputStartSample < 0 || providerMS < 0 || lastSentSample < inputStartSample {
		return 0, fmt.Errorf("火山实时转写时间映射参数无效")
	}
	sample := inputStartSample + (providerMS*16000+500)/1000
	if sample < inputStartSample || sample > lastSentSample {
		return 0, fmt.Errorf("火山实时转写时间超出已发送 PCM")
	}
	return sample, nil
}
