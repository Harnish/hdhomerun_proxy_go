package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
)

// HDHREndpointServer serves HDHR-compatible discovery endpoints
// This is separate from the admin WebUI and doesn't require authentication
type HDHREndpointServer struct {
	store        *configStore
	router       statsProvider
	tunerStates  *TunerStateManager
}

// DiscoverJSONResponse matches HDHomeRun discover.json format
type DiscoverJSONResponse struct {
	FriendlyName    string `json:"FriendlyName"`
	ModelNumber     string `json:"ModelNumber"`
	FirmwareName    string `json:"FirmwareName"`
	FirmwareVersion string `json:"FirmwareVersion"`
	DeviceID        string `json:"DeviceID"`
	DeviceAuth      string `json:"DeviceAuth"`
	BaseURL         string `json:"BaseURL"`
	LineupURL       string `json:"LineupURL"`
	TunerCount      int    `json:"TunerCount"`
}

// LineupItemJSON is a single channel in the lineup
type LineupItemJSON struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
}

// LineupStatusJSON is the lineup status response
type LineupStatusJSON struct {
	ScanInProgress int      `json:"ScanInProgress"`
	ScanPossible   int      `json:"ScanPossible"`
	Source         string   `json:"Source"`
	SourceList     []string `json:"SourceList"`
	NumChannels    int      `json:"NumChannels"`
}

// DeviceXML is the XML representation of an HDHR device
type DeviceXML struct {
	XMLName     xml.Name `xml:"root"`
	Xmlns       string   `xml:"xmlns,attr"`
	Device      DeviceXMLDevice
	ServiceList ServiceList
}

type DeviceXMLDevice struct {
	DeviceType   string
	FriendlyName string
	Manufacturer string
	ModelNumber  string
	DeviceID     string
	BaseURL      string
}

type ServiceList struct {
	Service []Service
}

type Service struct {
	ServiceType string
	ServiceID   string
	SCPDURL     string
	ControlURL  string
	EventSubURL string
}

// TunerStatusJSON is per-tuner status
type TunerStatusJSON struct {
	TunerIndex     int    `json:"TunerIndex"`
	Status         string `json:"Status"`
	Channel        string `json:"Channel"`
	SessionID      string `json:"SessionID"`
	Tuning         bool   `json:"Tuning"`
	SignalStrength int    `json:"SignalStrength"`
	VCT            bool   `json:"VCT"`
	TargetIP       string `json:"TargetIP"`
	TargetPort     int    `json:"TargetPort"`
}

// NewHDHREndpointServer creates a new HDHR endpoint server
func NewHDHREndpointServer(store *configStore, router statsProvider) *HDHREndpointServer {
	// Initialize tuner state manager with tuner count from config
	cfg := store.Get()
	modelType := cfg.Device.ModelType
	if modelType == "" {
		modelType = "HDFX-4K"
	}
	modelInfo, _ := GetModelInfo(modelType)

	return &HDHREndpointServer{
		store:       store,
		router:      router,
		tunerStates: NewTunerStateManager(modelInfo.TunerCount),
	}
}

// Handler returns the HTTP handler for HDHR endpoints
func (he *HDHREndpointServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/discover.json", he.handleDiscover)
	mux.HandleFunc("/lineup.json", he.handleLineup)
	mux.HandleFunc("/lineup_status.json", he.handleLineupStatus)
	mux.HandleFunc("/device.xml", he.handleDeviceXML)
	mux.HandleFunc("/tuner", he.handleTunerList)
	// Tuner status endpoints - pattern matching for /tuner{N}/status
	for i := 0; i < 8; i++ {
		tunerNum := i
		mux.HandleFunc(fmt.Sprintf("/tuner%d/status", tunerNum), func(w http.ResponseWriter, r *http.Request) {
			he.handleTunerStatus(w, r, tunerNum)
		})
		mux.HandleFunc(fmt.Sprintf("/tuner%d/streaminfo", tunerNum), func(w http.ResponseWriter, r *http.Request) {
			he.handleTunerStreamInfo(w, r, tunerNum)
		})
	}
	return mux
}

// getDeviceConfig gets the current device configuration, auto-generating DeviceID if needed
func (he *HDHREndpointServer) getDeviceConfig(ctx context.Context) *DiscoverJSONResponse {
	cfg := he.store.Get()
	model := cfg.Device.ModelType
	if model == "" {
		model = "HDFX-4K"
	}

	modelInfo, err := GetModelInfo(model)
	if err != nil {
		modelInfo, _ = GetModelInfo("HDFX-4K")
	}

	deviceID := cfg.Device.DeviceID
	if deviceID == "" {
		// Auto-generate a realistic Device ID
		deviceID = GenerateRealisticDeviceID(model)
		// Update config with generated ID (in-memory only for now)
		cfg.Device.DeviceID = deviceID
	}

	baseURL := he.getBaseURL()
	friendlyName := cfg.Device.FriendlyName
	if friendlyName == "" {
		friendlyName = modelInfo.FriendlyName
	}

	firmwareVersion := cfg.Device.FirmwareVersion
	if firmwareVersion == "" {
		firmwareVersion = "20250825"
	}

	deviceAuth := cfg.Device.DeviceAuth
	if deviceAuth == "" {
		deviceAuth = "00000000"
	}

	return &DiscoverJSONResponse{
		FriendlyName:    friendlyName,
		ModelNumber:     modelInfo.ModelNumber,
		FirmwareName:    modelInfo.FirmwareName,
		FirmwareVersion: firmwareVersion,
		DeviceID:        deviceID,
		DeviceAuth:      deviceAuth,
		BaseURL:         baseURL,
		LineupURL:       baseURL + "/lineup.json",
		TunerCount:      modelInfo.TunerCount,
	}
}

// getBaseURL constructs the base URL for the device
func (he *HDHREndpointServer) getBaseURL() string {
	// This would be called in a real server context where we know the address
	// For now, return a placeholder that would be set by the calling server
	// In practice, this should use the request's Host header
	return "http://192.168.1.100:5004"
}

// handleDiscover handles /discover.json
func (he *HDHREndpointServer) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	discover := he.getDeviceConfig(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(discover) //nolint:errcheck
}

// handleLineup handles /lineup.json
func (he *HDHREndpointServer) handleLineup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var lineup []LineupItemJSON

	// If Tunarr is available, get lineup from it
	baseURL := he.getBaseURL()
	if stats := he.router.Stats(); stats.TunarrConfigured {
		// This would normally fetch from Tunarr backend
		// For now, return a sample lineup
		lineup = []LineupItemJSON{
			{
				GuideNumber: "1.1",
				GuideName:   "NBC",
				URL:         baseURL + "/auto/v1.1",
			},
			{
				GuideNumber: "2.1",
				GuideName:   "CBS",
				URL:         baseURL + "/auto/v2.1",
			},
		}
	} else {
		// Empty lineup for direct HDHR mode
		lineup = []LineupItemJSON{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(lineup) //nolint:errcheck
}

// handleLineupStatus handles /lineup_status.json
func (he *HDHREndpointServer) handleLineupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	status := LineupStatusJSON{
		ScanInProgress: 0,
		ScanPossible:   1,
		Source:         "Cable",
		SourceList:     []string{"Cable"},
		NumChannels:    100,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(status) //nolint:errcheck
}

// handleDeviceXML handles /device.xml
func (he *HDHREndpointServer) handleDeviceXML(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	discover := he.getDeviceConfig(r.Context())
	device := DeviceXML{
		Xmlns: "urn:schemas-upnp-org:device-1-0",
		Device: DeviceXMLDevice{
			DeviceType:   "urn:schemas-upnp-org:device:MediaServer:1",
			FriendlyName: discover.FriendlyName,
			Manufacturer: "Silicondust",
			ModelNumber:  discover.ModelNumber,
			DeviceID:     discover.DeviceID,
			BaseURL:      discover.BaseURL,
		},
		ServiceList: ServiceList{
			Service: []Service{
				{
					ServiceType: "urn:schemas-upnp-org:service:ContentDirectory:1",
					ServiceID:   "urn:upnp-org:serviceId:ContentDirectory",
					SCPDURL:     "/device.xml",
					ControlURL:  "/control",
					EventSubURL: "/subscribe",
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	xml.NewEncoder(w).Encode(device) //nolint:errcheck
}

// handleTunerList handles /tuner (lists tuners)
func (he *HDHREndpointServer) handleTunerList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	discover := he.getDeviceConfig(r.Context())
	baseURL := he.getBaseURL()

	type tunerInfo struct {
		Index      int    `json:"Index"`
		StatusURL  string `json:"StatusURL"`
		StreamInfo string `json:"StreamInfoURL"`
	}

	tuners := make([]tunerInfo, discover.TunerCount)
	for i := 0; i < discover.TunerCount; i++ {
		tuners[i] = tunerInfo{
			Index:      i,
			StatusURL:  fmt.Sprintf("%s/tuner%d/status", baseURL, i),
			StreamInfo: fmt.Sprintf("%s/tuner%d/streaminfo", baseURL, i),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tuners) //nolint:errcheck
}

// handleTunerStatus handles /tuner{N}/status
func (he *HDHREndpointServer) handleTunerStatus(w http.ResponseWriter, r *http.Request, tunerNum int) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	tuner, err := he.tunerStates.GetTuner(tunerNum)
	if err != nil {
		http.Error(w, "Tuner Not Found", http.StatusNotFound)
		return
	}

	status := TunerStatusJSON{
		TunerIndex:     tuner.Index,
		Status:         tuner.Status,
		Channel:        tuner.Channel,
		SessionID:      tuner.SessionID,
		Tuning:         tuner.Tuning,
		SignalStrength: tuner.SignalStrength,
		VCT:            tuner.VCT,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status) //nolint:errcheck
}

// handleTunerStreamInfo handles /tuner{N}/streaminfo
func (he *HDHREndpointServer) handleTunerStreamInfo(w http.ResponseWriter, r *http.Request, tunerNum int) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	tuner, err := he.tunerStates.GetTuner(tunerNum)
	if err != nil {
		http.Error(w, "Tuner Not Found", http.StatusNotFound)
		return
	}

	type streamInfo struct {
		TunerIndex     int    `json:"TunerIndex"`
		Status         string `json:"Status"`
		Channel        string `json:"Channel"`
		Program        string `json:"Program"`
		Bandwidth      int    `json:"Bandwidth"`
		SessionID      string `json:"SessionID"`
		TargetIP       string `json:"TargetIP"`
		TargetPort     int    `json:"TargetPort"`
		BitRate        int    `json:"BitRate"`
		SignalStrength int    `json:"SignalStrength"`
	}

	info := streamInfo{
		TunerIndex:     tuner.Index,
		Status:         tuner.Status,
		Channel:        tuner.Channel,
		Program:        tuner.Program,
		Bandwidth:      0,
		SessionID:      tuner.SessionID,
		TargetIP:       tuner.TargetIP,
		TargetPort:     tuner.TargetPort,
		BitRate:        tuner.BitRate,
		SignalStrength: tuner.SignalStrength,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info) //nolint:errcheck
}

// UpdateBaseURLFromRequest updates the base URL based on the incoming request
// This should be called on the first request to set the correct base URL for responses
func (he *HDHREndpointServer) UpdateBaseURLFromRequest(r *http.Request) string {
	var scheme string
	if r.TLS != nil {
		scheme = "https"
	} else {
		scheme = "http"
	}

	host := r.Host
	if host == "" {
		host = r.Header.Get("Host")
	}

	baseURL := fmt.Sprintf("%s://%s", scheme, host)
	return baseURL
}
