// Command verifyvoiceprofile 阻止缺少正式校准档案的发布构建。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/assets"
)

// main 校验 profile 的模型绑定及其校准记录存在性。
func main() {
	profilePath := flag.String("profile", "models/voice-matching-profile.json", "正式匹配档案路径")
	assetsPath := flag.String("assets", "third_party/assets.lock.json", "第三方资源锁路径")
	flag.Parse()
	if err := verify(*profilePath, *assetsPath); err != nil {
		fmt.Fprintln(os.Stderr, "正式声纹档案校验失败："+err.Error())
		os.Exit(1)
	}
	fmt.Println("正式声纹档案校验通过")
}

// verify 严格解析资源锁与 profile，并确认审计记录是仓库内普通文件。
func verify(profilePath string, assetsPath string) error {
	assetContent, err := os.ReadFile(assetsPath)
	if err != nil {
		return fmt.Errorf("读取资源锁失败：%w", err)
	}
	manifest, err := assets.ParseManifest(assetContent)
	if err != nil {
		return err
	}
	model, err := manifest.SelectVoiceModel("campplus")
	if err != nil {
		return err
	}
	profileContent, err := os.ReadFile(profilePath)
	if err != nil {
		return fmt.Errorf("读取正式档案失败：%w", err)
	}
	expected := speakerdomain.ModelIdentity{
		ID: model.ModelID, Version: model.Version, SHA256: model.ModelSHA256, Dimension: model.EmbeddingDimension,
	}
	profile, err := speakerdomain.ParseMatchingProfile(profileContent, expected)
	if err != nil {
		return err
	}
	recordPath := filepath.Clean(profile.CalibrationRecord)
	recordInfo, err := os.Stat(recordPath)
	if err != nil {
		return fmt.Errorf("校准记录不存在：%w", err)
	}
	if !recordInfo.Mode().IsRegular() || recordInfo.Size() == 0 {
		return fmt.Errorf("校准记录不是非空普通文件")
	}
	return nil
}
