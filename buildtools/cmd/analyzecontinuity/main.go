// Command analyzecontinuity 使用真实会议音频评估 CAM++ 短窗连续性分布，不修改会议数据。
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"sort"

	voiceonnx "meet-sieve/internal/adapter/voice/onnx"
	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/assets"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/port"
	meetingrepository "meet-sieve/internal/repository/meeting"
	speakerservice "meet-sieve/internal/service/speaker"
)

const sampleRate = 16000

type sampleDefinition struct {
	ID          string `json:"id"`
	SpeakerID   string `json:"speaker_id"`
	ProviderKey string `json:"provider_key"`
	Split       string `json:"split"`
	StartSample int64  `json:"start_sample"`
	EndSample   int64  `json:"end_sample"`
}

type manifest struct {
	SchemaVersion int                `json:"schema_version"`
	MeetingID     string             `json:"meeting_id"`
	WindowMS      int                `json:"window_ms"`
	Samples       []sampleDefinition `json:"samples"`
}

type encodedSample struct {
	definition sampleDefinition
	embedding  port.Embedding
	sha256     string
}

type sampleAudit struct {
	ID     string `json:"id"`
	SHA256 string `json:"pcm_sha256"`
}

type pairResult struct {
	LeftID  string  `json:"left_id"`
	RightID string  `json:"right_id"`
	Split   string  `json:"split"`
	Same    bool    `json:"same_speaker"`
	Score   float64 `json:"score"`
}

type distribution struct {
	Count int     `json:"count"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Mean  float64 `json:"mean"`
}

type routingGate struct {
	MinScore                 float64 `json:"min_score"`
	MinMargin                float64 `json:"min_margin"`
	SelectionMinTopSame      float64 `json:"selection_min_top_same"`
	SelectionMaxTopDifferent float64 `json:"selection_max_top_different"`
	SelectionMinMargin       float64 `json:"selection_min_margin"`
	ValidationCount          int     `json:"validation_count"`
	ValidationPassed         int     `json:"validation_passed"`
}

type report struct {
	Model     speakerdomain.ModelIdentity `json:"model"`
	WindowMS  int                         `json:"window_ms"`
	Gate      routingGate                 `json:"routing_gate"`
	Same      map[string]distribution     `json:"same_speaker"`
	Different map[string]distribution     `json:"different_speaker"`
	Replay    replayReport                `json:"routing_replay"`
	Samples   []sampleAudit               `json:"samples"`
	Pairs     []pairResult                `json:"pairs"`
}

type replayReport struct {
	SegmentCount int `json:"segment_count"`
	FalseMerge   int `json:"false_merge"`
	FalseSplit   int `json:"false_split"`
}

type replaySegment struct {
	speakerID string
	vectors   []speakerservice.TrackVector
}

// main 执行只读短窗分析并把脱敏分布写到标准输出。
func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "continuity 分析失败："+err.Error())
		os.Exit(1)
	}
}

// run 加载严格清单、会议音频和锁定模型后计算成对 cosine 分布。
func run(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("analyzecontinuity", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "", "短窗标注清单")
	databasePath := flags.String("database", "", "只读分析使用的会议数据库")
	workspacePath := flags.String("workspace", "", "会议数据根目录")
	assetsPath := flags.String("assets", "third_party/assets.lock.json", "第三方资源锁")
	modelPath := flags.String("model", "", "锁定 CAM++ model.onnx")
	runtimePath := flags.String("runtime", "", "锁定 ONNX Runtime 动态库")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *manifestPath == "" || *databasePath == "" || *workspacePath == "" || *modelPath == "" || *runtimePath == "" {
		return fmt.Errorf("必须显式提供 manifest、database、workspace、model 和 runtime")
	}
	definition, err := loadManifest(*manifestPath)
	if err != nil {
		return err
	}
	modelAsset, runtimeAsset, err := loadAssets(*assetsPath)
	if err != nil {
		return err
	}
	environment := voiceonnx.NewRuntime(runtimeAsset, *runtimePath)
	if _, err := environment.Start(); err != nil {
		return err
	}
	defer environment.Close()
	encoder, err := voiceonnx.NewEncoder(modelAsset, *modelPath)
	if err != nil {
		return err
	}
	defer encoder.Close()
	db, err := database.Open(*databasePath)
	if err != nil {
		return err
	}
	defer database.Close(db)
	reader, err := speakerservice.NewMeetingAudioReader(
		*workspacePath, meetingrepository.NewRepository(db), nil, int64(definition.WindowMS)*sampleRate/1000,
	)
	if err != nil {
		return err
	}
	encoded, model, err := encodeSamples(ctx, definition, reader, encoder)
	if err != nil {
		return err
	}
	result, err := buildReport(definition.WindowMS, model, encoded)
	if err != nil {
		return err
	}
	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(output))
	return nil
}

// loadManifest 严格解析并验证短窗标注清单。
func loadManifest(path string) (manifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return manifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var definition manifest
	if err := decoder.Decode(&definition); err != nil {
		return manifest{}, err
	}
	if err := requireEOF(decoder); err != nil {
		return manifest{}, err
	}
	if definition.SchemaVersion != 1 || definition.MeetingID == "" || definition.WindowMS < 1000 || definition.WindowMS > 8000 {
		return manifest{}, fmt.Errorf("清单基础字段无效")
	}
	seen := map[string]struct{}{}
	speakers := map[string]struct{}{}
	splits := map[string]struct{}{}
	for _, sample := range definition.Samples {
		if sample.ID == "" || sample.SpeakerID == "" || sample.ProviderKey == "" ||
			(sample.Split != "selection" && sample.Split != "validation") || sample.StartSample < 0 || sample.EndSample <= sample.StartSample {
			return manifest{}, fmt.Errorf("样本 %s 字段无效", sample.ID)
		}
		if _, exists := seen[sample.ID]; exists {
			return manifest{}, fmt.Errorf("样本 ID 重复：%s", sample.ID)
		}
		seen[sample.ID] = struct{}{}
		speakers[sample.SpeakerID] = struct{}{}
		splits[sample.Split] = struct{}{}
	}
	if len(speakers) < 2 || len(splits) != 2 {
		return manifest{}, fmt.Errorf("清单至少需要两位说话人及 selection/validation 两组")
	}
	return definition, nil
}

// encodeSamples 按窗口截断每条真实 final 并生成规范化 embedding。
func encodeSamples(ctx context.Context, definition manifest, reader *speakerservice.MeetingAudioReader, encoder port.VoiceEncoder) ([]encodedSample, speakerdomain.ModelIdentity, error) {
	info := encoder.ModelInfo()
	model := speakerdomain.ModelIdentity{ID: info.ID, Version: info.Version, SHA256: info.SHA256, Dimension: info.Dimension}
	windowSamples := int64(definition.WindowMS) * sampleRate / 1000
	result := make([]encodedSample, 0, len(definition.Samples))
	for _, sample := range definition.Samples {
		endSample := min(sample.EndSample, sample.StartSample+windowSamples)
		pcm, err := reader.Read(ctx, definition.MeetingID, sample.StartSample, endSample)
		if err != nil {
			return nil, model, fmt.Errorf("读取样本 %s 失败：%w", sample.ID, err)
		}
		embedding, err := speakerservice.EncodeTrack(ctx, encoder, model, pcm)
		if err != nil {
			return nil, model, fmt.Errorf("编码样本 %s 失败：%w", sample.ID, err)
		}
		result = append(result, encodedSample{definition: sample, embedding: embedding, sha256: hashPCM(pcm)})
	}
	return result, model, nil
}

// buildReport 计算同一数据分组内的全部成对分数与摘要。
func buildReport(windowMS int, model speakerdomain.ModelIdentity, samples []encodedSample) (report, error) {
	result := report{Model: model, WindowMS: windowMS, Same: map[string]distribution{}, Different: map[string]distribution{}}
	for left := 0; left < len(samples); left++ {
		for right := left + 1; right < len(samples); right++ {
			if samples[left].definition.Split != samples[right].definition.Split {
				continue
			}
			score, err := speakerservice.CosineSimilarity(samples[left].embedding, samples[right].embedding, model.Dimension)
			if err != nil {
				return report{}, err
			}
			result.Pairs = append(result.Pairs, pairResult{LeftID: samples[left].definition.ID, RightID: samples[right].definition.ID,
				Split: samples[left].definition.Split, Same: samples[left].definition.SpeakerID == samples[right].definition.SpeakerID, Score: score})
		}
	}
	sort.Slice(result.Pairs, func(left int, right int) bool { return result.Pairs[left].Score < result.Pairs[right].Score })
	for _, split := range []string{"selection", "validation"} {
		result.Same[split] = summarizePairs(result.Pairs, split, true)
		result.Different[split] = summarizePairs(result.Pairs, split, false)
	}
	result.Gate = calibrateRoutingGate(samples, result.Pairs)
	result.Replay = replayRouting(samples, result.Gate, model.Dimension)
	for _, sample := range samples {
		result.Samples = append(result.Samples, sampleAudit{ID: sample.definition.ID, SHA256: sample.sha256})
	}
	return result, nil
}

// hashPCM 对实际进入 encoder 的短窗 PCM 计算稳定摘要，不输出原始音频。
func hashPCM(samples []int16) string {
	content := make([]byte, len(samples)*2)
	for index, sample := range samples {
		binary.LittleEndian.PutUint16(content[index*2:], uint16(sample))
	}
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

// replayRouting 按 manifest 顺序复现生产 Router 的 provider-key scoped centroid 决策。
func replayRouting(samples []encodedSample, gate routingGate, dimension int) replayReport {
	segments := map[string][]replaySegment{}
	result := replayReport{}
	for index, sample := range samples {
		key := sample.definition.ProviderKey
		candidates := segments[key]
		if len(candidates) == 0 {
			segments[key] = []replaySegment{{speakerID: sample.definition.SpeakerID, vectors: []speakerservice.TrackVector{{TrackID: sample.definition.ID, FirstFinalSeq: int64(index), Embedding: sample.embedding}}}}
			result.SegmentCount++
			continue
		}
		topIndex, topScore, runnerUp := -1, math.Inf(-1), math.Inf(-1)
		for candidateIndex, candidate := range candidates {
			centroid, err := speakerservice.RecomputeCentroid(candidate.vectors, dimension)
			if err != nil {
				continue
			}
			score, err := speakerservice.CosineSimilarity(sample.embedding, centroid, dimension)
			if err != nil {
				continue
			}
			if score > topScore {
				topIndex, runnerUp, topScore = candidateIndex, topScore, score
			} else if score > runnerUp {
				runnerUp = score
			}
		}
		marginPassed := len(candidates) == 1 || topScore-runnerUp >= gate.MinMargin
		if topIndex >= 0 && topScore >= gate.MinScore && marginPassed {
			if candidates[topIndex].speakerID != sample.definition.SpeakerID {
				result.FalseMerge++
			}
			candidates[topIndex].vectors = append(candidates[topIndex].vectors, speakerservice.TrackVector{TrackID: sample.definition.ID, FirstFinalSeq: int64(index), Embedding: sample.embedding})
			segments[key] = candidates
			continue
		}
		for _, candidate := range candidates {
			if candidate.speakerID == sample.definition.SpeakerID {
				result.FalseSplit++
				break
			}
		}
		segments[key] = append(candidates, replaySegment{speakerID: sample.definition.SpeakerID, vectors: []speakerservice.TrackVector{{TrackID: sample.definition.ID, FirstFinalSeq: int64(index), Embedding: sample.embedding}}})
		result.SegmentCount++
	}
	return result
}

// calibrateRoutingGate 用 selection 最近邻间隔选阈值，并只在 validation 上统计通过率。
func calibrateRoutingGate(samples []encodedSample, pairs []pairResult) routingGate {
	selection := nearestNeighborScores(samples, pairs, "selection")
	gate := routingGate{SelectionMinTopSame: math.Inf(1), SelectionMaxTopDifferent: math.Inf(-1), SelectionMinMargin: math.Inf(1)}
	for _, score := range selection {
		gate.SelectionMinTopSame = math.Min(gate.SelectionMinTopSame, score.topSame)
		gate.SelectionMaxTopDifferent = math.Max(gate.SelectionMaxTopDifferent, score.topDifferent)
		gate.SelectionMinMargin = math.Min(gate.SelectionMinMargin, score.topSame-score.topDifferent)
	}
	gate.MinScore = (gate.SelectionMinTopSame + gate.SelectionMaxTopDifferent) / 2
	gate.MinMargin = gate.SelectionMinMargin / 2
	validation := nearestNeighborScores(samples, pairs, "validation")
	gate.ValidationCount = len(validation)
	for _, score := range validation {
		if score.topSame >= gate.MinScore && score.topSame-score.topDifferent >= gate.MinMargin {
			gate.ValidationPassed++
		}
	}
	return gate
}

type neighborScore struct {
	topSame      float64
	topDifferent float64
}

// nearestNeighborScores 返回每条样本在同分组内最相近的同人和异人分数。
func nearestNeighborScores(samples []encodedSample, pairs []pairResult, split string) []neighborScore {
	byID := map[string]neighborScore{}
	for _, sample := range samples {
		if sample.definition.Split == split {
			byID[sample.definition.ID] = neighborScore{topSame: math.Inf(-1), topDifferent: math.Inf(-1)}
		}
	}
	for _, pair := range pairs {
		if pair.Split != split {
			continue
		}
		for _, id := range []string{pair.LeftID, pair.RightID} {
			score := byID[id]
			if pair.Same {
				score.topSame = math.Max(score.topSame, pair.Score)
			} else {
				score.topDifferent = math.Max(score.topDifferent, pair.Score)
			}
			byID[id] = score
		}
	}
	result := make([]neighborScore, 0, len(byID))
	for _, score := range byID {
		if !math.IsInf(score.topSame, 0) && !math.IsInf(score.topDifferent, 0) {
			result = append(result, score)
		}
	}
	return result
}

// summarizePairs 汇总指定分组及同异人类型的有限分数。
func summarizePairs(pairs []pairResult, split string, same bool) distribution {
	values := make([]float64, 0)
	for _, pair := range pairs {
		if pair.Split == split && pair.Same == same {
			values = append(values, pair.Score)
		}
	}
	result := distribution{Count: len(values)}
	if len(values) == 0 {
		return result
	}
	result.Min, result.Max = values[0], values[0]
	for _, value := range values {
		result.Min = math.Min(result.Min, value)
		result.Max = math.Max(result.Max, value)
		result.Mean += value
	}
	result.Mean /= float64(len(values))
	return result
}

// loadAssets 读取当前平台锁定的 CAM++ 和 ONNX Runtime 资源身份。
func loadAssets(path string) (assets.VoiceModelAsset, assets.Asset, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return assets.VoiceModelAsset{}, assets.Asset{}, err
	}
	definition, err := assets.ParseManifest(content)
	if err != nil {
		return assets.VoiceModelAsset{}, assets.Asset{}, err
	}
	model, err := definition.SelectVoiceModel("campplus")
	if err != nil {
		return assets.VoiceModelAsset{}, assets.Asset{}, err
	}
	runtimeAsset, err := definition.Select("onnxruntime", runtime.GOOS, runtime.GOARCH)
	return model, runtimeAsset, err
}

// requireEOF 拒绝清单根对象后的额外 JSON 值。
func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("清单尾部不合法")
	}
	return nil
}
