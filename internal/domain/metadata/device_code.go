package metadata

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
)

const deviceCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// DeviceCode 是展示在会议编号中的稳定四位设备标识。
type DeviceCode string

// ParseDeviceCode 校验并创建只含已登记字符集的四位设备码。
func ParseDeviceCode(value string) (DeviceCode, error) {
	if len(value) != 4 {
		return "", errors.New("设备码必须是四位")
	}
	for _, character := range value {
		if !strings.ContainsRune(deviceCodeAlphabet, character) {
			return "", fmt.Errorf("设备码包含非法字符 %q", character)
		}
	}
	return DeviceCode(value), nil
}

// String 返回可用于展示和数据库写入的设备码。
func (code DeviceCode) String() string {
	return string(code)
}

// FixedRandomSource 为确定性设备码测试提供每一位字符集索引。
type FixedRandomSource []int

// DeviceCodeGenerator 根据预设字符集索引生成设备码。
type DeviceCodeGenerator struct {
	values []int
	index  int
}

// SecureDeviceCodeGenerator 使用密码学安全随机源生成生产设备码。
type SecureDeviceCodeGenerator struct{}

// NewSecureDeviceCodeGenerator 创建生产环境使用的设备码生成器。
func NewSecureDeviceCodeGenerator() *SecureDeviceCodeGenerator {
	return &SecureDeviceCodeGenerator{}
}

// New 返回新的密码学安全随机设备码。
func (generator *SecureDeviceCodeGenerator) New() (DeviceCode, error) {
	return NewRandomDeviceCode()
}

// NewDeviceCodeGenerator 创建用于测试的确定性设备码生成器。
func NewDeviceCodeGenerator(source FixedRandomSource) *DeviceCodeGenerator {
	return &DeviceCodeGenerator{values: append([]int(nil), source...)}
}

// New 返回下一个四位设备码；索引不足或越界时返回错误。
func (generator *DeviceCodeGenerator) New() (DeviceCode, error) {
	if len(generator.values)-generator.index < 4 {
		return "", errors.New("设备码随机源不足")
	}
	characters := make([]byte, 4)
	for index := range characters {
		value := generator.values[generator.index]
		generator.index++
		if value < 0 || value >= len(deviceCodeAlphabet) {
			return "", errors.New("设备码随机索引越界")
		}
		characters[index] = deviceCodeAlphabet[value]
	}
	return ParseDeviceCode(string(characters))
}

// NewRandomDeviceCode 使用密码学安全随机源生成新的四位设备码。
func NewRandomDeviceCode() (DeviceCode, error) {
	return newDeviceCodeFromReader(rand.Reader)
}

// newDeviceCodeFromReader 从随机字节读取器生成设备码，供生产实现复用安全随机源。
func newDeviceCodeFromReader(reader io.Reader) (DeviceCode, error) {
	characters := make([]byte, 4)
	upperBound := big.NewInt(int64(len(deviceCodeAlphabet)))
	for index := range characters {
		value, err := rand.Int(reader, upperBound)
		if err != nil {
			return "", fmt.Errorf("读取设备码随机数：%w", err)
		}
		characters[index] = deviceCodeAlphabet[value.Int64()]
	}
	return ParseDeviceCode(string(characters))
}
