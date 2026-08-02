package port

import "context"

// FileTranscriptionRequest 描述会后缺口音频转写请求。
type FileTranscriptionRequest struct {
	MeetingID   string
	AudioPath   string
	StartSample int64
	EndSample   int64
}

// FileTranscriptionSegment 是文件转写返回的单段结果。
type FileTranscriptionSegment struct {
	Text        string
	SpeakerID   string
	StartSample int64
	EndSample   int64
}

// FileTranscriptionResult 保存一次文件转写的完整结果。
type FileTranscriptionResult struct {
	Segments []FileTranscriptionSegment
}

// FileTranscriber 提供会后音频文件补转写能力。
type FileTranscriber interface {
	Transcribe(ctx context.Context, request FileTranscriptionRequest) (FileTranscriptionResult, error)
}
