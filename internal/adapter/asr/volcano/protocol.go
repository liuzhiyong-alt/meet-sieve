package volcano

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	protocolVersion = 1
	headerWords     = 1

	messageFullClientRequest  = 0x1
	messageAudioOnlyRequest   = 0x2
	messageFullServerResponse = 0x9
	messageServerACK          = 0xB
	messageServerError        = 0xF

	flagNoSequence       = 0x0
	flagPositiveSequence = 0x1
	flagLastPacket       = 0x2
	flagNegativeSequence = 0x3

	serializationNone = 0x0
	serializationJSON = 0x1
	compressionNone   = 0x0
	compressionGzip   = 0x1
)

// ServerFrame 是完成 wire 校验和解压后的火山服务端帧。
type ServerFrame struct {
	MessageType byte
	Sequence    int32
	Payload     []byte
	ErrorCode   uint32
}

// EncodeFullClientRequest 编码 gzip JSON 初始化请求。
func EncodeFullClientRequest(payload []byte) ([]byte, error) {
	compressed, err := gzipPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("压缩 full client request 失败：%w", err)
	}
	return encodePayloadFrame(messageFullClientRequest, flagNoSequence, serializationJSON, compressionGzip, nil, compressed), nil
}

// EncodeAudioOnlyRequest 编码优化流式端点的无序号 PCM 帧；last 使用空尾包结束输入。
func EncodeAudioOnlyRequest(last bool, pcm []byte) ([]byte, error) {
	if (!last && len(pcm) == 0) || (last && len(pcm) != 0) {
		return nil, fmt.Errorf("audio only request 参数无效")
	}
	compressed, err := gzipPayload(pcm)
	if err != nil {
		return nil, fmt.Errorf("压缩 audio only request 失败：%w", err)
	}
	flag := byte(flagNoSequence)
	if last {
		flag = flagLastPacket
	}
	return encodePayloadFrame(messageAudioOnlyRequest, flag, serializationNone, compressionGzip, nil, compressed), nil
}

// DecodeServerFrame 严格解析 full response、ACK 和 error，未知类型或截断长度直接失败。
func DecodeServerFrame(data []byte) (ServerFrame, error) {
	header, body, err := decodeHeader(data)
	if err != nil {
		return ServerFrame{}, err
	}
	frame := ServerFrame{MessageType: header.messageType}
	if err = validateServerFlags(header); err != nil {
		return ServerFrame{}, err
	}
	switch header.messageType {
	case messageFullServerResponse:
		if header.flags == flagNoSequence {
			frame.Payload, err = decodeUnsequencedPayload(body, header.compression)
		} else {
			frame.Sequence, frame.Payload, err = decodeSequencedPayload(body, header.compression)
		}
	case messageServerACK:
		frame.Sequence, frame.Payload, err = decodeACK(body, header.compression)
	case messageServerError:
		frame.ErrorCode, frame.Payload, err = decodeErrorPayload(body, header.compression)
	default:
		err = fmt.Errorf("不支持的火山服务端消息类型：0x%x", header.messageType)
	}
	if err != nil {
		return ServerFrame{}, err
	}
	return frame, nil
}

// validateServerFlags 拒绝与消息类型不匹配的 sequence flag，避免错位解析正文。
func validateServerFlags(header frameHeader) error {
	switch header.messageType {
	case messageFullServerResponse:
		if header.flags != flagNoSequence && header.flags != flagPositiveSequence && header.flags != flagNegativeSequence {
			return fmt.Errorf("火山响应 sequence flag 不兼容")
		}
	case messageServerACK:
		if header.flags != flagPositiveSequence && header.flags != flagNegativeSequence && header.flags != flagNoSequence {
			return fmt.Errorf("火山 ACK flag 不兼容")
		}
	case messageServerError:
		if header.flags != flagNoSequence {
			return fmt.Errorf("火山错误响应 flag 不兼容")
		}
	}
	return nil
}

// decodeUnsequencedPayload 解析优化流式端点不携带 sequence 的响应正文。
func decodeUnsequencedPayload(body []byte, compression byte) ([]byte, error) {
	payload, err := decodeSizedPayload(body)
	if err != nil {
		return nil, err
	}
	return decompressPayload(payload, compression)
}

type frameHeader struct {
	messageType byte
	flags       byte
	compression byte
}

// decodeHeader 校验 Seed V1 四字节基础 Header，拒绝未知扩展与保留位。
func decodeHeader(data []byte) (frameHeader, []byte, error) {
	if len(data) < 4 {
		return frameHeader{}, nil, fmt.Errorf("火山协议 Header 截断")
	}
	version, words := data[0]>>4, data[0]&0x0F
	if version != protocolVersion || words != headerWords || data[3] != 0 {
		return frameHeader{}, nil, fmt.Errorf("火山协议 Header 不兼容")
	}
	serialization, compression := data[2]>>4, data[2]&0x0F
	if serialization != serializationNone && serialization != serializationJSON {
		return frameHeader{}, nil, fmt.Errorf("火山协议序列化方式不兼容")
	}
	if compression != compressionNone && compression != compressionGzip {
		return frameHeader{}, nil, fmt.Errorf("火山协议压缩方式不兼容")
	}
	return frameHeader{messageType: data[1] >> 4, flags: data[1] & 0x0F, compression: compression}, data[4:], nil
}

// decodeSequencedPayload 解析 sequence、payload size 与 payload。
func decodeSequencedPayload(body []byte, compression byte) (int32, []byte, error) {
	if len(body) < 8 {
		return 0, nil, fmt.Errorf("火山响应 sequence 或长度截断")
	}
	sequence := int32(binary.BigEndian.Uint32(body[:4]))
	payload, err := decodeSizedPayload(body[4:])
	if err != nil {
		return 0, nil, err
	}
	payload, err = decompressPayload(payload, compression)
	return sequence, payload, err
}

// decodeACK 兼容只包含 sequence 的 ACK；存在 payload 时仍严格校验其长度。
func decodeACK(body []byte, compression byte) (int32, []byte, error) {
	if len(body) < 4 {
		return 0, nil, fmt.Errorf("火山 ACK sequence 截断")
	}
	sequence := int32(binary.BigEndian.Uint32(body[:4]))
	if len(body) == 4 {
		return sequence, nil, nil
	}
	payload, err := decodeSizedPayload(body[4:])
	if err != nil {
		return 0, nil, err
	}
	payload, err = decompressPayload(payload, compression)
	return sequence, payload, err
}

// decodeErrorPayload 解析稳定数值错误码，但调用方不得向 UI 暴露原始正文。
func decodeErrorPayload(body []byte, compression byte) (uint32, []byte, error) {
	if len(body) < 8 {
		return 0, nil, fmt.Errorf("火山错误响应截断")
	}
	code := binary.BigEndian.Uint32(body[:4])
	payload, err := decodeSizedPayload(body[4:])
	if err != nil {
		return 0, nil, err
	}
	payload, err = decompressPayload(payload, compression)
	return code, payload, err
}

// decodeSizedPayload 确保 payload size 与实际字节逐字节一致。
func decodeSizedPayload(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("火山响应 payload size 截断")
	}
	size := int(binary.BigEndian.Uint32(data[:4]))
	if size < 0 || len(data[4:]) != size {
		return nil, fmt.Errorf("火山响应 payload size 不匹配")
	}
	return append([]byte(nil), data[4:]...), nil
}

// encodePayloadFrame 组装基础 Header、可选 sequence、payload size 与 payload。
func encodePayloadFrame(messageType byte, flags byte, serialization byte, compression byte, prefix []byte, payload []byte) []byte {
	frame := make([]byte, 4+len(prefix)+4+len(payload))
	frame[0] = protocolVersion<<4 | headerWords
	frame[1] = messageType<<4 | flags
	frame[2] = serialization<<4 | compression
	copy(frame[4:], prefix)
	binary.BigEndian.PutUint32(frame[4+len(prefix):], uint32(len(payload)))
	copy(frame[8+len(prefix):], payload)
	return frame
}

// gzipPayload 使用官方 Seed 协议指定的 gzip 压缩 payload。
func gzipPayload(payload []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(payload); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// decompressPayload 按 Header 声明解压，禁止把未知压缩格式当原始正文处理。
func decompressPayload(payload []byte, compression byte) ([]byte, error) {
	if compression == compressionNone {
		return payload, nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("创建 gzip reader 失败：%w", err)
	}
	defer reader.Close()
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("解压火山 payload 失败：%w", err)
	}
	return decoded, nil
}
