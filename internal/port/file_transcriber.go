package port

import "context"

// FileTranscriptionRequest 描述一次已切片、可审计的会后文件识别请求。
type FileTranscriptionRequest struct {
	MeetingID        string
	RequestID        string
	AudioPath        string
	AudioSHA256      string
	CoreStartSample  int64
	CoreEndSample    int64
	AudioStartSample int64
	AudioEndSample   int64
	SampleRate       int
}

// FileTranscriptionSegment 是文件转写返回的单段结果。
type FileTranscriptionSegment struct {
	Text        string
	SpeakerID   string
	StartSample int64
	EndSample   int64
}

// FileTranscriptionResult 保存一次文件转写的规范化结果，不暴露厂商协议字段。
type FileTranscriptionResult struct {
	ProviderLogIDSuffix string
	NoSpeech            bool
	Segments            []FileTranscriptionSegment
}

// FileTranscriber 提供会后音频文件补转写能力。
type FileTranscriber interface {
	Transcribe(ctx context.Context, request FileTranscriptionRequest) (FileTranscriptionResult, error)
}
