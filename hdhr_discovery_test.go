package main

import (
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"strings"
	"testing"
)

func TestDiscoverRequestRoundTrip(t *testing.T) {
	tags, ok := parseDiscoverRequest(buildDiscoverRequest())
	if !ok {
		t.Fatal("valid request rejected")
	}
	if !discoverRequestMatches(tags, "1072ABCD") {
		t.Error("wildcard request should match any tuner")
	}
}

func TestDiscoverRequestRejectsBadCRCAndText(t *testing.T) {
	req := buildDiscoverRequest()
	req[len(req)-1] ^= 0xFF
	if _, ok := parseDiscoverRequest(req); ok {
		t.Error("bad CRC accepted")
	}
	if _, ok := parseDiscoverRequest([]byte("TYPE: discover\r\n")); ok {
		t.Error("text query accepted as binary discover")
	}
}

func TestDiscoverRequestDeviceIDFilter(t *testing.T) {
	tags := map[byte][]byte{hdhrTagDeviceID: {0x10, 0x72, 0xAB, 0xCD}}
	if !discoverRequestMatches(tags, "1072abcd") {
		t.Error("request for our ID should match")
	}
	if discoverRequestMatches(tags, "10123405") {
		t.Error("request for another ID should not match")
	}
	storage := map[byte][]byte{hdhrTagDeviceType: {0, 0, 0, 5}}
	if discoverRequestMatches(storage, "1072ABCD") {
		t.Error("non-tuner device type should not match")
	}
}

func TestBuildDiscoverReply(t *testing.T) {
	longURL := "http://" + strings.Repeat("a", 150) + ":5004" // forces 2-byte TLV length
	pkt := buildDiscoverReply(hdhrDevice{DeviceID: "1072ABCD", DeviceAuth: "x", TunerCount: 4, BaseURL: longURL})

	if binary.BigEndian.Uint16(pkt[0:2]) != hdhrTypeDiscoverRpy {
		t.Fatalf("type = %#x", binary.BigEndian.Uint16(pkt[0:2]))
	}
	n := int(binary.BigEndian.Uint16(pkt[2:4]))
	if 4+n+4 != len(pkt) {
		t.Fatalf("length field %d inconsistent with packet size %d", n, len(pkt))
	}
	if crc32.ChecksumIEEE(pkt[:4+n]) != binary.LittleEndian.Uint32(pkt[4+n:]) {
		t.Fatal("bad CRC")
	}

	// Reuse the request parser to decode the reply's TLVs.
	cp := append([]byte(nil), pkt...)
	binary.BigEndian.PutUint16(cp[0:2], hdhrTypeDiscoverReq)
	binary.LittleEndian.PutUint32(cp[4+n:], crc32.ChecksumIEEE(cp[:4+n]))
	tags, ok := parseDiscoverRequest(cp)
	if !ok {
		t.Fatal("reply TLVs did not parse")
	}
	if got := binary.BigEndian.Uint32(tags[hdhrTagDeviceType]); got != hdhrDeviceTypeTuner {
		t.Errorf("device type = %#x", got)
	}
	if got := binary.BigEndian.Uint32(tags[hdhrTagDeviceID]); got != 0x1072ABCD {
		t.Errorf("device id = %#x", got)
	}
	if got := tags[hdhrTagTunerCount]; len(got) != 1 || got[0] != 4 {
		t.Errorf("tuner count = %v", got)
	}
	if got := string(tags[hdhrTagBaseURL]); got != longURL {
		t.Errorf("base url = %q", got)
	}
	if got := string(tags[hdhrTagLineupURL]); got != longURL+"/lineup.json" {
		t.Errorf("lineup url = %q", got)
	}
}

// Reply captured from a real HDFX-4K (firmware 20260326), with its DeviceAuth
// token replaced by X's and the CRC recomputed. Our encoder must produce the
// same bytes for the same identity.
const realHDFX4KReplyHex = "00030063010400000001020410ab2a5f2b185858585858585858585858585858585858585858585858582a15687474703a2f2f31302e31302e31302e31353a38301001042721687474703a2f2f31302e31302e31302e31353a38302f6c696e6575702e6a736f6e5c63c761"

func TestBuildDiscoverReplyMatchesRealDevice(t *testing.T) {
	got := buildDiscoverReply(hdhrDevice{
		DeviceID:   "10AB2A5F",
		DeviceAuth: strings.Repeat("X", 24),
		TunerCount: 4,
		BaseURL:    "http://10.10.10.15:80",
	})
	if want := realHDFX4KReplyHex; hex.EncodeToString(got) != want {
		t.Errorf("reply differs from real device\n got %x\nwant %s", got, want)
	}
}
