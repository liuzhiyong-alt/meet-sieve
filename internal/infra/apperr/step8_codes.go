package apperr

var (
	// CodeFinalizationFailed 表示会议核心收尾尚未完成。
	CodeFinalizationFailed = Code{Value: 500, ErrorCode: "FINALIZATION_FAILED", Message: "会议本地保存尚未完成，请重试", Kind: KindSystem, Retryable: true}
	// CodeGapAudioUnavailable 表示无法取得可信缺口音频。
	CodeGapAudioUnavailable = Code{Value: 500, ErrorCode: "GAP_AUDIO_UNAVAILABLE", Message: "缺口录音暂时不可用，请重试", Kind: KindSystem, Retryable: true}
	// CodeGapAudioInvalid 表示缺口音频完整性校验失败。
	CodeGapAudioInvalid = Code{Value: 500, ErrorCode: "GAP_AUDIO_INVALID", Message: "缺口录音校验失败，原录音已保留", Kind: KindSystem, Retryable: true}
	// CodeGapRequestTooLarge 表示缺口切片仍超过发送上限。
	CodeGapRequestTooLarge = Code{Value: 409, ErrorCode: "GAP_REQUEST_TOO_LARGE", Message: "缺口录音仍超过处理上限", Kind: KindBusiness}
	// CodeGapTranscriptionTimeout 表示文件转写请求超时。
	CodeGapTranscriptionTimeout = Code{Value: 504, ErrorCode: "GAP_TRANSCRIPTION_TIMEOUT", Message: "补转写服务响应超时，请重试", Kind: KindDependency, Retryable: true}
	// CodeGapTranscriptionRejected 表示文件转写请求被 provider 拒绝。
	CodeGapTranscriptionRejected = Code{Value: 502, ErrorCode: "GAP_TRANSCRIPTION_REJECTED", Message: "补转写服务拒绝了本次请求，请检查设置后重试", Kind: KindDependency, Retryable: true}
	// CodeGapTranscriptionNoSpeech 表示文件转写确认范围内没有语音。
	CodeGapTranscriptionNoSpeech = Code{Value: 200, ErrorCode: "GAP_TRANSCRIPTION_NO_SPEECH", Message: "缺口中未识别到语音", Kind: KindBusiness}
	// CodeGapTranscriptionConflict 表示文件候选与当前事实发生重叠。
	CodeGapTranscriptionConflict = Code{Value: 409, ErrorCode: "GAP_TRANSCRIPTION_CONFLICT", Message: "补转写与现有内容冲突，请人工确认", Kind: KindBusiness, Retryable: true}
	// CodeGapTranscriptionCancelled 表示用户停止了文件转写。
	CodeGapTranscriptionCancelled = Code{Value: 409, ErrorCode: "GAP_TRANSCRIPTION_CANCELLED", Message: "补转写已停止，可稍后重试", Kind: KindBusiness, Retryable: true}
	// CodeGapAttemptInterrupted 表示应用退出中断了无法确认远端结果的请求。
	CodeGapAttemptInterrupted = Code{Value: 500, ErrorCode: "GAP_ATTEMPT_INTERRUPTED", Message: "补转写被应用退出中断，请确认后重试", Kind: KindSystem, Retryable: true}
	// CodeAgentFinalSyncFailed 表示 Codex 结束同步失败。
	CodeAgentFinalSyncFailed = Code{Value: 502, ErrorCode: "AGENT_FINAL_SYNC_FAILED", Message: "会议已保存，但 Codex 结束同步未完成", Kind: KindDependency, Retryable: true}
	// CodeMinutesGapProcessing 表示补转写运行中，暂不能生成纪要。
	CodeMinutesGapProcessing = Code{Value: 409, ErrorCode: "MINUTES_GAP_PROCESSING", Message: "补转写正在处理，请等待完成或先停止", Kind: KindBusiness, Retryable: true}
	// CodeMinutesBusy 表示已有纪要或 Agent turn 正在运行。
	CodeMinutesBusy = Code{Value: 409, ErrorCode: "MINUTES_BUSY", Message: "AI 正在处理其他任务，请稍后重试", Kind: KindBusiness, Retryable: true}
	// CodeMinutesOutputInvalid 表示纪要结构化输出未通过本地校验。
	CodeMinutesOutputInvalid = Code{Value: 502, ErrorCode: "MINUTES_OUTPUT_INVALID", Message: "纪要结果未通过校验，旧版本已保留", Kind: KindDependency, Retryable: true}
	// CodeMinutesVersionConflict 表示纪要版本在提交前已经变化。
	CodeMinutesVersionConflict = Code{Value: 409, ErrorCode: "MINUTES_VERSION_CONFLICT", Message: "纪要版本已变化，请刷新后重试", Kind: KindBusiness, Retryable: true}
	// CodeMinutesProjectionFailed 表示纪要版本已保存但 Markdown 投影失败。
	CodeMinutesProjectionFailed = Code{Value: 500, ErrorCode: "MINUTES_PROJECTION_FAILED", Message: "纪要版本已保存，但文件尚未刷新", Kind: KindSystem, Retryable: true}
)
