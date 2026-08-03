package resource

import (
	"bytes"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"meet-sieve/internal/infra/apperr"
)

const MaxAttachmentBytes int64 = 500 * 1024 * 1024

var blockedExtensions = map[string]struct{}{
	".exe": {}, ".msi": {}, ".msix": {}, ".appx": {}, ".dmg": {}, ".pkg": {}, ".deb": {}, ".rpm": {},
	".dll": {}, ".so": {}, ".dylib": {}, ".sys": {}, ".com": {}, ".scr": {},
	".bat": {}, ".cmd": {}, ".ps1": {},
}

var blockedMediaTypes = map[string]struct{}{
	"application/x-msdownload": {}, "application/x-msdos-program": {}, "application/x-executable": {},
	"application/x-mach-binary": {}, "application/x-sharedlib": {}, "application/vnd.microsoft.portable-executable": {},
}

// FilePolicy 按文件名、声明 MIME 和头部 magic 拒绝主动可执行内容。
type FilePolicy struct{}

// FileValidation 是落盘后可用于内部命名与数据库投影的结果。
type FileValidation struct {
	DisplayName string
	Extension   string
	MediaType   string
}

// NewFilePolicy 创建固定安全策略，不执行、解压或内联附件。
func NewFilePolicy() *FilePolicy { return &FilePolicy{} }

// Validate 组合文件名、MIME 和 magic 给出最终接收决策。
func (policy *FilePolicy) Validate(originalName string, declaredMediaType string, head []byte) (FileValidation, error) {
	name, extension, err := validateDisplayFilename(originalName)
	if err != nil {
		return FileValidation{}, err
	}
	if _, blocked := blockedExtensions[extension]; blocked || hasExecutableMagic(head) || hasShebang(head) {
		return FileValidation{}, blockedType()
	}
	declared, err := normalizeMediaType(declaredMediaType)
	if err != nil {
		return FileValidation{}, blockedType()
	}
	detected := http.DetectContentType(head)
	if _, blocked := blockedMediaTypes[declared]; blocked {
		return FileValidation{}, blockedType()
	}
	if _, blocked := blockedMediaTypes[detected]; blocked || obviousMediaConflict(declared, detected) {
		return FileValidation{}, blockedType()
	}
	mediaType := detected
	if detected == "application/octet-stream" && declared != "" {
		mediaType = declared
	}
	return FileValidation{DisplayName: name, Extension: extension, MediaType: mediaType}, nil
}

// ValidateDeclaredSize 要求附件非空且不超过精确 500 MiB。
func ValidateDeclaredSize(size int64) error {
	if size <= 0 || size > MaxAttachmentBytes {
		return apperr.Biz(apperr.CodeAttachmentTooLarge, apperr.WithOp("resource.upload.declared_size"))
	}
	return nil
}

// validateDisplayFilename 拒绝路径、NUL、Windows 保留名和无效显示名。
func validateDisplayFilename(value string) (string, string, error) {
	if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') || strings.ContainsAny(value, `/\`) {
		return "", "", blockedType()
	}
	name := strings.Trim(strings.TrimSpace(value), " .")
	if name == "" || name != filepath.Base(name) {
		return "", "", blockedType()
	}
	extension := strings.ToLower(filepath.Ext(name))
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if isWindowsReservedName(stem) {
		return "", "", blockedType()
	}
	return name, extension, nil
}

// normalizeMediaType 只保留小写 media type，不信任客户端参数。
func normalizeMediaType(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	mediaType, _, err := mime.ParseMediaType(value)
	return strings.ToLower(mediaType), err
}

// obviousMediaConflict 拒绝声明为图片/PDF 但 magic 明确不一致的内容。
func obviousMediaConflict(declared string, detected string) bool {
	if declared == "" || detected == "application/octet-stream" {
		return false
	}
	if strings.HasPrefix(declared, "image/") {
		return !strings.HasPrefix(detected, "image/")
	}
	if declared == "application/pdf" {
		return detected != "application/pdf"
	}
	return false
}

// hasExecutableMagic 识别 PE、ELF、Mach-O 和 Fat Mach-O 头。
func hasExecutableMagic(head []byte) bool {
	if len(head) >= 2 && bytes.Equal(head[:2], []byte{'M', 'Z'}) {
		return true
	}
	if len(head) >= 4 && bytes.Equal(head[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		return true
	}
	if len(head) < 4 {
		return false
	}
	magic := [4]byte{head[0], head[1], head[2], head[3]}
	switch magic {
	case [4]byte{0xfe, 0xed, 0xfa, 0xce}, [4]byte{0xfe, 0xed, 0xfa, 0xcf},
		[4]byte{0xce, 0xfa, 0xed, 0xfe}, [4]byte{0xcf, 0xfa, 0xed, 0xfe},
		[4]byte{0xca, 0xfe, 0xba, 0xbe}, [4]byte{0xbe, 0xba, 0xfe, 0xca}:
		return true
	default:
		return false
	}
}

// hasShebang 拒绝即使被改名为文本的明显可执行脚本。
func hasShebang(head []byte) bool {
	return len(head) >= 2 && head[0] == '#' && head[1] == '!'
}

// isWindowsReservedName 在 macOS 上也预先拒绝 Windows 保留设备名。
func isWindowsReservedName(stem string) bool {
	name := strings.ToUpper(strings.TrimSpace(stem))
	if name == "CON" || name == "PRN" || name == "AUX" || name == "NUL" {
		return true
	}
	for index := 1; index <= 9; index++ {
		if name == "COM"+string(rune('0'+index)) || name == "LPT"+string(rune('0'+index)) {
			return true
		}
	}
	return false
}

// blockedType 构建不包含原始文件名或 magic 的安全错误。
func blockedType() error {
	return apperr.Biz(apperr.CodeAttachmentTypeBlocked, apperr.WithOp("resource.file_policy"))
}
