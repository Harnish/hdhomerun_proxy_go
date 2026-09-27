package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeTunarr serves the Tunarr routes the proxy uses. Streams write one chunk
// and then block until the client disconnects, like a live channel.
func fakeTunarr(t *testing.T, channelsJSON string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/channels", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, channelsJSON) //nolint:errcheck
	})
	mux.HandleFunc("/api/xmltv.xml", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<tv><channel id="c1"/></tv>`) //nolint:errcheck
	})
	mux.HandleFunc("/stream/channels/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stream/channels/uuid-1.ts" || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "video/mp2t")
		io.WriteString(w, "TS-BYTES") //nolint:errcheck
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestTunarr(t *testing.T, tunarrURL string, cfg *Config) *TunarrBackend {
	t.Helper()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(tunarrURL, "http://"))
	var p int
	for _, c := range port {
		p = p*10 + int(c-'0')
	}
	tb := NewTunarrBackend(host, p, 5)
	if err := tb.RefreshCatalog(context.Background(), cfg); err != nil {
		t.Fatalf("RefreshCatalog: %v", err)
	}
	return tb
}

func TestParseTunarrChannelsShapes(t *testing.T) {
	for name, body := range map[string]string{
		"array":   `[{"id":"a","name":"A","number":5},{"uuid":"b","title":"B"}]`,
		"wrapped": `{"channels":[{"id":"a","name":"A","number":5},{"uuid":"b","title":"B"}]}`,
		"data":    `{"data":[{"id":"a","name":"A"},{"id":"b","name":"B"},{"id":"","name":"no id"}]}`,
	} {
		chs, err := parseTunarrChannels([]byte(body))
		if err != nil || len(chs) != 2 {
			t.Errorf("%s: got %d channels, err %v", name, len(chs), err)
		}
	}
	if _, err := parseTunarrChannels([]byte(`{"nope":[]}`)); err == nil {
		t.Error("object without a channel array should error")
	}
}

func TestRefreshCatalogNumbering(t *testing.T) {
	srv := fakeTunarr(t, `[{"id":"uuid-1","name":"One","number":7},{"id":"uuid-2","name":"Two","number":"9.1"}]`)

	cfg := DefaultConfig()
	tb := newTestTunarr(t, srv.URL, cfg)
	if got := tb.catalog.Entries(); got[0].GuideNumber != "100" || got[1].GuideNumber != "101" {
		t.Errorf("default numbering: got %q, %q", got[0].GuideNumber, got[1].GuideNumber)
	}

	cfg.Tunarr.UseTunarrNumbers = true
	tb = newTestTunarr(t, srv.URL, cfg)
	if got := tb.catalog.Entries(); got[0].GuideNumber != "7" || got[1].GuideNumber != "9.1" {
		t.Errorf("tunarr numbering: got %q, %q", got[0].GuideNumber, got[1].GuideNumber)
	}
}

func TestTunarrLineupAndStream(t *testing.T) {
	tunarr := fakeTunarr(t, `[{"id":"uuid-1","name":"One"}]`)
	cfg := DefaultConfig()
	cfg.Device.ModelType = "HDHR4-2US" // 2 tuners
	store := newConfigStore(cfg, "")
	he := NewHDHREndpointServer(store, &mockHDHRStatsProvider{}, newTestTunarr(t, tunarr.URL, cfg))
	proxy := httptest.NewServer(he.Handler())
	t.Cleanup(proxy.Close)

	// lineup.json: URLs point back at this server, not at Tunarr.
	resp, err := http.Get(proxy.URL + "/lineup.json")
	if err != nil {
		t.Fatal(err)
	}
	var lineup []LineupItemJSON
	json.NewDecoder(resp.Body).Decode(&lineup) //nolint:errcheck
	resp.Body.Close()
	if len(lineup) != 1 || lineup[0].URL != proxy.URL+"/auto/v100" || lineup[0].HD != 1 {
		t.Fatalf("lineup = %+v", lineup)
	}

	// lineup.xml: real-device shape.
	resp, _ = http.Get(proxy.URL + "/lineup.xml")
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), "<Lineup><Program><GuideNumber>100</GuideNumber><GuideName>One</GuideName><HD>1</HD>") {
		t.Errorf("lineup.xml = %s", b)
	}

	// epg.xml passes Tunarr's XMLTV through.
	resp, _ = http.Get(proxy.URL + "/epg.xml")
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != `<tv><channel id="c1"/></tv>` {
		t.Errorf("epg.xml = %s", b)
	}

	// Two streams take both tuners; a third gets 503 like a real device.
	openStream := func() (*http.Response, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, "GET", proxy.URL+"/auto/v100", nil)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return r, cancel
	}
	s1, cancel1 := openStream()
	s2, cancel2 := openStream()
	defer cancel2()
	chunk := make([]byte, 8)
	if _, err := io.ReadFull(s1.Body, chunk); err != nil || string(chunk) != "TS-BYTES" {
		t.Fatalf("stream bytes = %q, err %v", chunk, err)
	}
	if ct := s1.Header.Get("Content-Type"); ct != "video/mp2t" {
		t.Errorf("stream Content-Type = %q", ct)
	}
	s3, cancel3 := openStream()
	cancel3()
	if s3.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("third stream status = %d, want 503", s3.StatusCode)
	}
	if st, _ := he.tunerStates.GetTuner(0); st.Channel != "100" || st.Status != "locked" {
		t.Errorf("tuner 0 state = %+v", st)
	}

	// Closing a stream frees its tuner.
	cancel1()
	s1.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if st, _ := he.tunerStates.GetTuner(0); st.Status == "idle" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tuner 0 not released after client disconnect")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s2.Body.Close()

	// Unknown channel.
	resp, _ = http.Get(proxy.URL + "/auto/v999")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown channel status = %d", resp.StatusCode)
	}
}

func TestTunarrDiscoverReply(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Device.DeviceID = "1072ABCD"
	cfg.Tunarr.BaseURL = "http://proxy.lan:5004/"
	br := backendRouter{store: newConfigStore(cfg, "")}

	reply := br.tunarrDiscoverReply(buildDiscoverRequest(), net.IPv4(127, 0, 0, 1))
	want := buildDiscoverReply(hdhrDevice{DeviceID: "1072ABCD", DeviceAuth: "00000000", TunerCount: 4, BaseURL: "http://proxy.lan:5004"})
	if string(reply) != string(want) {
		t.Errorf("reply mismatch\n got %x\nwant %x", reply, want)
	}
	if br.tunarrDiscoverReply([]byte("not a discover packet"), net.IPv4(127, 0, 0, 1)) != nil {
		t.Error("non-discover query should get no reply")
	}
}
