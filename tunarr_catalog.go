package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// Tunarr channel catalog and streaming, ported from tunarr-hdhr. The catalog
// is refreshed from Tunarr's channel API and served as an HDHR lineup; each
// lineup entry streams from Tunarr through this proxy.

// tunarrChannel is one lineup entry.
type tunarrChannel struct {
	GuideNumber string
	GuideName   string
	TunarrID    string
	StreamURL   string // explicit stream URL from Tunarr, if any
}

// tunarrCatalog holds the last successfully fetched channel list.
type tunarrCatalog struct {
	mu       sync.RWMutex
	entries  []tunarrChannel
	byNumber map[string]tunarrChannel
}

func (c *tunarrCatalog) Entries() []tunarrChannel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]tunarrChannel(nil), c.entries...)
}

func (c *tunarrCatalog) Get(number string) (tunarrChannel, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.byNumber[number]
	return e, ok
}

// apiChannel accepts the channel shapes Tunarr has used across releases.
type apiChannel struct {
	ID            string `json:"id"`
	UUID          string `json:"uuid"`
	Number        any    `json:"number"`
	ChannelNumber any    `json:"channelNumber"`
	Name          string `json:"name"`
	Title         string `json:"title"`
	StreamURL     string `json:"streamUrl"`
	StreamURL2    string `json:"stream_url"`
}

// RefreshCatalog fetches the channel list and replaces the catalog. On error
// the previous catalog is kept, so a Tunarr blip doesn't empty the lineup.
func (tb *TunarrBackend) RefreshCatalog(ctx context.Context, cfg *Config) error {
	b, err := tb.get(ctx, cfg.TunarrChannelsEndpoint())
	if err != nil {
		return err
	}
	channels, err := parseTunarrChannels(b)
	if err != nil {
		return err
	}

	entries := make([]tunarrChannel, 0, len(channels))
	byNumber := make(map[string]tunarrChannel, len(channels))
	next := cfg.TunarrChannelStart()
	for _, ch := range channels {
		num := valueString(ch.Number)
		if num == "" {
			num = valueString(ch.ChannelNumber)
		}
		if !cfg.Tunarr.UseTunarrNumbers || num == "" {
			num = strconv.Itoa(next)
			next++
		}
		if _, dup := byNumber[num]; dup {
			slog.Warn("Duplicate Tunarr channel number, skipping", "number", num, "name", firstNonEmpty(ch.Name, ch.Title))
			continue
		}
		e := tunarrChannel{
			GuideNumber: num,
			GuideName:   firstNonEmpty(ch.Name, ch.Title),
			TunarrID:    firstNonEmpty(ch.ID, ch.UUID),
			StreamURL:   firstNonEmpty(ch.StreamURL, ch.StreamURL2),
		}
		entries = append(entries, e)
		byNumber[num] = e
	}

	tb.catalog.mu.Lock()
	changed := len(entries) != len(tb.catalog.entries)
	tb.catalog.entries, tb.catalog.byNumber = entries, byNumber
	tb.catalog.mu.Unlock()
	if changed {
		slog.Info("Tunarr channel catalog refreshed", "channels", len(entries))
	}
	return nil
}

// parseTunarrChannels accepts a JSON array, or an object wrapping one under
// "channels", "data", or "items". Channels without an ID or name are dropped.
func parseTunarrChannels(b []byte) ([]apiChannel, error) {
	var raw json.RawMessage = b
	var wrapped map[string]json.RawMessage
	if json.Unmarshal(b, &wrapped) == nil {
		raw = nil
		for _, key := range []string{"channels", "data", "items"} {
			if v, ok := wrapped[key]; ok {
				raw = v
				break
			}
		}
		if raw == nil {
			return nil, fmt.Errorf("Tunarr channels JSON has no channels/data/items array")
		}
	}
	var all []apiChannel
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("decode Tunarr channels JSON: %w", err)
	}
	out := all[:0]
	for _, ch := range all {
		if firstNonEmpty(ch.ID, ch.UUID) != "" && firstNonEmpty(ch.Name, ch.Title) != "" {
			out = append(out, ch)
		}
	}
	return out, nil
}

// streamURL returns the Tunarr URL to pull for a channel. Pass-through uses
// the MPEG-TS route Tunarr's own HDHR lineup advertises (no query string:
// Plex mishandles them); ffmpeg mode remuxes from HLS, as tunarr-hdhr did.
func (tb *TunarrBackend) streamURL(e tunarrChannel, mode string) string {
	if e.StreamURL != "" {
		if strings.HasPrefix(e.StreamURL, "/") {
			return tb.baseURL + e.StreamURL
		}
		return e.StreamURL
	}
	if mode == "mpegts" {
		return fmt.Sprintf("%s/stream/channels/%s?streamMode=hls", tb.baseURL, e.TunarrID)
	}
	return fmt.Sprintf("%s/stream/channels/%s.ts", tb.baseURL, e.TunarrID)
}

// streamClient has no overall timeout: live streams run for hours. The
// request context (client disconnect) ends them instead.
var streamClient = &http.Client{}

// proxyStream copies a Tunarr stream to w until either side closes.
func proxyStream(ctx context.Context, w http.ResponseWriter, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := streamClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Tunarr returned HTTP %d", resp.StatusCode)
	}
	w.Header().Set("Content-Type", orDefault(resp.Header.Get("Content-Type"), "video/mp2t"))
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.WriteHeader(http.StatusOK)
	_, err = io.Copy(flushWriter{w}, resp.Body)
	return err
}

// flushWriter pushes each chunk to the client immediately. Without it the
// ResponseWriter buffers, delaying headers and the first packets of a live
// stream until the buffer fills.
type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	http.NewResponseController(f.w).Flush() //nolint:errcheck
	return n, err
}

// remuxToMPEGTS runs ffmpeg to copy (not transcode) Tunarr's HLS into an
// MPEG-TS container written to w.
func remuxToMPEGTS(ctx context.Context, w http.ResponseWriter, ffmpegPath, inputURL string) error {
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner", "-nostdin", "-loglevel", "warning",
		"-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5",
		"-i", inputURL,
		"-map", "0:v:0?", "-map", "0:a:0?", "-c", "copy",
		"-muxdelay", "0", "-muxpreload", "0",
		"-f", "mpegts", "pipe:1",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	_, copyErr := io.Copy(flushWriter{w}, stdout)
	waitErr := cmd.Wait()
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		slog.Debug("ffmpeg stderr", "msg", msg)
	}
	switch {
	case ctx.Err() != nil:
		return nil // client went away; normal end of a stream
	case copyErr != nil:
		return copyErr
	case waitErr != nil:
		return fmt.Errorf("ffmpeg exited: %w", waitErr)
	}
	return nil
}

// proxyXMLTV passes Tunarr's XMLTV guide through unchanged.
func (tb *TunarrBackend) proxyXMLTV(ctx context.Context, w http.ResponseWriter, endpoint string) error {
	b, err := tb.get(ctx, endpoint)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, err = w.Write(b)
	return err
}

// get fetches a Tunarr API path (or absolute URL) and returns the body.
func (tb *TunarrBackend) get(ctx context.Context, endpoint string) ([]byte, error) {
	target := endpoint
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		target = tb.baseURL + "/" + strings.TrimLeft(endpoint, "/")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := tb.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("Tunarr %s returned HTTP %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return io.ReadAll(resp.Body)
}

func firstNonEmpty(v ...string) string {
	for _, x := range v {
		if x != "" {
			return x
		}
	}
	return ""
}

func valueString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}
