package main

import (
	"bytes"
	"encoding/binary"
)

const (
	HDHomeRunDiscoveryUDPPort = 65001
	TCPPort                   = HDHomeRunDiscoveryUDPPort
	UDPReadTimeout            = 500 // milliseconds
	UDPReadBufferSize         = 4096
	ReconnectInterval         = 3 // seconds
)

// MessageCodec encodes and decodes messages to/from a byte stream
type MessageCodec struct {
	msgBuffer            bytes.Buffer
	lengthBytesRemaining int
	msgBytesRemaining    int
}

// NewMessageCodec creates a new message codec
func NewMessageCodec() *MessageCodec {
	return &MessageCodec{
		lengthBytesRemaining: 2,
		msgBytesRemaining:    0,
	}
}

// Encode encodes data with a 2-byte big-endian length prefix
func (mc *MessageCodec) Encode(data []byte) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, uint16(len(data)))
	buf.Write(data)
	return buf.Bytes()
}

// Decode decodes a stream of bytes and calls the callback for each complete message
func (mc *MessageCodec) Decode(data []byte, callback func([]byte)) {
	i := 0

	for {
		// Read length bytes if we need them
		for mc.lengthBytesRemaining > 0 {
			if i >= len(data) {
				return
			}

			// The length is big-endian
			mc.lengthBytesRemaining--
			mc.msgBytesRemaining |= int(data[i]) << (mc.lengthBytesRemaining * 8)
			i++
		}

		if mc.msgBytesRemaining > 0 {
			// Read message bytes
			remaining := len(data) - i
			if remaining == 0 {
				return
			}

			readLen := remaining
			if readLen > mc.msgBytesRemaining {
				readLen = mc.msgBytesRemaining
			}

			mc.msgBuffer.Write(data[i : i+readLen])
			mc.msgBytesRemaining -= readLen
			i += readLen

			if mc.msgBytesRemaining > 0 {
				return
			}
		}

		// We have a complete message
		message := mc.msgBuffer.Bytes()
		msgCopy := make([]byte, len(message))
		copy(msgCopy, message)

		// Reset state
		mc.msgBuffer.Reset()
		mc.lengthBytesRemaining = 2
		mc.msgBytesRemaining = 0

		// Call the callback
		callback(msgCopy)
	}
}
