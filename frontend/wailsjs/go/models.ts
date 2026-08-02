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
	    mode: string;
	    connection_established: boolean;
	    real_audio_verified: boolean;
	    latency_ms: number;

	    static createFrom(source: any = {}) {
	        return new ASRConnectionProbeDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.connection_established = source["connection_established"];
	        this.real_audio_verified = source["real_audio_verified"];
	        this.latency_ms = source["latency_ms"];
	    }
	}
	export class ASRSettingsDTO {
	    mode: string;
	    app_id_configured: boolean;
	    app_id_mask: string;
	    access_token_configured: boolean;
	    access_token_mask: string;
	    api_key_configured: boolean;
	    api_key_mask: string;
	    updated_at: number;

	    static createFrom(source: any = {}) {
	        return new ASRSettingsDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.app_id_configured = source["app_id_configured"];
	        this.app_id_mask = source["app_id_mask"];
	        this.access_token_configured = source["access_token_configured"];
	        this.access_token_mask = source["access_token_mask"];
	        this.api_key_configured = source["api_key_configured"];
	        this.api_key_mask = source["api_key_mask"];
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

	export class MemberDTO {
	    id: string;
	    name: string;
	    notes?: string;
	    accepted_sample_count: number;
	    rejected_sample_count: number;
	    voice_readiness: string;
	    created_at: number;
	    updated_at: number;

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
	    mode: string;
	    app_id: CredentialChangeDTO;
	    access_token: CredentialChangeDTO;
	    api_key: CredentialChangeDTO;

	    static createFrom(source: any = {}) {
	        return new SaveASRSettingsDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.app_id = this.convertValues(source["app_id"], CredentialChangeDTO);
	        this.access_token = this.convertValues(source["access_token"], CredentialChangeDTO);
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

	export class StartMeetingDTO {
	    meeting_no: string;
	    suggested_meeting_no: string;
	    subject: string;
	    member_ids: string[];
	    temporary_participant_names: string[];
	    microphone_id: string;
	    local_timezone: string;
	    asr_mode: string;

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
	    }
	}
	export class TestASRConnectionDTO {
	    mode: string;
	    app_id: string;
	    access_token: string;
	    api_key: string;

	    static createFrom(source: any = {}) {
	        return new TestASRConnectionDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.app_id = source["app_id"];
	        this.access_token = source["access_token"];
	        this.api_key = source["api_key"];
	    }
	}
	export class UpdateMemberDTO {
	    name: string;
	    notes: string;

	    static createFrom(source: any = {}) {
	        return new UpdateMemberDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.notes = source["notes"];
	    }
	}







}
