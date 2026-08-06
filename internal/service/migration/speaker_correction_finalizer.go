package migration

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

const (
	step5UtteranceLegacyTable  = "utterances_step4_legacy"
	step5ClusterLegacyTable    = "speaker_clusters_step4_legacy"
	step5CorrectionLegacyTable = "corrections_step4_legacy"
)

var step5MigrationNamespace = uuid.MustParse("2ed5a330-558f-4da4-9b43-4e21fe10de56")

type legacySpeakerCluster struct {
	id               string
	meetingID        string
	asrSessionID     string
	asrSpeakerLabel  string
	assignedMemberID sql.NullString
	confidence       sql.NullFloat64
	createdAt        int64
	updatedAt        int64
}

type migratedTrackProjection struct {
	trackID       string
	participantID sql.NullString
	clusterID     sql.NullString
	confidence    sql.NullFloat64
}

// finalizeStep5Legacy 将 Step 4 staging 事实转换为 Step 5 结构并执行守恒校验。
func finalizeStep5Legacy(tx *sql.Tx) error {
	legacyCounts, err := readStep5LegacyCounts(tx)
	if err != nil {
		return err
	}
	tracks, err := migrateLegacySpeakerClusters(tx)
	if err != nil {
		return err
	}
	if err := migrateLegacyUtterances(tx, tracks); err != nil {
		return err
	}
	if err := migrateLegacyCorrections(tx); err != nil {
		return err
	}
	if err := verifyStep5RowConservation(tx, legacyCounts); err != nil {
		return err
	}
	if err := dropStep5LegacyTables(tx); err != nil {
		return err
	}
	return verifyForeignKeys(tx)
}

// readStep5LegacyCounts 读取迁移前行数，供完成前守恒验证。
func readStep5LegacyCounts(tx *sql.Tx) ([3]int, error) {
	var counts [3]int
	for index, table := range []string{step5ClusterLegacyTable, step5UtteranceLegacyTable, step5CorrectionLegacyTable} {
		if err := tx.QueryRow("SELECT count(*) FROM " + table).Scan(&counts[index]); err != nil {
			return counts, fmt.Errorf("统计 Step 5 legacy 表失败：%w", err)
		}
	}
	return counts, nil
}

// migrateLegacySpeakerClusters 将旧 session label 行转换为 track，并为 unknown 分配稳定本场编号。
func migrateLegacySpeakerClusters(tx *sql.Tx) (map[string]migratedTrackProjection, error) {
	clusters, err := queryLegacySpeakerClusters(tx)
	if err != nil {
		return nil, err
	}
	projections := make(map[string]migratedTrackProjection, len(clusters))
	displayNumbers := map[string]int{}
	for _, cluster := range clusters {
		projection, migrateErr := migrateLegacySpeakerCluster(tx, cluster, displayNumbers)
		if migrateErr != nil {
			return nil, migrateErr
		}
		projections[speakerTrackKey(cluster.asrSessionID, cluster.asrSpeakerLabel)] = projection
	}
	return projections, nil
}

// queryLegacySpeakerClusters 按会议和创建顺序读取旧 label，保证 unknown 编号可复现。
func queryLegacySpeakerClusters(tx *sql.Tx) ([]legacySpeakerCluster, error) {
	rows, err := tx.Query(`SELECT id, meeting_id, asr_session_id, asr_speaker_label,
		assigned_member_id, confidence, created_at, updated_at
		FROM speaker_clusters_step4_legacy ORDER BY meeting_id, created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("读取 legacy speaker clusters 失败：%w", err)
	}
	defer rows.Close()
	var clusters []legacySpeakerCluster
	for rows.Next() {
		var cluster legacySpeakerCluster
		if err := rows.Scan(&cluster.id, &cluster.meetingID, &cluster.asrSessionID, &cluster.asrSpeakerLabel,
			&cluster.assignedMemberID, &cluster.confidence, &cluster.createdAt, &cluster.updatedAt); err != nil {
			return nil, fmt.Errorf("解析 legacy speaker cluster 失败：%w", err)
		}
		clusters = append(clusters, cluster)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 legacy speaker clusters 失败：%w", err)
	}
	return clusters, nil
}

// migrateLegacySpeakerCluster 转换一条旧 label；成员映射不唯一时明确终止升级。
func migrateLegacySpeakerCluster(tx *sql.Tx, cluster legacySpeakerCluster, displayNumbers map[string]int) (migratedTrackProjection, error) {
	projection := migratedTrackProjection{trackID: cluster.id, confidence: cluster.confidence}
	state := "clustered"
	if cluster.assignedMemberID.Valid {
		participantID, err := findUniqueParticipant(tx, cluster.meetingID, cluster.assignedMemberID.String)
		if err != nil {
			return projection, err
		}
		projection.participantID = sql.NullString{String: participantID, Valid: true}
		state = "matched"
	} else {
		displayNumbers[cluster.meetingID]++
		clusterID := uuid.NewSHA1(step5MigrationNamespace, []byte("legacy-speaker-cluster:"+cluster.id)).String()
		projection.clusterID = sql.NullString{String: clusterID, Valid: true}
		if _, err := tx.Exec(`INSERT INTO speaker_clusters (
			id, meeting_id, display_no, assignment_source, track_count, confidence, revision, created_at, updated_at
		) VALUES (?, ?, ?, 'unassigned', 0, ?, 1, ?, ?)`,
			clusterID, cluster.meetingID, displayNumbers[cluster.meetingID], cluster.confidence, cluster.createdAt, cluster.updatedAt); err != nil {
			return projection, fmt.Errorf("迁移 unknown speaker cluster 失败：%w", err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO speaker_tracks (
		id, meeting_id, asr_session_id, source, asr_speaker_label, provider_segment_no, state, automatic_participant_id,
		speaker_cluster_id, top_score, evidence_duration_ms, routing_revision, revision, created_at, updated_at
	) VALUES (?, ?, ?, 'provider_label', ?, 1, ?, ?, ?, ?, 0, 1, 1, ?, ?)`, cluster.id, cluster.meetingID, cluster.asrSessionID,
		cluster.asrSpeakerLabel, state, projection.participantID, projection.clusterID, cluster.confidence,
		cluster.createdAt, cluster.updatedAt); err != nil {
		return projection, fmt.Errorf("迁移 speaker track 失败：%w", err)
	}
	return projection, nil
}

// findUniqueParticipant 将旧成员身份映射到同场唯一正式参会者快照。
func findUniqueParticipant(tx *sql.Tx, meetingID string, memberID string) (string, error) {
	rows, err := tx.Query(`SELECT id FROM meeting_participants
		WHERE meeting_id = ? AND member_id = ? AND participant_kind = 'member' ORDER BY id`, meetingID, memberID)
	if err != nil {
		return "", fmt.Errorf("查询会议参会者快照失败：%w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("解析会议参会者快照失败：%w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("遍历会议参会者快照失败：%w", err)
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("成员无法唯一映射到会议参会者：meeting_id=%s count=%d", meetingID, len(ids))
	}
	return ids[0], nil
}

// migrateLegacyUtterances 保留原始转写，并回填 participant、track/cluster 与版本投影。
func migrateLegacyUtterances(tx *sql.Tx, tracks map[string]migratedTrackProjection) error {
	rows, err := tx.Query(`SELECT id, meeting_id, event_id, asr_session_id, provider_result_id,
		original_text, current_text, start_sample, end_sample, asr_speaker_label, current_member_id,
		created_at, updated_at FROM utterances_step4_legacy ORDER BY created_at, id`)
	if err != nil {
		return fmt.Errorf("读取 legacy utterances 失败：%w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var values legacyUtterance
		if err := values.scan(rows); err != nil {
			return err
		}
		if err := insertMigratedUtterance(tx, values, tracks); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历 legacy utterances 失败：%w", err)
	}
	return nil
}

type legacyUtterance struct {
	id, meetingID, eventID, asrSessionID, providerResultID string
	originalText, currentText                              string
	startSample, endSample, createdAt, updatedAt           int64
	asrSpeakerLabel, currentMemberID                       sql.NullString
}

// scan 从固定列序读取一条 legacy utterance。
func (value *legacyUtterance) scan(rows *sql.Rows) error {
	if err := rows.Scan(&value.id, &value.meetingID, &value.eventID, &value.asrSessionID, &value.providerResultID,
		&value.originalText, &value.currentText, &value.startSample, &value.endSample, &value.asrSpeakerLabel,
		&value.currentMemberID, &value.createdAt, &value.updatedAt); err != nil {
		return fmt.Errorf("解析 legacy utterance 失败：%w", err)
	}
	return nil
}

// insertMigratedUtterance 计算单条转写的确定性身份与历史 revision 后写入新表。
func insertMigratedUtterance(tx *sql.Tx, value legacyUtterance, tracks map[string]migratedTrackProjection) error {
	participantID := sql.NullString{}
	if value.currentMemberID.Valid {
		id, err := findUniqueParticipant(tx, value.meetingID, value.currentMemberID.String)
		if err != nil {
			return err
		}
		participantID = sql.NullString{String: id, Valid: true}
	}
	projection := migratedTrackProjection{}
	if value.asrSpeakerLabel.Valid {
		projection = tracks[speakerTrackKey(value.asrSessionID, value.asrSpeakerLabel.String)]
	}
	source := "unassigned"
	if participantID.Valid {
		source = "automatic_member"
	} else if projection.clusterID.Valid {
		source = "automatic_cluster"
	}
	textRevision, err := migratedRevision(tx, value.id, "text")
	if err != nil {
		return err
	}
	speakerRevision, err := migratedRevision(tx, value.id, "member_assignment")
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO utterances (
		id, meeting_id, event_id, asr_session_id, provider_result_id, original_text, current_text,
		start_sample, end_sample, asr_speaker_label, current_participant_id, speaker_track_id,
		speaker_cluster_id, speaker_assignment_source, speaker_confidence, text_revision, speaker_revision,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.id, value.meetingID, value.eventID, value.asrSessionID, value.providerResultID, value.originalText,
		value.currentText, value.startSample, value.endSample, value.asrSpeakerLabel, participantID,
		nullableTrackID(projection.trackID), projection.clusterID, source, projection.confidence, textRevision,
		speakerRevision, value.createdAt, value.updatedAt)
	if err != nil {
		return fmt.Errorf("迁移 utterance 投影失败：%w", err)
	}
	return nil
}

// migratedRevision 按旧校对条数恢复当前投影版本，初始版本固定为 1。
func migratedRevision(tx *sql.Tx, targetID string, correctionKind string) (int, error) {
	var count int
	if err := tx.QueryRow(`SELECT count(*) FROM corrections_step4_legacy
		WHERE target_kind='utterance' AND target_id=? AND correction_kind=?`, targetID, correctionKind).Scan(&count); err != nil {
		return 0, fmt.Errorf("统计 legacy correction revision 失败：%w", err)
	}
	return count + 1, nil
}

// migrateLegacyCorrections 按目标和时间顺序回填 UUID v5 与相邻 revision。
func migrateLegacyCorrections(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id, meeting_id, event_id, target_kind, target_id, correction_kind,
		before_json, after_json, operator_kind, operator_id, reason, created_at, updated_at
		FROM corrections_step4_legacy ORDER BY target_kind, target_id, correction_kind, created_at, id`)
	if err != nil {
		return fmt.Errorf("读取 legacy corrections 失败：%w", err)
	}
	defer rows.Close()
	revisions := map[string]int{}
	for rows.Next() {
		var value legacyCorrection
		if err := value.scan(rows); err != nil {
			return err
		}
		key := value.targetKind + "\x00" + value.targetID + "\x00" + value.correctionKind
		targetRevision := revisions[key] + 1
		revisions[key] = targetRevision
		requestID := uuid.NewSHA1(step5MigrationNamespace, []byte("legacy-correction:"+value.id)).String()
		if err := value.insert(tx, requestID, targetRevision); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历 legacy corrections 失败：%w", err)
	}
	return nil
}

type legacyCorrection struct {
	id, meetingID, eventID, targetKind, targetID, correctionKind string
	beforeJSON, afterJSON, operatorKind                          string
	operatorID, reason                                           sql.NullString
	createdAt, updatedAt                                         int64
}

// scan 从固定列序读取一条 legacy correction。
func (value *legacyCorrection) scan(rows *sql.Rows) error {
	if err := rows.Scan(&value.id, &value.meetingID, &value.eventID, &value.targetKind, &value.targetID,
		&value.correctionKind, &value.beforeJSON, &value.afterJSON, &value.operatorKind, &value.operatorID,
		&value.reason, &value.createdAt, &value.updatedAt); err != nil {
		return fmt.Errorf("解析 legacy correction 失败：%w", err)
	}
	return nil
}

// insert 写入历史 correction 的确定性幂等与 revision 字段。
func (value legacyCorrection) insert(tx *sql.Tx, requestID string, targetRevision int) error {
	_, err := tx.Exec(`INSERT INTO corrections (
		id, meeting_id, event_id, request_id, target_kind, target_id, correction_kind, before_json,
		after_json, operator_kind, operator_id, reason, target_revision, result_revision, batch_scope,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'single', ?, ?)`, value.id, value.meetingID,
		value.eventID, requestID, value.targetKind, value.targetID, value.correctionKind, value.beforeJSON,
		value.afterJSON, value.operatorKind, value.operatorID, value.reason, targetRevision, targetRevision+1,
		value.createdAt, value.updatedAt)
	if err != nil {
		return fmt.Errorf("迁移 legacy correction 失败：%w", err)
	}
	return nil
}

// verifyStep5RowConservation 确认 track、转写与 correction 行数没有丢失。
func verifyStep5RowConservation(tx *sql.Tx, legacy [3]int) error {
	for index, table := range []string{"speaker_tracks", "utterances", "corrections"} {
		var count int
		if err := tx.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
			return fmt.Errorf("统计 Step 5 新表失败：%w", err)
		}
		if count != legacy[index] {
			return fmt.Errorf("Step 5 行数守恒失败：table=%s legacy=%d current=%d", table, legacy[index], count)
		}
	}
	return nil
}

// dropStep5LegacyTables 在所有转换和守恒检查通过后移除 staging 表。
func dropStep5LegacyTables(tx *sql.Tx) error {
	for _, table := range []string{step5CorrectionLegacyTable, step5UtteranceLegacyTable, step5ClusterLegacyTable} {
		if _, err := tx.Exec("DROP TABLE " + table); err != nil {
			return fmt.Errorf("删除 Step 5 legacy 表失败：table=%s err=%w", table, err)
		}
	}
	return nil
}

// verifyForeignKeys 要求 finalizer 事务结束前不存在任何外键违规。
func verifyForeignKeys(tx *sql.Tx) error {
	rows, err := tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("执行 Step 5 foreign_key_check 失败：%w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("Step 5 foreign_key_check 发现违规")
	}
	return rows.Err()
}

// speakerTrackKey 构造仅用于内存映射的无歧义 session/label 组合键。
func speakerTrackKey(sessionID string, label string) string { return sessionID + "\x00" + label }

// nullableTrackID 避免无 provider label 的历史转写写入空字符串外键。
func nullableTrackID(trackID string) sql.NullString {
	return sql.NullString{String: trackID, Valid: trackID != ""}
}
