package main

import (
	"strings"
	"testing"
)

func TestBuildDiscoveryResponseCRLF(t *testing.T) {
	backend := &TunarrBackend{
		host:    "localhost",
		port:    8000,
		baseURL: "http://localhost:8000",
	}
	info := &TunarrDiscoverResponse{TunerCount: 2}
	result := backend.BuildDiscoveryResponse(nil, info)
	if strings.Contains(result, `\r\n`) {
		t.Error("BuildDiscoveryResponse: contains literal \\r\\n instead of CRLF")
	}
	if !strings.Contains(result, "\r\n") {
		t.Error("BuildDiscoveryResponse: missing CRLF")
	}
}

func TestConvertLineupToHDHRFormatCRLF(t *testing.T) {
	lineup := []TunarrLineupItem{
		{GuideNumber: "1", GuideName: "Channel 1"},
		{GuideNumber: "2", GuideName: "Channel 2"},
	}
	result := ConvertLineupToHDHRFormat(lineup)
	if strings.Contains(result, `\r\n`) {
		t.Error("ConvertLineupToHDHRFormat: contains literal \\r\\n instead of CRLF")
	}
	if !strings.Contains(result, "\r\n") {
		t.Error("ConvertLineupToHDHRFormat: missing CRLF")
	}
}

func TestBuildHDHRDiscoveryPacketCRLF(t *testing.T) {
	info := &TunarrDiscoverResponse{TunerCount: 2}
	result := string(BuildHDHRDiscoveryPacket(info, 8000, "127.0.0.1"))
	if strings.Contains(result, `\r`) {
		t.Error("BuildHDHRDiscoveryPacket: contains literal \\r instead of CR")
	}
	if !strings.Contains(result, "\r\n") {
		t.Error("BuildHDHRDiscoveryPacket: missing CRLF")
	}
}
