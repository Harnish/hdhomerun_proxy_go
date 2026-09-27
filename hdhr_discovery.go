package main

import (
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"strings"
)

// Binary HDHomeRun discovery protocol (UDP 65001), as implemented by
// libhdhomerun (hdhomerun_pkt.h). A packet is:
//
//	type (u16 BE) | payload length (u16 BE) | TLV payload | CRC32 (u32 LE)
//
// The CRC is IEEE CRC-32 over header + payload. TLV lengths are a single byte
// when < 128, otherwise two bytes: (len & 0x7F)|0x80, then len >> 7.
const (
	hdhrTypeDiscoverReq = 0x0002
	hdhrTypeDiscoverRpy = 0x0003

	hdhrTagDeviceType    = 0x01
	hdhrTagDeviceID      = 0x02
	hdhrTagTunerCount    = 0x10
	hdhrTagLineupURL     = 0x27
	hdhrTagBaseURL       = 0x2A
	hdhrTagDeviceAuthStr = 0x2B

	hdhrDeviceTypeTuner    = 0x00000001
	hdhrDeviceTypeWildcard = 0xFFFFFFFF
	hdhrDeviceIDWildcard   = 0xFFFFFFFF
)

// hdhrDevice is the identity advertised in a discovery reply.
type hdhrDevice struct {
	DeviceID   string // 8 hex chars
	DeviceAuth string
	TunerCount int
	BaseURL    string // e.g. http://10.0.0.5:5004
}

// parseDiscoverRequest returns the TLVs of a valid discover request, or false.
func parseDiscoverRequest(pkt []byte) (map[byte][]byte, bool) {
	if len(pkt) < 8 {
		return nil, false
	}
	if binary.BigEndian.Uint16(pkt[0:2]) != hdhrTypeDiscoverReq {
		return nil, false
	}
	n := int(binary.BigEndian.Uint16(pkt[2:4]))
	if 4+n+4 > len(pkt) {
		return nil, false
	}
	if crc32.ChecksumIEEE(pkt[:4+n]) != binary.LittleEndian.Uint32(pkt[4+n:]) {
		return nil, false
	}
	tags := map[byte][]byte{}
	p := pkt[4 : 4+n]
	for i := 0; i < len(p); {
		tag := p[i]
		i++
		if i >= len(p) {
			return nil, false
		}
		l := int(p[i] & 0x7F)
		if p[i]&0x80 != 0 {
			i++
			if i >= len(p) {
				return nil, false
			}
			l |= int(p[i]) << 7
		}
		i++
		if i+l > len(p) {
			return nil, false
		}
		tags[tag] = p[i : i+l]
		i += l
	}
	return tags, true
}

// discoverRequestMatches reports whether a request (from parseDiscoverRequest)
// is asking for a tuner with this device ID (or any tuner).
func discoverRequestMatches(tags map[byte][]byte, deviceID string) bool {
	if v, ok := tags[hdhrTagDeviceType]; ok && len(v) == 4 {
		t := binary.BigEndian.Uint32(v)
		if t != hdhrDeviceTypeTuner && t != hdhrDeviceTypeWildcard {
			return false
		}
	}
	if v, ok := tags[hdhrTagDeviceID]; ok && len(v) == 4 {
		id := binary.BigEndian.Uint32(v)
		if id != hdhrDeviceIDWildcard && id != deviceIDUint32(deviceID) {
			return false
		}
	}
	return true
}

func deviceIDUint32(id string) uint32 {
	b, err := hex.DecodeString(strings.TrimSpace(id))
	if err != nil || len(b) != 4 {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

// buildDiscoverReply encodes a discover reply packet for d. Tag order matches
// a real HDFX-4K (firmware 20260326) so strict clients see a familiar reply.
func buildDiscoverReply(d hdhrDevice) []byte {
	u32 := func(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }
	var p []byte
	p = appendHDHRTLV(p, hdhrTagDeviceType, u32(hdhrDeviceTypeTuner))
	p = appendHDHRTLV(p, hdhrTagDeviceID, u32(deviceIDUint32(d.DeviceID)))
	if d.DeviceAuth != "" {
		p = appendHDHRTLV(p, hdhrTagDeviceAuthStr, []byte(d.DeviceAuth))
	}
	p = appendHDHRTLV(p, hdhrTagBaseURL, []byte(d.BaseURL))
	p = appendHDHRTLV(p, hdhrTagTunerCount, []byte{byte(d.TunerCount)})
	p = appendHDHRTLV(p, hdhrTagLineupURL, []byte(d.BaseURL+"/lineup.json"))

	out := make([]byte, 4, 4+len(p)+4)
	binary.BigEndian.PutUint16(out[0:2], hdhrTypeDiscoverRpy)
	binary.BigEndian.PutUint16(out[2:4], uint16(len(p)))
	out = append(out, p...)
	return binary.LittleEndian.AppendUint32(out, crc32.ChecksumIEEE(out))
}

// buildDiscoverRequest encodes a discover request for any tuner. Used by tests.
func buildDiscoverRequest() []byte {
	var p []byte
	p = appendHDHRTLV(p, hdhrTagDeviceType, []byte{0, 0, 0, 1})
	p = appendHDHRTLV(p, hdhrTagDeviceID, []byte{0xFF, 0xFF, 0xFF, 0xFF})
	out := make([]byte, 4, 4+len(p)+4)
	binary.BigEndian.PutUint16(out[0:2], hdhrTypeDiscoverReq)
	binary.BigEndian.PutUint16(out[2:4], uint16(len(p)))
	out = append(out, p...)
	return binary.LittleEndian.AppendUint32(out, crc32.ChecksumIEEE(out))
}

func appendHDHRTLV(p []byte, tag byte, v []byte) []byte {
	p = append(p, tag)
	if len(v) < 128 {
		p = append(p, byte(len(v)))
	} else {
		p = append(p, byte(len(v)&0x7F)|0x80, byte(len(v)>>7))
	}
	return append(p, v...)
}
