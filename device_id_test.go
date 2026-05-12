package main

import (
	"testing"
)

func TestValidateDeviceID(t *testing.T) {
	tests := []struct {
		id       string
		valid    bool
		testName string
	}{
		{"1072ABCD", true, "valid device ID"},
		{"1043A1BC", true, "another valid device ID"},
		{"12345678", true, "numeric device ID format"},
		{"DEADBEEF", true, "valid hex format"},
		{"12345", false, "too short"},
		{"123456789", false, "too long"},
		{"1234567G", false, "invalid hex character"},
		{"ZZZZZZZZ", false, "all invalid hex"},
		{"", false, "empty string"},
	}

	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			result := ValidateDeviceID(test.id)
			if result != test.valid {
				t.Errorf("ValidateDeviceID(%q) = %v, want %v", test.id, result, test.valid)
			}
		})
	}
}

func TestIsValidDeviceIDFormat(t *testing.T) {
	tests := []struct {
		id       string
		valid    bool
		testName string
	}{
		{"1072ABCD", true, "valid format"},
		{"1072abcd", true, "lowercase hex"},
		{"12345678", true, "numeric"},
		{"12345", false, "too short"},
		{"123456789", false, "too long"},
		{"1234567G", false, "invalid character"},
	}

	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			result := IsValidDeviceIDFormat(test.id)
			if result != test.valid {
				t.Errorf("IsValidDeviceIDFormat(%q) = %v, want %v", test.id, result, test.valid)
			}
		})
	}
}

func TestGenerateDeviceID(t *testing.T) {
	tests := []struct {
		modelType    string
		serialSuffix string
		shouldError  bool
		testName     string
	}{
		{"HDFX-4K", "AB", false, "valid HDFX-4K generation"},
		{"HDHR3-US", "12", false, "valid HDHR3-US generation"},
		{"HDHR4-2US", "56", false, "valid HDHR4-2US generation"},
		{"HDHOMERUN3", "AB", false, "valid HDHOMERUN3 generation"},
		{"INVALID", "AB", true, "invalid model type"},
		{"HDFX-4K", "ABCDE", true, "serial suffix too long"},
		{"HDFX-4K", "A", true, "serial suffix too short"},
	}

	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			deviceID, err := GenerateDeviceID(test.modelType, test.serialSuffix)
			if (err != nil) != test.shouldError {
				t.Errorf("GenerateDeviceID(%q, %q) error = %v, shouldError %v", test.modelType, test.serialSuffix, err, test.shouldError)
			}
			if !test.shouldError {
				if !IsValidDeviceIDFormat(deviceID) {
					t.Errorf("GenerateDeviceID(%q, %q) returned invalid format: %q", test.modelType, test.serialSuffix, deviceID)
				}
				if len(deviceID) != 8 {
					t.Errorf("GenerateDeviceID(%q, %q) returned wrong length: %d", test.modelType, test.serialSuffix, len(deviceID))
				}
			}
		})
	}
}

func TestCalculateHDHRChecksum(t *testing.T) {
	tests := []struct {
		dataHex     string
		expectedLen int
		testName    string
	}{
		{"100072AB", 2, "valid 6-digit hex input"},
		{"DEADBE", 2, "another valid input"},
		{"123456", 2, "numeric input"},
		{"AB", 2, "short input returns 2-char checksum"},
		{"", 2, "empty input returns 2-char checksum"},
	}

	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			checksum := calculateHDHRChecksum(test.dataHex)
			if len(checksum) != test.expectedLen {
				t.Errorf("calculateHDHRChecksum(%q) returned %q (len %d), want length %d", test.dataHex, checksum, len(checksum), test.expectedLen)
			}
			// Checksum should be valid hex
			if !IsValidDeviceIDFormat(test.dataHex + checksum) {
				// If combining them doesn't make valid format, at least checksum should be valid hex
				if len(test.dataHex) == 6 {
					t.Errorf("calculateHDHRChecksum(%q) returned invalid hex: %q", test.dataHex, checksum)
				}
			}
		})
	}
}

func TestGenerateRealisticDeviceID(t *testing.T) {
	tests := []string{
		"HDFX-4K",
		"HDHR3-US",
		"HDHR4-2US",
		"INVALID", // Should fall back gracefully
	}

	for _, modelType := range tests {
		t.Run(modelType, func(t *testing.T) {
			deviceID := GenerateRealisticDeviceID(modelType)
			if !IsValidDeviceIDFormat(deviceID) {
				t.Errorf("GenerateRealisticDeviceID(%q) returned invalid format: %q", modelType, deviceID)
			}
			if len(deviceID) != 8 {
				t.Errorf("GenerateRealisticDeviceID(%q) returned wrong length: %d", modelType, len(deviceID))
			}
		})
	}
}

func TestGetModelInfo(t *testing.T) {
	tests := []struct {
		modelType   string
		shouldError bool
		testName    string
	}{
		{"HDFX-4K", false, "valid HDFX-4K"},
		{"HDHR3-US", false, "valid HDHR3-US"},
		{"HDHR4-2US", false, "valid HDHR4-2US"},
		{"HDHOMERUN3", false, "valid HDHOMERUN3"},
		{"INVALID", true, "invalid model"},
		{"", true, "empty model"},
	}

	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			info, err := GetModelInfo(test.modelType)
			if (err != nil) != test.shouldError {
				t.Errorf("GetModelInfo(%q) error = %v, shouldError %v", test.modelType, err, test.shouldError)
			}
			if !test.shouldError {
				if info.ModelNumber == "" {
					t.Errorf("GetModelInfo(%q) returned empty ModelNumber", test.modelType)
				}
				if info.TunerCount <= 0 {
					t.Errorf("GetModelInfo(%q) returned invalid TunerCount: %d", test.modelType, info.TunerCount)
				}
			}
		})
	}
}
