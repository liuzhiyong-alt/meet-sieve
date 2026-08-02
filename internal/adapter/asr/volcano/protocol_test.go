package volcano

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestEncodeFullClientRequest 验证初始化包使用 Seed V1、JSON、gzip 和大端长度。
func TestEncodeFullClientRequest(t *testing.T) {
	payload := []byte(`{"user":{"uid":"fixture"}}`)
	frame, err := EncodeFullClientRequest(payload)
	if err != nil {
		t.Fatalf("编码初始化包失败：%v", err)
	}
	if !bytes.Equal(frame[:4], []byte{0x11, 0x10, 0x11, 0x00}) {
		t.Fatalf("初始化 Header 错误：%x", frame[:4])
	}
	if int(binary.BigEndian.Uint32(frame[4:8])) != len(frame[8:]) {
		t.Fatal("初始化 payload size 必须使用大端且与压缩正文一致")
	}
}

// TestEncodeAudioOnlyRequest 验证最后一包使用负 sequence 且不改变 PCM 内容语义。
func TestEncodeAudioOnlyRequest(t *testing.T) {
	frame, err := EncodeAudioOnlyRequest(7, true, []byte{1, 0, 2, 0})
	if err != nil {
		t.Fatalf("编码尾音频包失败：%v", err)
	}
	if !bytes.Equal(frame[:4], []byte{0x11, 0x23, 0x01, 0x00}) || int32(binary.BigEndian.Uint32(frame[4:8])) != -7 {
		t.Fatalf("尾音频 Header 或 sequence 错误：%x", frame[:8])
	}
}

// TestDecodeServerFrame 验证 full response 解压及未知/截断协议失败。
func TestDecodeServerFrame(t *testing.T) {
	payload := []byte(`{"result":{"text":"fixture"}}`)
	compressed, err := gzipPayload(payload)
	if err != nil {
		t.Fatalf("准备压缩响应失败：%v", err)
	}
	sequence := make([]byte, 4)
	binary.BigEndian.PutUint32(sequence, 2)
	wire := encodePayloadFrame(messageFullServerResponse, flagPositiveSequence, serializationJSON, compressionGzip, sequence, compressed)
	frame, err := DecodeServerFrame(wire)
	if err != nil {
		t.Fatalf("解析服务端响应失败：%v", err)
	}
	if frame.Sequence != 2 || !bytes.Equal(frame.Payload, payload) {
		t.Fatalf("服务端响应内容错误：%+v", frame)
	}
	if _, err = DecodeServerFrame(wire[:6]); err == nil {
		t.Fatal("截断响应必须失败")
	}
	wire[1] = 0xE0
	if _, err = DecodeServerFrame(wire); err == nil {
		t.Fatal("未知消息类型必须失败")
	}
}
