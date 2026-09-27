package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// HDHREndpointServer serves HDHR-compatible discovery endpoints
// This is separate from the admin WebUI and doesn't require authentication
type HDHREndpointServer struct {
	store       *configStore
	router      statsProvider
	tunerStates *TunerStateManager
	tunarr      *TunarrBackend // nil unless Tunarr is enabled; supplies the lineup and streams
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

// LineupItemJSON is a single channel in the lineup (also the <Program>
// element of lineup.xml). Field order matches a real HDHomeRun.
type LineupItemJSON struct {
	XMLName     xml.Name `json:"-" xml:"Program"`
	GuideNumber string   `json:"GuideNumber"`
	GuideName   string   `json:"GuideName"`
	HD          int      `json:"HD,omitempty" xml:"HD,omitempty"`
	URL         string   `json:"URL"`
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
func NewHDHREndpointServer(store *configStore, router statsProvider, tunarr *TunarrBackend) *HDHREndpointServer {
	modelInfo, _, _ := deviceIdentity(store.Get())
	return &HDHREndpointServer{
		store:       store,
		router:      router,
		tunerStates: NewTunerStateManager(modelInfo.TunerCount),
		tunarr:      tunarr,
	}
}

// Handler returns the HTTP handler for HDHR endpoints
func (he *HDHREndpointServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/discover.json", he.handleDiscover)
	mux.HandleFunc("/lineup.json", he.handleLineup)
	mux.HandleFunc("/lineup_status.json", he.handleLineupStatus)
	mux.HandleFunc("/lineup.xml", he.handleLineupXML)
	mux.HandleFunc("/epg.xml", he.handleEPG)
	mux.HandleFunc("/auto/", he.handleStream)
	mux.HandleFunc("/healthz", he.handleHealth)
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
func (he *HDHREndpointServer) getDeviceConfig(r *http.Request) *DiscoverJSONResponse {
	cfg := he.store.Get()
	modelInfo, deviceID, deviceAuth := deviceIdentity(cfg)
	baseURL := he.baseURL(r)
	return &DiscoverJSONResponse{
		FriendlyName:    orDefault(cfg.Device.FriendlyName, modelInfo.FriendlyName),
		ModelNumber:     modelInfo.ModelNumber,
		FirmwareName:    modelInfo.FirmwareName,
		FirmwareVersion: orDefault(cfg.Device.FirmwareVersion, "20250825"),
		DeviceID:        deviceID,
		DeviceAuth:      deviceAuth,
		BaseURL:         baseURL,
		LineupURL:       baseURL + "/lineup.json",
		TunerCount:      modelInfo.TunerCount,
	}
}

// baseURL is the URL clients use to reach this server: the configured
// tunarr.base_url, else the scheme and Host of the request itself.
func (he *HDHREndpointServer) baseURL(r *http.Request) string {
	if u := he.store.Get().Tunarr.BaseURL; u != "" {
		return strings.TrimRight(u, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// lineup returns the channels to advertise. Only Tunarr supplies channels;
// real HDHomeRun devices serve their own lineup.
func (he *HDHREndpointServer) lineup(r *http.Request) []LineupItemJSON {
	items := []LineupItemJSON{}
	if he.tunarr == nil {
		return items
	}
	base := he.baseURL(r)
	for _, e := range he.tunarr.catalog.Entries() {
		items = append(items, LineupItemJSON{
			GuideNumber: e.GuideNumber,
			GuideName:   e.GuideName,
			HD:          1,
			URL:         base + "/auto/v" + e.GuideNumber,
		})
	}
	return items
}

// handleDiscover handles /discover.json
func (he *HDHREndpointServer) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(he.getDeviceConfig(r)) //nolint:errcheck
}

// handleLineup handles /lineup.json
func (he *HDHREndpointServer) handleLineup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(he.lineup(r)) //nolint:errcheck
}

// handleLineupXML handles /lineup.xml in the shape a real HDHomeRun serves:
// <Lineup><Program>...</Program></Lineup>.
func (he *HDHREndpointServer) handleLineupXML(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	doc := struct {
		XMLName  xml.Name `xml:"Lineup"`
		Programs []LineupItemJSON
	}{Programs: he.lineup(r)}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(xml.Header))   //nolint:errcheck
	xml.NewEncoder(w).Encode(doc) //nolint:errcheck
}

// handleEPG passes Tunarr's XMLTV guide through at /epg.xml.
func (he *HDHREndpointServer) handleEPG(w http.ResponseWriter, r *http.Request) {
	if he.tunarr == nil {
		http.NotFound(w, r)
		return
	}
	if err := he.tunarr.proxyXMLTV(r.Context(), w, he.store.Get().TunarrXMLTVEndpoint()); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
}

// handleStream serves /auto/v<GuideNumber>: it claims a free tuner (503 when
// all are busy, like a real device) and streams the channel from Tunarr.
func (he *HDHREndpointServer) handleStream(w http.ResponseWriter, r *http.Request) {
	if he.tunarr == nil {
		http.NotFound(w, r)
		return
	}
	number := strings.TrimPrefix(r.URL.Path, "/auto/v")
	ch, ok := he.tunarr.catalog.Get(number)
	if !ok {
		http.Error(w, "Unknown channel", http.StatusNotFound)
		return
	}

	tuner := he.acquireTuner(r)
	if tuner < 0 {
		http.Error(w, "All tuners in use", http.StatusServiceUnavailable)
		return
	}
	defer he.tunerStates.ReleaseTuner(tuner)      //nolint:errcheck
	he.tunerStates.SetTunerChannel(tuner, number) //nolint:errcheck

	cfg := he.store.Get()
	src := he.tunarr.streamURL(ch, cfg.Tunarr.StreamMode)
	slog.Info("Stream started", "channel", number, "name", ch.GuideName, "tuner", tuner, "client", r.RemoteAddr)

	var err error
	if cfg.Tunarr.StreamMode == "mpegts" {
		err = remuxToMPEGTS(r.Context(), w, cfg.TunarrFFmpegPath(), src)
	} else {
		err = proxyStream(r.Context(), w, src)
	}
	if err != nil && r.Context().Err() == nil {
		slog.Warn("Stream failed", "channel", number, "source", src, "err", err)
		// Effective only if nothing was written yet; otherwise the client just sees EOF.
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	slog.Info("Stream ended", "channel", number, "tuner", tuner)
}

// acquireTuner locks the first idle tuner for this request, or returns -1.
func (he *HDHREndpointServer) acquireTuner(r *http.Request) int {
	host, port, _ := net.SplitHostPort(r.RemoteAddr)
	p, _ := strconv.Atoi(port)
	for i := 0; i < he.tunerStates.GetTunerCount(); i++ {
		if he.tunerStates.LockTuner(i, fmt.Sprintf("%08x", i+1), host, p) == nil {
			return i
		}
	}
	return -1
}

// handleHealth reports liveness and the number of channels in the lineup.
func (he *HDHREndpointServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "channels": len(he.lineup(r))}) //nolint:errcheck
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
		NumChannels:    len(he.lineup(r)),
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

	discover := he.getDeviceConfig(r)
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

	discover := he.getDeviceConfig(r)
	baseURL := discover.BaseURL

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

// serveHDHREndpoints runs the HDHR HTTP API (discover/lineup/streams) on
// bindAddr:5004 until ctx is cancelled.
func serveHDHREndpoints(ctx context.Context, bindAddr string, he *HDHREndpointServer) {
	addr := net.JoinHostPort(bindAddr, "5004")
	srv := &http.Server{Addr: addr, Handler: he.Handler()}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx) //nolint:errcheck
	}()
	slog.Info("HDHR endpoint server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("HDHR endpoint server error", "err", err)
	}
}
