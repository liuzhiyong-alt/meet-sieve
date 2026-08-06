// Command calibratevoice 使用真实标注 WAV 生成声纹匹配档案和校准记录。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	voiceonnx "meet-sieve/internal/adapter/voice/onnx"
	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/assets"
	"meet-sieve/internal/service/speaker/calibration"
)

// main 解析显式资源路径并运行不允许降级的正式校准。
func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "声纹校准失败："+err.Error())
		os.Exit(1)
	}
}

// run 加载锁定模型、执行校准，并把 profile 作为最后一个原子产物写入。
func run(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("calibratevoice", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "", "真实标注校准清单路径")
	assetsPath := flags.String("assets", "third_party/assets.lock.json", "第三方资源锁路径")
	modelPath := flags.String("model", "", "已安装且与资源锁匹配的 CAM++ model.onnx 路径")
	runtimePath := flags.String("runtime", "", "已下载且与资源锁匹配的 ONNX Runtime 动态库路径")
	profilePath := flags.String("profile-out", "models/voice-matching-profile.json", "正式匹配档案输出路径")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *manifestPath == "" || *modelPath == "" || *runtimePath == "" {
		return fmt.Errorf("必须显式提供 -manifest、-model 和 -runtime")
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

	manifest, err := loadCalibrationManifest(*manifestPath)
	if err != nil {
		return err
	}
	result, err := calibration.Run(ctx, manifest, filepath.Dir(*manifestPath), encoder)
	if err != nil {
		if result.Profile.ProfileID != "" {
			return fmt.Errorf("%w；metrics=%+v", err, result.Metrics)
		}
		return err
	}
	profile, err := json.MarshalIndent(result.Profile, "", "  ")
	if err != nil {
		return fmt.Errorf("编码正式匹配档案失败：%w", err)
	}
	profile = append(profile, '\n')
	if _, err := speakerdomain.ParseMatchingProfile(profile, result.Profile.Model); err != nil {
		return fmt.Errorf("回读正式匹配档案失败：%w", err)
	}
	record := []byte(buildMarkdownRecord(*manifestPath, result))
	if err := writeAtomic(manifest.CalibrationRecord, record); err != nil {
		return fmt.Errorf("写入校准记录失败：%w", err)
	}
	if err := writeAtomic(*profilePath, profile); err != nil {
		return fmt.Errorf("写入正式匹配档案失败：%w", err)
	}
	fmt.Printf("声纹校准通过：%s\n", *profilePath)
	return nil
}

// loadAssets 严格读取资源锁并选择当前平台 CAM++ 与 ONNX Runtime。
func loadAssets(path string) (assets.VoiceModelAsset, assets.Asset, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return assets.VoiceModelAsset{}, assets.Asset{}, fmt.Errorf("读取资源锁失败：%w", err)
	}
	manifest, err := assets.ParseManifest(content)
	if err != nil {
		return assets.VoiceModelAsset{}, assets.Asset{}, err
	}
	model, err := manifest.SelectVoiceModel("campplus")
	if err != nil {
		return assets.VoiceModelAsset{}, assets.Asset{}, err
	}
	runtimeAsset, err := manifest.Select("onnxruntime", runtime.GOOS, runtime.GOARCH)
	return model, runtimeAsset, err
}

// loadCalibrationManifest 读取并严格解析真实音频清单。
func loadCalibrationManifest(path string) (calibration.Manifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return calibration.Manifest{}, fmt.Errorf("读取校准清单失败：%w", err)
	}
	return calibration.ParseManifest(content)
}

// buildMarkdownRecord 生成不包含原始音频和真实姓名的可审计校准记录。
func buildMarkdownRecord(manifestPath string, result calibration.Result) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# 声纹匹配校准记录\n\n")
	fmt.Fprintf(&builder, "- Profile ID：`%s`\n", result.Profile.ProfileID)
	fmt.Fprintf(&builder, "- 清单：`%s`\n", filepath.Clean(manifestPath))
	fmt.Fprintf(&builder, "- 模型：`%s@%s`\n", result.Profile.Model.ID, result.Profile.Model.Version)
	fmt.Fprintf(&builder, "- 模型 SHA-256：`%s`\n", result.Profile.Model.SHA256)
	fmt.Fprintf(&builder, "- 说话人数：%d\n", result.Metrics.SpeakerCount)
	fmt.Fprintf(&builder, "- 录入/评估样本：%d / %d\n", result.Metrics.EnrollmentCount, result.Metrics.EvaluationCount)
	fmt.Fprintf(&builder, "- 成员识别：正确 %d，误认 %d，拒识 %d\n", result.Metrics.IdentityCorrect,
		result.Metrics.IdentityFalseAccept, result.Metrics.IdentityFalseReject)
	fmt.Fprintf(&builder, "- 匿名聚类：误合并 %d，误拆分 %d\n", result.Metrics.ClusterFalseMerge, result.Metrics.ClusterFalseSplit)
	fmt.Fprintf(&builder, "- 成员阈值：min_score=%.6f，min_margin=%.6f\n", result.Profile.Identity.MinScore, result.Profile.Identity.MinMargin)
	fmt.Fprintf(&builder, "- 聚类阈值：min_score=%.6f，min_margin=%.6f\n\n", result.Profile.UnknownCluster.MinScore, result.Profile.UnknownCluster.MinMargin)
	builder.WriteString("## 样本审计\n\n")
	builder.WriteString("| speaker_id | session_id | role | duration_ms | sha256 | top | score | matched |\n")
	builder.WriteString("| --- | --- | --- | ---: | --- | --- | ---: | --- |\n")
	for _, sample := range result.Samples {
		fmt.Fprintf(&builder, "| %s | %s | %s | %d | `%s` | %s | %.6f | %t |\n",
			sample.SpeakerID, sample.SessionID, sample.Role, sample.DurationMS, sample.SHA256,
			sample.TopSpeakerID, sample.TopScore, sample.Matched)
	}
	return builder.String()
}

// writeAtomic 在同目录写临时文件后替换目标，避免留下半截 profile。
func writeAtomic(path string, content []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".voice-calibration-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
