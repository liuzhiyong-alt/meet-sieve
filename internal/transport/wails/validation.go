package wails

import (
	"fmt"

	"github.com/google/uuid"
)

// requireUUID 校验外部边界只能传入规范 UUID。
func requireUUID(name string, value string) error {
	if uuid.Validate(value) != nil {
		return fmt.Errorf("%s 无效", name)
	}
	return nil
}
