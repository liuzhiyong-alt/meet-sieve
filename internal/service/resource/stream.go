package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"meet-sieve/internal/infra/apperr"
)

const (
	copyBufferBytes = 1024 * 1024
	magicHeadBytes  = 8 * 1024
)

// StreamResult 是固定内存流式复制的字节数、SHA-256 和 magic 头。
type StreamResult struct {
	SizeBytes int64
	SHA256    string
	Head      []byte
}

// copyExactAndHash 最多读取 limit+1，同时校验声明大小并计算 SHA-256。
func copyExactAndHash(ctx context.Context, destination io.Writer, source io.Reader, declaredSize int64, limit int64) (StreamResult, error) {
	return copyExactAndHashWithProgress(ctx, destination, source, declaredSize, limit, nil)
}

// copyExactAndHashWithProgress 在每跨过 32 MiB 边界时调用磁盘余量等进度检查。
func copyExactAndHashWithProgress(
	ctx context.Context,
	destination io.Writer,
	source io.Reader,
	declaredSize int64,
	limit int64,
	check func(int64) error,
) (StreamResult, error) {
	return copyExactAndHashWithCallbacks(ctx, destination, source, declaredSize, limit, check, nil)
}

// copyExactAndHashWithCallbacks 分离逐块进度通知与每 32 MiB 的磁盘检查。
func copyExactAndHashWithCallbacks(
	ctx context.Context,
	destination io.Writer,
	source io.Reader,
	declaredSize int64,
	limit int64,
	check func(int64) error,
	progress func(int64),
) (StreamResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	hasher := sha256.New()
	head := make([]byte, 0, magicHeadBytes)
	buffer := make([]byte, copyBufferBytes)
	limited := io.LimitReader(source, limit+1)
	written := int64(0)
	nextCheck := streamDiskCheckBytes
	for {
		if err := ctx.Err(); err != nil {
			return StreamResult{}, err
		}
		count, readErr := limited.Read(buffer)
		if count > 0 {
			written += int64(count)
			if written > limit {
				return StreamResult{}, apperrTooLarge()
			}
			chunk := buffer[:count]
			if len(head) < magicHeadBytes {
				remaining := magicHeadBytes - len(head)
				if remaining > len(chunk) {
					remaining = len(chunk)
				}
				head = append(head, chunk[:remaining]...)
			}
			if _, err := hasher.Write(chunk); err != nil {
				return StreamResult{}, fmt.Errorf("计算附件哈希：%w", err)
			}
			if _, err := destination.Write(chunk); err != nil {
				return StreamResult{}, fmt.Errorf("写入附件流：%w", err)
			}
			if progress != nil {
				progress(written)
			}
			if check != nil && written >= nextCheck {
				if err := check(written); err != nil {
					return StreamResult{}, err
				}
				for nextCheck <= written {
					nextCheck += streamDiskCheckBytes
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return StreamResult{}, fmt.Errorf("读取附件流：%w", readErr)
		}
	}
	if written != declaredSize {
		return StreamResult{}, fmt.Errorf("附件实际大小 %d 与声明大小 %d 不一致", written, declaredSize)
	}
	return StreamResult{SizeBytes: written, SHA256: hex.EncodeToString(hasher.Sum(nil)), Head: head}, nil
}

// apperrTooLarge 统一返回实际内容超过 limit 的安全错误。
func apperrTooLarge() error {
	return apperr.Biz(apperr.CodeAttachmentTooLarge, apperr.WithOp("resource.upload.actual_size"))
}
