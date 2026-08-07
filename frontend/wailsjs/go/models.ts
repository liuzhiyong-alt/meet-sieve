export namespace buildinfo {

	export class Info {
	    version: string;
	    commit: string;
	    buildTime: string;
	    buildMode: string;

	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.commit = source["commit"];
	        this.buildTime = source["buildTime"];
	        this.buildMode = source["buildMode"];
	    }
	}

}

export namespace diagnostics {

	export class MeetingStorage {
	    MeetingID: string;
	    MeetingNo: string;
	    Subject: string;
	    Bytes: number;

	    static createFrom(source: any = {}) {
	        return new MeetingStorage(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.MeetingID = source["MeetingID"];
	        this.MeetingNo = source["MeetingNo"];
	        this.Subject = source["Subject"];
	        this.Bytes = source["Bytes"];
	    }
	}
	export class StorageCategories {
	    Recordings: number;
	    Attachments: number;
	    DatabaseBackups: number;
	    DerivedTemp: number;
	    Logs: number;
	    VoiceModels: number;

	    static createFrom(source: any = {}) {
	        return new StorageCategories(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Recordings = source["Recordings"];
	        this.Attachments = source["Attachments"];
	        this.DatabaseBackups = source["DatabaseBackups"];
	        this.DerivedTemp = source["DerivedTemp"];
	        this.Logs = source["Logs"];
	        this.VoiceModels = source["VoiceModels"];
	    }
	}

}

export namespace health {

	export class Snapshot {
	    status: string;
	    errorCode?: number;
	    message?: string;

	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	    }
	}

}

export namespace voice {

	export class RebuildProgress {
	    Total: number;
	    Completed: number;
	    Failed: number;

	    static createFrom(source: any = {}) {
	        return new RebuildProgress(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Total = source["Total"];
	        this.Completed = source["Completed"];
	        this.Failed = source["Failed"];
	    }
	}

}

export namespace wails {

	export class ASRConnectionProbeDTO {
	    connection_established: boolean;
	    real_audio_verified: boolean;
	    latency_ms: number;

	    static createFrom(source: any = {}) {
	        return new ASRConnectionProbeDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connection_established = source["connection_established"];
	        this.real_audio_verified = source["real_audio_verified"];
	        this.latency_ms = source["latency_ms"];
	    }
	}
	export class ASRSettingsDTO {
	    api_key_configured: boolean;
	    api_key_mask: string;
	    requires_api_key_upgrade: boolean;
	    updated_at: number;

	    static createFrom(source: any = {}) {
	        return new ASRSettingsDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.api_key_configured = source["api_key_configured"];
	        this.api_key_mask = source["api_key_mask"];
	        this.requires_api_key_upgrade = source["requires_api_key_upgrade"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class ASRTimelineEntryDTO {
	    seq: number;
	    kind: string;
	    occurred_at: number;
	    start_sample: number;
	    end_sample: number;
	    text?: string;
	    speaker_label?: string;
	    session_order?: number;
	    gap_reason?: string;

	    static createFrom(source: any = {}) {
	        return new ASRTimelineEntryDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.kind = source["kind"];
	        this.occurred_at = source["occurred_at"];
	        this.start_sample = source["start_sample"];
	        this.end_sample = source["end_sample"];
	        this.text = source["text"];
	        this.speaker_label = source["speaker_label"];
	        this.session_order = source["session_order"];
	        this.gap_reason = source["gap_reason"];
	    }
	}
	export class MeetingProjectionDTO {
	    id: string;
	    meeting_no: string;
	    subject: string;
	    lifecycle_state: string;
	    local_save_state: string;
	    realtime_asr_state: string;
	    agent_state: string;
	    started_at?: number;
	    ended_at?: number;
	    updated_at: number;

	    static createFrom(source: any = {}) {
	        return new MeetingProjectionDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.meeting_no = source["meeting_no"];
	        this.subject = source["subject"];
	        this.lifecycle_state = source["lifecycle_state"];
	        this.local_save_state = source["local_save_state"];
	        this.realtime_asr_state = source["realtime_asr_state"];
	        this.agent_state = source["agent_state"];
	        this.started_at = source["started_at"];
	        this.ended_at = source["ended_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class ActiveMeetingDTO {
	    active: boolean;
	    meeting?: MeetingProjectionDTO;

	    static createFrom(source: any = {}) {
	        return new ActiveMeetingDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.meeting = this.convertValues(source["meeting"], MeetingProjectionDTO);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AgentApprovalDTO {
	    id: string;
	    tool: string;
	    target: string;
	    parameter_summary: string;
	    risk: string;

	    static createFrom(source: any = {}) {
	        return new AgentApprovalDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.tool = source["tool"];
	        this.target = source["target"];
	        this.parameter_summary = source["parameter_summary"];
	        this.risk = source["risk"];
	    }
	}
	export class AgentAskDTO {
	    turn_id: string;
	    question_seq: number;
	    answer?: string;
	    answer_seq?: number;

	    static createFrom(source: any = {}) {
	        return new AgentAskDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.turn_id = source["turn_id"];
	        this.question_seq = source["question_seq"];
	        this.answer = source["answer"];
	        this.answer_seq = source["answer_seq"];
	    }
	}
	export class AgentAvailabilityDTO {
	    state: string;
	    version?: string;
	    account_state: string;
	    protocol_state: string;
	    message: string;

	    static createFrom(source: any = {}) {
	        return new AgentAvailabilityDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.version = source["version"];
	        this.account_state = source["account_state"];
	        this.protocol_state = source["protocol_state"];
	        this.message = source["message"];
	    }
	}
	export class AgentRecoveryCommandsDTO {
	    thread_available: boolean;
	    thread_command: string;
	    directory_command: string;
	    recovery_prompt: string;

	    static createFrom(source: any = {}) {
	        return new AgentRecoveryCommandsDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.thread_available = source["thread_available"];
	        this.thread_command = source["thread_command"];
	        this.directory_command = source["directory_command"];
	        this.recovery_prompt = source["recovery_prompt"];
	    }
	}
	export class AgentSettingsDTO {
	    wake_word: string;
	    codex_executable_path: string;
	    codex_proxy_port: number;
	    availability: AgentAvailabilityDTO;
	    probed_at: number;
	    updated_at: number;

	    static createFrom(source: any = {}) {
	        return new AgentSettingsDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.wake_word = source["wake_word"];
	        this.codex_executable_path = source["codex_executable_path"];
	        this.codex_proxy_port = source["codex_proxy_port"];
	        this.availability = this.convertValues(source["availability"], AgentAvailabilityDTO);
	        this.probed_at = source["probed_at"];
	        this.updated_at = source["updated_at"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AgentStateDTO {
	    state: string;
	    meeting_id: string;
	    turn_id?: string;
	    partial?: string;
	    approval?: AgentApprovalDTO;
	    error_code?: string;
	    revision: number;

	    static createFrom(source: any = {}) {
	        return new AgentStateDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.meeting_id = source["meeting_id"];
	        this.turn_id = source["turn_id"];
	        this.partial = source["partial"];
	        this.approval = this.convertValues(source["approval"], AgentApprovalDTO);
	        this.error_code = source["error_code"];
	        this.revision = source["revision"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AgentTimelineEntryDTO {
	    seq: number;
	    kind: string;
	    occurred_at: number;
	    turn_id: string;
	    text?: string;
	    reason?: string;

	    static createFrom(source: any = {}) {
	        return new AgentTimelineEntryDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.kind = source["kind"];
	        this.occurred_at = source["occurred_at"];
	        this.turn_id = source["turn_id"];
	        this.text = source["text"];
	        this.reason = source["reason"];
	    }
	}
	export class AppEvent_string_ {
	    name: string;
	    version: number;
	    occurredAt: number;
	    sequence?: number;
	    data: string;

	    static createFrom(source: any = {}) {
	        return new AppEvent_string_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.occurredAt = source["occurredAt"];
	        this.sequence = source["sequence"];
	        this.data = source["data"];
	    }
	}
	export class AttachmentSendDTO {
	    cancelled: boolean;
	    request_id?: string;
	    resource_id?: string;
	    seq?: number;
	    occurred_at?: number;
	    original_name?: string;
	    media_type?: string;
	    size_bytes?: number;
	    sha256?: string;

	    static createFrom(source: any = {}) {
	        return new AttachmentSendDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cancelled = source["cancelled"];
	        this.request_id = source["request_id"];
	        this.resource_id = source["resource_id"];
	        this.seq = source["seq"];
	        this.occurred_at = source["occurred_at"];
	        this.original_name = source["original_name"];
	        this.media_type = source["media_type"];
	        this.size_bytes = source["size_bytes"];
	        this.sha256 = source["sha256"];
	    }
	}
	export class AudioClipDTO {
	    url: string;
	    start_sample: number;
	    end_sample: number;
	    expires_at: number;

	    static createFrom(source: any = {}) {
	        return new AudioClipDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.start_sample = source["start_sample"];
	        this.end_sample = source["end_sample"];
	        this.expires_at = source["expires_at"];
	    }
	}
	export class InputDeviceDTO {
	    id: string;
	    name: string;
	    is_default: boolean;
	    channel_count: number;

	    static createFrom(source: any = {}) {
	        return new InputDeviceDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.is_default = source["is_default"];
	        this.channel_count = source["channel_count"];
	    }
	}
	export class AudioSettingsDTO {
	    default_microphone_id: string;
	    revision: number;
	    devices: InputDeviceDTO[];

	    static createFrom(source: any = {}) {
	        return new AudioSettingsDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.default_microphone_id = source["default_microphone_id"];
	        this.revision = source["revision"];
	        this.devices = this.convertValues(source["devices"], InputDeviceDTO);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BootstrapStateDTO {
	    phase: string;
	    reason: string;
	    message: string;
	    retryable: boolean;
	    available_actions: string[];

	    static createFrom(source: any = {}) {
	        return new BootstrapStateDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.reason = source["reason"];
	        this.message = source["message"];
	        this.retryable = source["retryable"];
	        this.available_actions = source["available_actions"];
	    }
	}
	export class CancelUploadDTO {
	    cancelled: boolean;

	    static createFrom(source: any = {}) {
	        return new CancelUploadDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cancelled = source["cancelled"];
	    }
	}
	export class ChooseAttachmentDTO {
	    meeting_id: string;

	    static createFrom(source: any = {}) {
	        return new ChooseAttachmentDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.meeting_id = source["meeting_id"];
	    }
	}
	export class ClusterCorrectionDTO {
	    request_id: string;
	    meeting_id: string;
	    cluster_id: string;
	    participant_id: string;
	    expected_revision: number;
	    expected_count: number;
	    reason?: string;

	    static createFrom(source: any = {}) {
	        return new ClusterCorrectionDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.request_id = source["request_id"];
	        this.meeting_id = source["meeting_id"];
	        this.cluster_id = source["cluster_id"];
	        this.participant_id = source["participant_id"];
	        this.expected_revision = source["expected_revision"];
	        this.expected_count = source["expected_count"];
	        this.reason = source["reason"];
	    }
	}
	export class CommandResultDTO {
	    executed: boolean;

	    static createFrom(source: any = {}) {
	        return new CommandResultDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.executed = source["executed"];
	    }
	}
	export class ContentItemDTO {
	    seq: number;
	    kind: string;
	    occurred_at: number;
	    entity_id: string;
	    display_name?: string;
	    text?: string;
	    resource_kind?: string;
	    resource_name?: string;
	    resource_state?: string;
	    hostname?: string;
	    display_url?: string;

	    static createFrom(source: any = {}) {
	        return new ContentItemDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.kind = source["kind"];
	        this.occurred_at = source["occurred_at"];
	        this.entity_id = source["entity_id"];
	        this.display_name = source["display_name"];
	        this.text = source["text"];
	        this.resource_kind = source["resource_kind"];
	        this.resource_name = source["resource_name"];
	        this.resource_state = source["resource_state"];
	        this.hostname = source["hostname"];
	        this.display_url = source["display_url"];
	    }
	}
	export class ContentPageDTO {
	    items: ContentItemDTO[];
	    has_more: boolean;
	    has_previous: boolean;
	    has_next: boolean;
	    after_seq?: number;
	    before_seq?: number;

	    static createFrom(source: any = {}) {
	        return new ContentPageDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], ContentItemDTO);
	        this.has_more = source["has_more"];
	        this.has_previous = source["has_previous"];
	        this.has_next = source["has_next"];
	        this.after_seq = source["after_seq"];
	        this.before_seq = source["before_seq"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CorrectionCommandDTO {
	    request_id: string;
	    meeting_id: string;
	    utterance_id: string;
	    expected_revision: number;
	    value: string;
	    reason?: string;

	    static createFrom(source: any = {}) {
	        return new CorrectionCommandDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.request_id = source["request_id"];
	        this.meeting_id = source["meeting_id"];
	        this.utterance_id = source["utterance_id"];
	        this.expected_revision = source["expected_revision"];
	        this.value = source["value"];
	        this.reason = source["reason"];
	    }
	}
	export class CorrectionEntryDTO {
	    seq: number;
	    utterance_id: string;
	    start_sample: number;
	    end_sample: number;
	    original_text: string;
	    current_text: string;
	    speaker_display: string;
	    current_participant_id?: string;
	    speaker_cluster_id?: string;
	    cluster_display_no?: number;
	    cluster_participant_id?: string;
	    assignment_source: string;
	    text_revision: number;
	    speaker_revision: number;
	    cluster_revision?: number;
	    cluster_count?: number;
	    can_play: boolean;
	    playback_disabled_reason?: string;
	    can_enroll: boolean;
	    enrollment_disabled_reason?: string;

	    static createFrom(source: any = {}) {
	        return new CorrectionEntryDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.utterance_id = source["utterance_id"];
	        this.start_sample = source["start_sample"];
	        this.end_sample = source["end_sample"];
	        this.original_text = source["original_text"];
	        this.current_text = source["current_text"];
	        this.speaker_display = source["speaker_display"];
	        this.current_participant_id = source["current_participant_id"];
	        this.speaker_cluster_id = source["speaker_cluster_id"];
	        this.cluster_display_no = source["cluster_display_no"];
	        this.cluster_participant_id = source["cluster_participant_id"];
	        this.assignment_source = source["assignment_source"];
	        this.text_revision = source["text_revision"];
	        this.speaker_revision = source["speaker_revision"];
	        this.cluster_revision = source["cluster_revision"];
	        this.cluster_count = source["cluster_count"];
	        this.can_play = source["can_play"];
	        this.playback_disabled_reason = source["playback_disabled_reason"];
	        this.can_enroll = source["can_enroll"];
	        this.enrollment_disabled_reason = source["enrollment_disabled_reason"];
	    }
	}
	export class CorrectionParticipantDTO {
	    id: string;
	    display_name: string;
	    kind: string;
	    is_member: boolean;

	    static createFrom(source: any = {}) {
	        return new CorrectionParticipantDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.display_name = source["display_name"];
	        this.kind = source["kind"];
	        this.is_member = source["is_member"];
	    }
	}
	export class CorrectionPageDTO {
	    entries: CorrectionEntryDTO[];
	    participants: CorrectionParticipantDTO[];
	    next_seq: number;

	    static createFrom(source: any = {}) {
	        return new CorrectionPageDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entries = this.convertValues(source["entries"], CorrectionEntryDTO);
	        this.participants = this.convertValues(source["participants"], CorrectionParticipantDTO);
	        this.next_seq = source["next_seq"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class CorrectionResultDTO {
	    correction_id?: string;
	    result_revision: number;
	    saved: boolean;
	    duplicate: boolean;
	    no_op: boolean;
	    impacted_count: number;
	    projection_state: string;
	    projection_error_code?: string;

	    static createFrom(source: any = {}) {
	        return new CorrectionResultDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.correction_id = source["correction_id"];
	        this.result_revision = source["result_revision"];
	        this.saved = source["saved"];
	        this.duplicate = source["duplicate"];
	        this.no_op = source["no_op"];
	        this.impacted_count = source["impacted_count"];
	        this.projection_state = source["projection_state"];
	        this.projection_error_code = source["projection_error_code"];
	    }
	}
	export class CreateGroupDTO {
	    name: string;
	    default_lan_enabled: boolean;
	    member_ids: string[];

	    static createFrom(source: any = {}) {
	        return new CreateGroupDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.default_lan_enabled = source["default_lan_enabled"];
	        this.member_ids = source["member_ids"];
	    }
	}
	export class CreateMemberDTO {
	    name: string;
	    notes: string;

	    static createFrom(source: any = {}) {
	        return new CreateMemberDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.notes = source["notes"];
	    }
	}
	export class CredentialChangeDTO {
	    action: string;
	    value: string;

	    static createFrom(source: any = {}) {
	        return new CredentialChangeDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.value = source["value"];
	    }
	}
	export class DeleteMeetingDTO {
	    meeting_id: string;
	    meeting_no: string;
	    revision: number;
	    digest: string;

	    static createFrom(source: any = {}) {
	        return new DeleteMeetingDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.meeting_id = source["meeting_id"];
	        this.meeting_no = source["meeting_no"];
	        this.revision = source["revision"];
	        this.digest = source["digest"];
	    }
	}
	export class DeletionFailedItemDTO {
	    item_id: string;
	    safe_name: string;
	    code: string;

	    static createFrom(source: any = {}) {
	        return new DeletionFailedItemDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.item_id = source["item_id"];
	        this.safe_name = source["safe_name"];
	        this.code = source["code"];
	    }
	}
	export class DeletionJobDTO {
	    job_id: string;
	    meeting_id: string;
	    kind: string;
	    state: string;
	    remaining: DeletionFailedItemDTO[];
	    attempt_count: number;
	    last_error_code?: string;

	    static createFrom(source: any = {}) {
	        return new DeletionJobDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.job_id = source["job_id"];
	        this.meeting_id = source["meeting_id"];
	        this.kind = source["kind"];
	        this.state = source["state"];
	        this.remaining = this.convertValues(source["remaining"], DeletionFailedItemDTO);
	        this.attempt_count = source["attempt_count"];
	        this.last_error_code = source["last_error_code"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DeletionPreviewDTO {
	    meeting_id: string;
	    meeting_no: string;
	    kind: string;
	    revision: number;
	    digest: string;
	    file_count: number;
	    directory_count: number;
	    symlink_count: number;
	    unknown_count: number;
	    size_bytes: number;

	    static createFrom(source: any = {}) {
	        return new DeletionPreviewDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.meeting_id = source["meeting_id"];
	        this.meeting_no = source["meeting_no"];
	        this.kind = source["kind"];
	        this.revision = source["revision"];
	        this.digest = source["digest"];
	        this.file_count = source["file_count"];
	        this.directory_count = source["directory_count"];
	        this.symlink_count = source["symlink_count"];
	        this.unknown_count = source["unknown_count"];
	        this.size_bytes = source["size_bytes"];
	    }
	}
	export class DiagnosticExportDTO {
	    file_name: string;
	    size_bytes: number;
	    sha256: string;
	    entries: string[];
	    cancelled: boolean;

	    static createFrom(source: any = {}) {
	        return new DiagnosticExportDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file_name = source["file_name"];
	        this.size_bytes = source["size_bytes"];
	        this.sha256 = source["sha256"];
	        this.entries = source["entries"];
	        this.cancelled = source["cancelled"];
	    }
	}
	export class FinalizationStateDTO {
	    meeting_id: string;
	    state: string;
	    stage: string;
	    error_code?: string;
	    revision: number;

	    static createFrom(source: any = {}) {
	        return new FinalizationStateDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.meeting_id = source["meeting_id"];
	        this.state = source["state"];
	        this.stage = source["stage"];
	        this.error_code = source["error_code"];
	        this.revision = source["revision"];
	    }
	}
	export class GapCandidateDTO {
	    text: string;
	    speaker_id?: string;
	    start_sample: number;
	    end_sample: number;

	    static createFrom(source: any = {}) {
	        return new GapCandidateDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.speaker_id = source["speaker_id"];
	        this.start_sample = source["start_sample"];
	        this.end_sample = source["end_sample"];
	    }
	}
	export class GapCommandDTO {
	    accepted: boolean;

	    static createFrom(source: any = {}) {
	        return new GapCommandDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.accepted = source["accepted"];
	    }
	}
	export class GapConflictUtteranceDTO {
	    id: string;
	    seq: number;
	    original_text: string;
	    current_text: string;
	    start_sample: number;
	    end_sample: number;
	    text_revision: number;

	    static createFrom(source: any = {}) {
	        return new GapConflictUtteranceDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.seq = source["seq"];
	        this.original_text = source["original_text"];
	        this.current_text = source["current_text"];
	        this.start_sample = source["start_sample"];
	        this.end_sample = source["end_sample"];
	        this.text_revision = source["text_revision"];
	    }
	}
	export class GapConflictDTO {
	    gap_id: string;
	    revision: number;
	    core_start_sample: number;
	    core_end_sample: number;
	    audio_start_sample: number;
	    audio_end_sample: number;
	    audio_clip_url: string;
	    audio_clip_expires_at: number;
	    candidates: GapCandidateDTO[];
	    existing: GapConflictUtteranceDTO[];
	    context: GapConflictUtteranceDTO[];

	    static createFrom(source: any = {}) {
	        return new GapConflictDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.gap_id = source["gap_id"];
	        this.revision = source["revision"];
	        this.core_start_sample = source["core_start_sample"];
	        this.core_end_sample = source["core_end_sample"];
	        this.audio_start_sample = source["audio_start_sample"];
	        this.audio_end_sample = source["audio_end_sample"];
	        this.audio_clip_url = source["audio_clip_url"];
	        this.audio_clip_expires_at = source["audio_clip_expires_at"];
	        this.candidates = this.convertValues(source["candidates"], GapCandidateDTO);
	        this.existing = this.convertValues(source["existing"], GapConflictUtteranceDTO);
	        this.context = this.convertValues(source["context"], GapConflictUtteranceDTO);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class GapItemDTO {
	    id: string;
	    start_sample: number;
	    end_sample: number;
	    state: string;
	    attempt_count: number;
	    error_code?: string;

	    static createFrom(source: any = {}) {
	        return new GapItemDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.start_sample = source["start_sample"];
	        this.end_sample = source["end_sample"];
	        this.state = source["state"];
	        this.attempt_count = source["attempt_count"];
	        this.error_code = source["error_code"];
	    }
	}
	export class GapResolutionEditDTO {
	    target_id: string;
	    expected_revision: number;
	    text: string;

	    static createFrom(source: any = {}) {
	        return new GapResolutionEditDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.target_id = source["target_id"];
	        this.expected_revision = source["expected_revision"];
	        this.text = source["text"];
	    }
	}
	export class GapStateDTO {
	    meeting_id: string;
	    state: string;
	    gaps: GapItemDTO[];
	    active_attempt_id?: string;
	    revision: number;

	    static createFrom(source: any = {}) {
	        return new GapStateDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.meeting_id = source["meeting_id"];
	        this.state = source["state"];
	        this.gaps = this.convertValues(source["gaps"], GapItemDTO);
	        this.active_attempt_id = source["active_attempt_id"];
	        this.revision = source["revision"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GroupMemberDTO {
	    member_id: string;
	    sort_order: number;

	    static createFrom(source: any = {}) {
	        return new GroupMemberDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.member_id = source["member_id"];
	        this.sort_order = source["sort_order"];
	    }
	}
	export class GroupDTO {
	    id: string;
	    name: string;
	    default_lan_enabled: boolean;
	    members: GroupMemberDTO[];
	    created_at: number;
	    updated_at: number;

	    static createFrom(source: any = {}) {
	        return new GroupDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.default_lan_enabled = source["default_lan_enabled"];
	        this.members = this.convertValues(source["members"], GroupMemberDTO);
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class MeetingPrimaryActionDTO {
	    kind: string;
	    label: string;
	    target_id?: string;
	    enabled: boolean;
	    disabled_reason?: string;

	    static createFrom(source: any = {}) {
	        return new MeetingPrimaryActionDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.label = source["label"];
	        this.target_id = source["target_id"];
	        this.enabled = source["enabled"];
	        this.disabled_reason = source["disabled_reason"];
	    }
	}
	export class MeetingSummaryDTO {
	    id: string;
	    meeting_no: string;
	    subject: string;
	    started_at: number;
	    ended_at?: number;
	    lifecycle_state: string;
	    local_save_state: string;
	    realtime_asr_state: string;
	    gap_state: string;
	    agent_state: string;
	    minute_state: string;
	    lan_state: string;
	    participants: string[];
	    participant_member_ids: string[];
	    highest_status: string;
	    primary_action: MeetingPrimaryActionDTO;
	    can_delete_meeting: boolean;
	    delete_disabled_reason?: string;

	    static createFrom(source: any = {}) {
	        return new MeetingSummaryDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.meeting_no = source["meeting_no"];
	        this.subject = source["subject"];
	        this.started_at = source["started_at"];
	        this.ended_at = source["ended_at"];
	        this.lifecycle_state = source["lifecycle_state"];
	        this.local_save_state = source["local_save_state"];
	        this.realtime_asr_state = source["realtime_asr_state"];
	        this.gap_state = source["gap_state"];
	        this.agent_state = source["agent_state"];
	        this.minute_state = source["minute_state"];
	        this.lan_state = source["lan_state"];
	        this.participants = source["participants"];
	        this.participant_member_ids = source["participant_member_ids"];
	        this.highest_status = source["highest_status"];
	        this.primary_action = this.convertValues(source["primary_action"], MeetingPrimaryActionDTO);
	        this.can_delete_meeting = source["can_delete_meeting"];
	        this.delete_disabled_reason = source["delete_disabled_reason"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class HomeDTO {
	    continuation?: MeetingSummaryDTO;
	    remaining: number;
	    recent_meetings: MeetingSummaryDTO[];

	    static createFrom(source: any = {}) {
	        return new HomeDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.continuation = this.convertValues(source["continuation"], MeetingSummaryDTO);
	        this.remaining = source["remaining"];
	        this.recent_meetings = this.convertValues(source["recent_meetings"], MeetingSummaryDTO);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class InterruptedRecoveryDTO {
	    meeting: MeetingSummaryDTO;
	    can_retry: boolean;
	    disabled_reason?: string;
	    segment_count: number;
	    duration_samples: number;
	    sample_rate: number;
	    first_sequence: number;
	    last_sequence: number;
	    gap_count: number;
	    pending_gap_count: number;
	    ready_file_count: number;
	    failed_file_count: number;
	    deleted_file_count: number;
	    failure_stage?: string;

	    static createFrom(source: any = {}) {
	        return new InterruptedRecoveryDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.meeting = this.convertValues(source["meeting"], MeetingSummaryDTO);
	        this.can_retry = source["can_retry"];
	        this.disabled_reason = source["disabled_reason"];
	        this.segment_count = source["segment_count"];
	        this.duration_samples = source["duration_samples"];
	        this.sample_rate = source["sample_rate"];
	        this.first_sequence = source["first_sequence"];
	        this.last_sequence = source["last_sequence"];
	        this.gap_count = source["gap_count"];
	        this.pending_gap_count = source["pending_gap_count"];
	        this.ready_file_count = source["ready_file_count"];
	        this.failed_file_count = source["failed_file_count"];
	        this.deleted_file_count = source["deleted_file_count"];
	        this.failure_stage = source["failure_stage"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LANInterfaceDTO {
	    id: string;
	    name: string;
	    address: string;
	    default_route: boolean;

	    static createFrom(source: any = {}) {
	        return new LANInterfaceDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.address = source["address"];
	        this.default_route = source["default_route"];
	    }
	}
	export class LANInterfaceListDTO {
	    interfaces: LANInterfaceDTO[];
	    recommended_id?: string;
	    reason: string;
	    warning?: string;

	    static createFrom(source: any = {}) {
	        return new LANInterfaceListDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.interfaces = this.convertValues(source["interfaces"], LANInterfaceDTO);
	        this.recommended_id = source["recommended_id"];
	        this.reason = source["reason"];
	        this.warning = source["warning"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LANUploadDTO {
	    request_id: string;
	    name: string;
	    written: number;
	    total: number;

	    static createFrom(source: any = {}) {
	        return new LANUploadDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.request_id = source["request_id"];
	        this.name = source["name"];
	        this.written = source["written"];
	        this.total = source["total"];
	    }
	}
	export class LANStatusDTO {
	    state: string;
	    meeting_id?: string;
	    interface_id?: string;
	    address?: string;
	    join_url?: string;
	    error_code?: string;
	    online_count: number;
	    active_uploads: LANUploadDTO[];

	    static createFrom(source: any = {}) {
	        return new LANStatusDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.meeting_id = source["meeting_id"];
	        this.interface_id = source["interface_id"];
	        this.address = source["address"];
	        this.join_url = source["join_url"];
	        this.error_code = source["error_code"];
	        this.online_count = source["online_count"];
	        this.active_uploads = this.convertValues(source["active_uploads"], LANUploadDTO);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class LiveMeetingStatusDTO {
	    started_at?: number;
	    ended_at?: number;
	    recording_state: string;
	    microphone_state: string;
	    local_save_state: string;
	    realtime_asr_state: string;
	    latest_final_at?: number;
	    agent_state: string;
	    lan_state: string;
	    online_count: number;

	    static createFrom(source: any = {}) {
	        return new LiveMeetingStatusDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.started_at = source["started_at"];
	        this.ended_at = source["ended_at"];
	        this.recording_state = source["recording_state"];
	        this.microphone_state = source["microphone_state"];
	        this.local_save_state = source["local_save_state"];
	        this.realtime_asr_state = source["realtime_asr_state"];
	        this.latest_final_at = source["latest_final_at"];
	        this.agent_state = source["agent_state"];
	        this.lan_state = source["lan_state"];
	        this.online_count = source["online_count"];
	    }
	}
	export class MeetingClipDTO {
	    request_id: string;
	    meeting_id: string;
	    utterance_id: string;
	    environment_kind: string;
	    confirmed: boolean;

	    static createFrom(source: any = {}) {
	        return new MeetingClipDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.request_id = source["request_id"];
	        this.meeting_id = source["meeting_id"];
	        this.utterance_id = source["utterance_id"];
	        this.environment_kind = source["environment_kind"];
	        this.confirmed = source["confirmed"];
	    }
	}
	export class MeetingCreateDraftDTO {
	    suggested_meeting_no: string;
	    default_subject: string;

	    static createFrom(source: any = {}) {
	        return new MeetingCreateDraftDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.suggested_meeting_no = source["suggested_meeting_no"];
	        this.default_subject = source["default_subject"];
	    }
	}
	export class MeetingDetailDTO {
	    summary: MeetingSummaryDTO;
	    can_play_audio: boolean;
	    can_retranscribe: boolean;
	    can_delete_meeting: boolean;
	    disabled_reason?: string;

	    static createFrom(source: any = {}) {
	        return new MeetingDetailDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.summary = this.convertValues(source["summary"], MeetingSummaryDTO);
	        this.can_play_audio = source["can_play_audio"];
	        this.can_retranscribe = source["can_retranscribe"];
	        this.can_delete_meeting = source["can_delete_meeting"];
	        this.disabled_reason = source["disabled_reason"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MeetingMemberOptionDTO {
	    id: string;
	    name: string;
	    sort_order: number;
	    voice_readiness: string;

	    static createFrom(source: any = {}) {
	        return new MeetingMemberOptionDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.sort_order = source["sort_order"];
	        this.voice_readiness = source["voice_readiness"];
	    }
	}
	export class MeetingGroupOptionDTO {
	    id: string;
	    name: string;
	    default_lan_enabled: boolean;
	    members: MeetingMemberOptionDTO[];

	    static createFrom(source: any = {}) {
	        return new MeetingGroupOptionDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.default_lan_enabled = source["default_lan_enabled"];
	        this.members = this.convertValues(source["members"], MeetingMemberOptionDTO);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MeetingListInputDTO {
	    search: string;
	    status: string;
	    cursor: string;
	    limit: number;

	    static createFrom(source: any = {}) {
	        return new MeetingListInputDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.search = source["search"];
	        this.status = source["status"];
	        this.cursor = source["cursor"];
	        this.limit = source["limit"];
	    }
	}

	export class MeetingPageDTO {
	    items: MeetingSummaryDTO[];
	    next_cursor?: string;
	    previous_cursor?: string;

	    static createFrom(source: any = {}) {
	        return new MeetingPageDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], MeetingSummaryDTO);
	        this.next_cursor = source["next_cursor"];
	        this.previous_cursor = source["previous_cursor"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MeetingPeopleOptionsDTO {
	    groups: MeetingGroupOptionDTO[];
	    members: MeetingMemberOptionDTO[];

	    static createFrom(source: any = {}) {
	        return new MeetingPeopleOptionsDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.groups = this.convertValues(source["groups"], MeetingGroupOptionDTO);
	        this.members = this.convertValues(source["members"], MeetingMemberOptionDTO);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}


	export class MeetingStateEventDTO {
	    meeting_id: string;
	    state: string;

	    static createFrom(source: any = {}) {
	        return new MeetingStateEventDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.meeting_id = source["meeting_id"];
	        this.state = source["state"];
	    }
	}

	export class MemberDTO {
	    id: string;
	    name: string;
	    notes?: string;
	    accepted_sample_count: number;
	    rejected_sample_count: number;
	    voice_readiness: string;
	    created_at: number;
	    updated_at: number;
	    archived_at?: number;

	    static createFrom(source: any = {}) {
	        return new MemberDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.notes = source["notes"];
	        this.accepted_sample_count = source["accepted_sample_count"];
	        this.rejected_sample_count = source["rejected_sample_count"];
	        this.voice_readiness = source["voice_readiness"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.archived_at = source["archived_at"];
	    }
	}
	export class MemberDetailDTO {
	    member: MemberDTO;
	    revision: number;
	    group_count: number;
	    historical_meetings: number;
	    can_archive: boolean;
	    can_restore: boolean;
	    can_delete: boolean;

	    static createFrom(source: any = {}) {
	        return new MemberDetailDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.member = this.convertValues(source["member"], MemberDTO);
	        this.revision = source["revision"];
	        this.group_count = source["group_count"];
	        this.historical_meetings = source["historical_meetings"];
	        this.can_archive = source["can_archive"];
	        this.can_restore = source["can_restore"];
	        this.can_delete = source["can_delete"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MinuteVersionDTO {
	    id: string;
	    version_no: number;
	    source: string;
	    content_markdown: string;
	    state: string;
	    is_current: boolean;
	    confirmed_at?: number;
	    created_at: number;

	    static createFrom(source: any = {}) {
	        return new MinuteVersionDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.version_no = source["version_no"];
	        this.source = source["source"];
	        this.content_markdown = source["content_markdown"];
	        this.state = source["state"];
	        this.is_current = source["is_current"];
	        this.confirmed_at = source["confirmed_at"];
	        this.created_at = source["created_at"];
	    }
	}
	export class MinuteMutationDTO {
	    version: MinuteVersionDTO;
	    projection_state: string;
	    projection_error?: string;

	    static createFrom(source: any = {}) {
	        return new MinuteMutationDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = this.convertValues(source["version"], MinuteVersionDTO);
	        this.projection_state = source["projection_state"];
	        this.projection_error = source["projection_error"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class MinuteVersionPageDTO {
	    items: MinuteVersionDTO[];
	    next_cursor?: number;

	    static createFrom(source: any = {}) {
	        return new MinuteVersionPageDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], MinuteVersionDTO);
	        this.next_cursor = source["next_cursor"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MinutesSettingsDTO {
	    prompt: string;
	    is_default: boolean;
	    updated_at: number;

	    static createFrom(source: any = {}) {
	        return new MinutesSettingsDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.prompt = source["prompt"];
	        this.is_default = source["is_default"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class MinutesStateDTO {
	    meeting_id: string;
	    state: string;
	    current?: MinuteVersionDTO;
	    latest_candidate?: MinuteVersionDTO;
	    recent_error_code?: string;
	    turn_id?: string;
	    runtime_state: string;
	    projection_state: string;
	    revision: number;

	    static createFrom(source: any = {}) {
	        return new MinutesStateDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.meeting_id = source["meeting_id"];
	        this.state = source["state"];
	        this.current = this.convertValues(source["current"], MinuteVersionDTO);
	        this.latest_candidate = this.convertValues(source["latest_candidate"], MinuteVersionDTO);
	        this.recent_error_code = source["recent_error_code"];
	        this.turn_id = source["turn_id"];
	        this.runtime_state = source["runtime_state"];
	        this.projection_state = source["projection_state"];
	        this.revision = source["revision"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RawRecordStateDTO {
	    state: string;
	    error_code?: string;

	    static createFrom(source: any = {}) {
	        return new RawRecordStateDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.error_code = source["error_code"];
	    }
	}
	export class ResourceCorrectionDTO {
	    request_id: string;
	    meeting_id: string;
	    resource_id: string;
	    expected_revision: number;
	    description: string;
	    reason?: string;

	    static createFrom(source: any = {}) {
	        return new ResourceCorrectionDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.request_id = source["request_id"];
	        this.meeting_id = source["meeting_id"];
	        this.resource_id = source["resource_id"];
	        this.expected_revision = source["expected_revision"];
	        this.description = source["description"];
	        this.reason = source["reason"];
	    }
	}
	export class ResourceOpenDTO {
	    resource_id: string;
	    integrity_state?: string;
	    verified_at?: number;
	    hostname?: string;
	    opened: boolean;

	    static createFrom(source: any = {}) {
	        return new ResourceOpenDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.resource_id = source["resource_id"];
	        this.integrity_state = source["integrity_state"];
	        this.verified_at = source["verified_at"];
	        this.hostname = source["hostname"];
	        this.opened = source["opened"];
	    }
	}
	export class Result___meet_sieve_internal_transport_wails_ASRTimelineEntryDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: ASRTimelineEntryDTO[];
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result___meet_sieve_internal_transport_wails_ASRTimelineEntryDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], ASRTimelineEntryDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result___meet_sieve_internal_transport_wails_AgentTimelineEntryDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: AgentTimelineEntryDTO[];
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result___meet_sieve_internal_transport_wails_AgentTimelineEntryDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], AgentTimelineEntryDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result___meet_sieve_internal_transport_wails_GroupDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: GroupDTO[];
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result___meet_sieve_internal_transport_wails_GroupDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], GroupDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result___meet_sieve_internal_transport_wails_InputDeviceDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: InputDeviceDTO[];
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result___meet_sieve_internal_transport_wails_InputDeviceDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], InputDeviceDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result___meet_sieve_internal_transport_wails_MemberDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: MemberDTO[];
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result___meet_sieve_internal_transport_wails_MemberDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], MemberDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class VoiceSampleDTO {
	    id: string;
	    member_id: string;
	    duration_ms: number;
	    source_kind: string;
	    source_name: string;
	    environment_kind: string;
	    processing_state: string;
	    quality_state: string;
	    quality_code: string;
	    created_at: number;

	    static createFrom(source: any = {}) {
	        return new VoiceSampleDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.member_id = source["member_id"];
	        this.duration_ms = source["duration_ms"];
	        this.source_kind = source["source_kind"];
	        this.source_name = source["source_name"];
	        this.environment_kind = source["environment_kind"];
	        this.processing_state = source["processing_state"];
	        this.quality_state = source["quality_state"];
	        this.quality_code = source["quality_code"];
	        this.created_at = source["created_at"];
	    }
	}
	export class Result___meet_sieve_internal_transport_wails_VoiceSampleDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: VoiceSampleDTO[];
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result___meet_sieve_internal_transport_wails_VoiceSampleDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], VoiceSampleDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_bool_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: boolean;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_bool_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = source["data"];
	        this.requestId = source["requestId"];
	    }
	}
	export class Result_meet_sieve_internal_app_buildinfo_Info_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: buildinfo.Info;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_app_buildinfo_Info_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], buildinfo.Info);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_app_health_Snapshot_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: health.Snapshot;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_app_health_Snapshot_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], health.Snapshot);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_service_voice_RebuildProgress_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: voice.RebuildProgress;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_service_voice_RebuildProgress_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], voice.RebuildProgress);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_ASRConnectionProbeDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: ASRConnectionProbeDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_ASRConnectionProbeDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], ASRConnectionProbeDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_ASRSettingsDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: ASRSettingsDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_ASRSettingsDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], ASRSettingsDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_ActiveMeetingDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: ActiveMeetingDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_ActiveMeetingDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], ActiveMeetingDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_AgentAskDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: AgentAskDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_AgentAskDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], AgentAskDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_AgentAvailabilityDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: AgentAvailabilityDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_AgentAvailabilityDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], AgentAvailabilityDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_AgentRecoveryCommandsDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: AgentRecoveryCommandsDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_AgentRecoveryCommandsDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], AgentRecoveryCommandsDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_AgentSettingsDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: AgentSettingsDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_AgentSettingsDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], AgentSettingsDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_AgentStateDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: AgentStateDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_AgentStateDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], AgentStateDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_AppEvent_string__ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: AppEvent_string_;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_AppEvent_string__(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], AppEvent_string_);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_AttachmentSendDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: AttachmentSendDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_AttachmentSendDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], AttachmentSendDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_AudioClipDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: AudioClipDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_AudioClipDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], AudioClipDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_AudioSettingsDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: AudioSettingsDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_AudioSettingsDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], AudioSettingsDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_BootstrapStateDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: BootstrapStateDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_BootstrapStateDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], BootstrapStateDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_CancelUploadDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: CancelUploadDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_CancelUploadDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], CancelUploadDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_CommandResultDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: CommandResultDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_CommandResultDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], CommandResultDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_ContentPageDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: ContentPageDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_ContentPageDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], ContentPageDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_CorrectionEntryDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: CorrectionEntryDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_CorrectionEntryDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], CorrectionEntryDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_CorrectionPageDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: CorrectionPageDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_CorrectionPageDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], CorrectionPageDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_CorrectionResultDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: CorrectionResultDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_CorrectionResultDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], CorrectionResultDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_DeletionJobDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: DeletionJobDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_DeletionJobDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], DeletionJobDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_DeletionPreviewDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: DeletionPreviewDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_DeletionPreviewDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], DeletionPreviewDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_DiagnosticExportDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: DiagnosticExportDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_DiagnosticExportDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], DiagnosticExportDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_FinalizationStateDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: FinalizationStateDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_FinalizationStateDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], FinalizationStateDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_GapCommandDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: GapCommandDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_GapCommandDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], GapCommandDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_GapConflictDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: GapConflictDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_GapConflictDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], GapConflictDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_GapStateDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: GapStateDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_GapStateDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], GapStateDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_GroupDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: GroupDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_GroupDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], GroupDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_HomeDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: HomeDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_HomeDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], HomeDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_InterruptedRecoveryDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: InterruptedRecoveryDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_InterruptedRecoveryDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], InterruptedRecoveryDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_LANInterfaceListDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: LANInterfaceListDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_LANInterfaceListDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], LANInterfaceListDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_LANStatusDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: LANStatusDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_LANStatusDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], LANStatusDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_LiveMeetingStatusDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: LiveMeetingStatusDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_LiveMeetingStatusDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], LiveMeetingStatusDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_MeetingCreateDraftDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: MeetingCreateDraftDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_MeetingCreateDraftDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], MeetingCreateDraftDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_MeetingDetailDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: MeetingDetailDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_MeetingDetailDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], MeetingDetailDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_MeetingPageDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: MeetingPageDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_MeetingPageDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], MeetingPageDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_MeetingPeopleOptionsDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: MeetingPeopleOptionsDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_MeetingPeopleOptionsDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], MeetingPeopleOptionsDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_MeetingProjectionDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: MeetingProjectionDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_MeetingProjectionDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], MeetingProjectionDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_MeetingStateEventDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: MeetingStateEventDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_MeetingStateEventDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], MeetingStateEventDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_MemberDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: MemberDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_MemberDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], MemberDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_MemberDetailDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: MemberDetailDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_MemberDetailDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], MemberDetailDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_MinuteMutationDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: MinuteMutationDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_MinuteMutationDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], MinuteMutationDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_MinuteVersionPageDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: MinuteVersionPageDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_MinuteVersionPageDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], MinuteVersionPageDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_MinutesSettingsDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: MinutesSettingsDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_MinutesSettingsDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], MinutesSettingsDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_MinutesStateDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: MinutesStateDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_MinutesStateDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], MinutesStateDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_RawRecordStateDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: RawRecordStateDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_RawRecordStateDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], RawRecordStateDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_ResourceOpenDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: ResourceOpenDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_ResourceOpenDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], ResourceOpenDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SpeakerStatusDTO {
	    meeting_id: string;
	    state: string;
	    error_code?: string;

	    static createFrom(source: any = {}) {
	        return new SpeakerStatusDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.meeting_id = source["meeting_id"];
	        this.state = source["state"];
	        this.error_code = source["error_code"];
	    }
	}
	export class Result_meet_sieve_internal_transport_wails_SpeakerStatusDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: SpeakerStatusDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_SpeakerStatusDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], SpeakerStatusDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class StorageScanDTO {
	    stage: string;
	    running: boolean;
	    scanned_items: number;
	    total_bytes: number;
	    available_bytes: number;
	    workspace_bytes: number;
	    categories: diagnostics.StorageCategories;
	    top_meetings: diagnostics.MeetingStorage[];
	    warnings: string[];
	    scanned_at: number;
	    error_code?: string;

	    static createFrom(source: any = {}) {
	        return new StorageScanDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stage = source["stage"];
	        this.running = source["running"];
	        this.scanned_items = source["scanned_items"];
	        this.total_bytes = source["total_bytes"];
	        this.available_bytes = source["available_bytes"];
	        this.workspace_bytes = source["workspace_bytes"];
	        this.categories = this.convertValues(source["categories"], diagnostics.StorageCategories);
	        this.top_meetings = this.convertValues(source["top_meetings"], diagnostics.MeetingStorage);
	        this.warnings = source["warnings"];
	        this.scanned_at = source["scanned_at"];
	        this.error_code = source["error_code"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_StorageScanDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: StorageScanDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_StorageScanDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], StorageScanDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TimelineEntryDTO {
	    seq: number;
	    kind: string;
	    occurred_at: number;
	    source: string;
	    entity_id?: string;
	    display_name?: string;
	    text?: string;
	    content_format?: string;
	    speaker_key?: string;
	    speaker_label?: string;
	    speaker_revision?: number;
	    start_sample?: number;
	    end_sample?: number;
	    state?: string;
	    reason?: string;
	    resource_kind?: string;
	    original_name?: string;
	    media_type?: string;
	    size_bytes?: number;
	    sha256?: string;
	    url?: string;
	    description?: string;

	    static createFrom(source: any = {}) {
	        return new TimelineEntryDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.kind = source["kind"];
	        this.occurred_at = source["occurred_at"];
	        this.source = source["source"];
	        this.entity_id = source["entity_id"];
	        this.display_name = source["display_name"];
	        this.text = source["text"];
	        this.content_format = source["content_format"];
	        this.speaker_key = source["speaker_key"];
	        this.speaker_label = source["speaker_label"];
	        this.speaker_revision = source["speaker_revision"];
	        this.start_sample = source["start_sample"];
	        this.end_sample = source["end_sample"];
	        this.state = source["state"];
	        this.reason = source["reason"];
	        this.resource_kind = source["resource_kind"];
	        this.original_name = source["original_name"];
	        this.media_type = source["media_type"];
	        this.size_bytes = source["size_bytes"];
	        this.sha256 = source["sha256"];
	        this.url = source["url"];
	        this.description = source["description"];
	    }
	}
	export class Result_meet_sieve_internal_transport_wails_TimelineEntryDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: TimelineEntryDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_TimelineEntryDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], TimelineEntryDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TimelinePageDTO {
	    entries: TimelineEntryDTO[];
	    oldest_seq: number;
	    latest_seq: number;
	    has_older: boolean;
	    has_more_after: boolean;

	    static createFrom(source: any = {}) {
	        return new TimelinePageDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entries = this.convertValues(source["entries"], TimelineEntryDTO);
	        this.oldest_seq = source["oldest_seq"];
	        this.latest_seq = source["latest_seq"];
	        this.has_older = source["has_older"];
	        this.has_more_after = source["has_more_after"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_TimelinePageDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: TimelinePageDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_TimelinePageDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], TimelinePageDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TranscriptItemDTO {
	    seq: number;
	    kind: string;
	    occurred_at: number;
	    text?: string;
	    speaker_name?: string;
	    speaker_display?: string;
	    start_sample?: number;
	    end_sample?: number;

	    static createFrom(source: any = {}) {
	        return new TranscriptItemDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.kind = source["kind"];
	        this.occurred_at = source["occurred_at"];
	        this.text = source["text"];
	        this.speaker_name = source["speaker_name"];
	        this.speaker_display = source["speaker_display"];
	        this.start_sample = source["start_sample"];
	        this.end_sample = source["end_sample"];
	    }
	}
	export class TranscriptPageDTO {
	    items: TranscriptItemDTO[];
	    has_more: boolean;
	    has_previous: boolean;
	    has_next: boolean;
	    after_seq?: number;
	    before_seq?: number;

	    static createFrom(source: any = {}) {
	        return new TranscriptPageDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], TranscriptItemDTO);
	        this.has_more = source["has_more"];
	        this.has_previous = source["has_previous"];
	        this.has_next = source["has_next"];
	        this.after_seq = source["after_seq"];
	        this.before_seq = source["before_seq"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_TranscriptPageDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: TranscriptPageDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_TranscriptPageDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], TranscriptPageDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class VoiceFileSelectionDTO {
	    token: string;
	    file_name: string;

	    static createFrom(source: any = {}) {
	        return new VoiceFileSelectionDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.token = source["token"];
	        this.file_name = source["file_name"];
	    }
	}
	export class Result_meet_sieve_internal_transport_wails_VoiceFileSelectionDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: VoiceFileSelectionDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_VoiceFileSelectionDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], VoiceFileSelectionDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class VoiceModelDTO {
	    state: string;
	    usable: boolean;
	    modelId: string;
	    modelName: string;
	    modelVersion: string;
	    modelSize: number;
	    location: string;

	    static createFrom(source: any = {}) {
	        return new VoiceModelDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.usable = source["usable"];
	        this.modelId = source["modelId"];
	        this.modelName = source["modelName"];
	        this.modelVersion = source["modelVersion"];
	        this.modelSize = source["modelSize"];
	        this.location = source["location"];
	    }
	}
	export class Result_meet_sieve_internal_transport_wails_VoiceModelDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: VoiceModelDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_VoiceModelDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], VoiceModelDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class VoiceRecordingStateDTO {
	    recording: boolean;
	    level: number;
	    duration_ms: number;

	    static createFrom(source: any = {}) {
	        return new VoiceRecordingStateDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.recording = source["recording"];
	        this.level = source["level"];
	        this.duration_ms = source["duration_ms"];
	    }
	}
	export class Result_meet_sieve_internal_transport_wails_VoiceRecordingStateDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: VoiceRecordingStateDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_VoiceRecordingStateDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], VoiceRecordingStateDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class VoiceSampleChangedDTO {
	    sample_id: string;
	    member_id: string;
	    processing_state: string;
	    quality_state: string;
	    quality_code?: string;

	    static createFrom(source: any = {}) {
	        return new VoiceSampleChangedDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sample_id = source["sample_id"];
	        this.member_id = source["member_id"];
	        this.processing_state = source["processing_state"];
	        this.quality_state = source["quality_state"];
	        this.quality_code = source["quality_code"];
	    }
	}
	export class Result_meet_sieve_internal_transport_wails_VoiceSampleChangedDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: VoiceSampleChangedDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_VoiceSampleChangedDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], VoiceSampleChangedDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_meet_sieve_internal_transport_wails_VoiceSampleDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: VoiceSampleDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_VoiceSampleDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], VoiceSampleDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WakeWordTestStateDTO {
	    state: string;
	    matched: number;
	    required: number;
	    asr_state: string;
	    error_code?: string;

	    static createFrom(source: any = {}) {
	        return new WakeWordTestStateDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.matched = source["matched"];
	        this.required = source["required"];
	        this.asr_state = source["asr_state"];
	        this.error_code = source["error_code"];
	    }
	}
	export class Result_meet_sieve_internal_transport_wails_WakeWordTestStateDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: WakeWordTestStateDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_WakeWordTestStateDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], WakeWordTestStateDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WorkspaceCandidateDTO {
	    canonical_path: string;
	    kind: string;
	    reason: string;
	    writable: boolean;
	    local_volume: boolean;
	    schema_state: string;
	    free_bytes: number;
	    warnings: string[];

	    static createFrom(source: any = {}) {
	        return new WorkspaceCandidateDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.canonical_path = source["canonical_path"];
	        this.kind = source["kind"];
	        this.reason = source["reason"];
	        this.writable = source["writable"];
	        this.local_volume = source["local_volume"];
	        this.schema_state = source["schema_state"];
	        this.free_bytes = source["free_bytes"];
	        this.warnings = source["warnings"];
	    }
	}
	export class Result_meet_sieve_internal_transport_wails_WorkspaceCandidateDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: WorkspaceCandidateDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_WorkspaceCandidateDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], WorkspaceCandidateDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WorkspaceSettingsDTO {
	    active_path: string;
	    saved_path: string;
	    restart_required: boolean;
	    editable: boolean;
	    disabled_reason: string;

	    static createFrom(source: any = {}) {
	        return new WorkspaceSettingsDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active_path = source["active_path"];
	        this.saved_path = source["saved_path"];
	        this.restart_required = source["restart_required"];
	        this.editable = source["editable"];
	        this.disabled_reason = source["disabled_reason"];
	    }
	}
	export class Result_meet_sieve_internal_transport_wails_WorkspaceSettingsDTO_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: WorkspaceSettingsDTO;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_meet_sieve_internal_transport_wails_WorkspaceSettingsDTO_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = this.convertValues(source["data"], WorkspaceSettingsDTO);
	        this.requestId = source["requestId"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_string_ {
	    code: number;
	    errorCode?: string;
	    message: string;
	    data?: string;
	    requestId: string;

	    static createFrom(source: any = {}) {
	        return new Result_string_(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.data = source["data"];
	        this.requestId = source["requestId"];
	    }
	}
	export class SaveASRSettingsDTO {
	    api_key: CredentialChangeDTO;

	    static createFrom(source: any = {}) {
	        return new SaveASRSettingsDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.api_key = this.convertValues(source["api_key"], CredentialChangeDTO);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SaveAgentSettingsDTO {
	    wake_word: string;
	    codex_executable_path: string;
	    codex_proxy_port: number;

	    static createFrom(source: any = {}) {
	        return new SaveAgentSettingsDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.wake_word = source["wake_word"];
	        this.codex_executable_path = source["codex_executable_path"];
	        this.codex_proxy_port = source["codex_proxy_port"];
	    }
	}
	export class SendMeetingMessageDTO {
	    meeting_id: string;
	    request_id: string;
	    content: string;

	    static createFrom(source: any = {}) {
	        return new SendMeetingMessageDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.meeting_id = source["meeting_id"];
	        this.request_id = source["request_id"];
	        this.content = source["content"];
	    }
	}
	export class SeqPageInputDTO {
	    meeting_id: string;
	    after_seq: number;
	    before_seq: number;
	    limit: number;

	    static createFrom(source: any = {}) {
	        return new SeqPageInputDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.meeting_id = source["meeting_id"];
	        this.after_seq = source["after_seq"];
	        this.before_seq = source["before_seq"];
	        this.limit = source["limit"];
	    }
	}

	export class StartMeetingDTO {
	    meeting_no: string;
	    suggested_meeting_no: string;
	    subject: string;
	    member_ids: string[];
	    temporary_participant_names: string[];
	    microphone_id: string;
	    local_timezone: string;
	    asr_mode: string;
	    lan_enabled: boolean;
	    lan_interface_id: string;

	    static createFrom(source: any = {}) {
	        return new StartMeetingDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.meeting_no = source["meeting_no"];
	        this.suggested_meeting_no = source["suggested_meeting_no"];
	        this.subject = source["subject"];
	        this.member_ids = source["member_ids"];
	        this.temporary_participant_names = source["temporary_participant_names"];
	        this.microphone_id = source["microphone_id"];
	        this.local_timezone = source["local_timezone"];
	        this.asr_mode = source["asr_mode"];
	        this.lan_enabled = source["lan_enabled"];
	        this.lan_interface_id = source["lan_interface_id"];
	    }
	}

	export class TestASRConnectionDTO {
	    api_key: string;

	    static createFrom(source: any = {}) {
	        return new TestASRConnectionDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.api_key = source["api_key"];
	    }
	}


	export class TimelineQueryDTO {
	    meeting_id: string;
	    direction: string;
	    cursor_seq: number;
	    limit: number;

	    static createFrom(source: any = {}) {
	        return new TimelineQueryDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.meeting_id = source["meeting_id"];
	        this.direction = source["direction"];
	        this.cursor_seq = source["cursor_seq"];
	        this.limit = source["limit"];
	    }
	}


	export class UpdateGroupDTO {
	    name: string;
	    default_lan_enabled: boolean;
	    member_ids: string[];
	    revision?: number;

	    static createFrom(source: any = {}) {
	        return new UpdateGroupDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.default_lan_enabled = source["default_lan_enabled"];
	        this.member_ids = source["member_ids"];
	        this.revision = source["revision"];
	    }
	}
	export class UpdateMemberDTO {
	    name: string;
	    notes: string;
	    revision?: number;

	    static createFrom(source: any = {}) {
	        return new UpdateMemberDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.notes = source["notes"];
	        this.revision = source["revision"];
	    }
	}








}
