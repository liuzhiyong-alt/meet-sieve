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

// TestEncodeAudioOnlyRequest 验证普通音频无 sequence，最后一包使用空尾包标记。
func TestEncodeAudioOnlyRequest(t *testing.T) {
	frame, err := EncodeAudioOnlyRequest(false, []byte{1, 0, 2, 0})
	if err != nil {
		t.Fatalf("编码普通音频包失败：%v", err)
	}
	if !bytes.Equal(frame[:4], []byte{0x11, 0x20, 0x01, 0x00}) {
		t.Fatalf("普通音频 Header 错误：%x", frame[:4])
	}
	last, err := EncodeAudioOnlyRequest(true, nil)
	if err != nil {
		t.Fatalf("编码尾音频包失败：%v", err)
	}
	if !bytes.Equal(last[:4], []byte{0x11, 0x22, 0x01, 0x00}) {
		t.Fatalf("尾音频 Header 错误：%x", last[:4])
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

// TestDecodeUnsequencedServerFrame 验证优化流式端点的无 sequence 响应可被解析。
func TestDecodeUnsequencedServerFrame(t *testing.T) {
	payload := []byte(`{"result":{"text":"fixture"}}`)
	compressed, err := gzipPayload(payload)
	if err != nil {
		t.Fatalf("准备压缩响应失败：%v", err)
	}
	wire := encodePayloadFrame(messageFullServerResponse, flagNoSequence, serializationJSON, compressionGzip, nil, compressed)
	frame, err := DecodeServerFrame(wire)
	if err != nil || frame.Sequence != 0 || !bytes.Equal(frame.Payload, payload) {
		t.Fatalf("无 sequence 响应解析错误：frame=%+v err=%v", frame, err)
	}
}
