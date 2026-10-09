package core

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/require"

	"github.com/bluenviron/mediamtx/internal/test"
)

// hookRecorder records the bodies received by a fake smart-streamer API.
type hookRecorder struct {
	mutex   sync.Mutex
	bodies  []map[string]any
	keys    []string
	handler func(w http.ResponseWriter, body map[string]any)
}

func (hr *hookRecorder) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var parsed map[string]any
		require.NoError(t, json.Unmarshal(raw, &parsed))

		hr.mutex.Lock()
		hr.bodies = append(hr.bodies, parsed)
		hr.keys = append(hr.keys, r.Header.Get("x-api-key"))
		handler := hr.handler
		hr.mutex.Unlock()

		if handler != nil {
			handler(w, parsed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
}

func (hr *hookRecorder) types() []string {
	hr.mutex.Lock()
	defer hr.mutex.Unlock()
	ret := make([]string, len(hr.bodies))
	for i, b := range hr.bodies {
		ret[i], _ = b["type"].(string)
	}
	return ret
}

func (hr *hookRecorder) cameras() []string {
	hr.mutex.Lock()
	defer hr.mutex.Unlock()
	ret := make([]string, len(hr.bodies))
	for i, b := range hr.bodies {
		ret[i], _ = b["camera"].(string)
	}
	return ret
}

func (hr *hookRecorder) waitForTypes(t *testing.T, want []string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		got := hr.types()
		if len(got) >= len(want) {
			require.Equal(t, want, got[:len(want)])
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %v, got %v", want, got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func httpHookConf(addr string, extra string) string {
	return "rtmp: no\n" +
		"hls: no\n" +
		"webrtc: no\n" +
		"paths:\n" +
		"  '~^([^/]+)-([^/]+)/(.+)$':\n" +
		"    runOnDemandHTTPAddress: " + addr + "\n" +
		"    runOnDemandHTTPHeaders:\n" +
		"      - 'Content-Type: application/json'\n" +
		"      - 'x-api-key: secret123'\n" +
		"    runOnDemandHTTPBody: '{\"camera\":\"$G3\",\"type\":\"start\"," +
		"\"requestSource\":\"live\",\"initiateVia\":\"API\"}'\n" +
		"    runOnUnDemandHTTPBody: '{\"camera\":\"$G3\",\"type\":\"stop\"," +
		"\"requestSource\":\"live\",\"initiateVia\":\"API\"}'\n" +
		"    runOnDemandHTTPKey: '$G3'\n" +
		extra
}

// a reader demands the path, the remote API is told to start, it publishes,
// the reader leaves, and the remote API is told to stop.
func TestPathRunOnDemandHTTP(t *testing.T) {
	hr := &hookRecorder{}

	var publisher *gortsplib.Client
	var publisherMutex sync.Mutex

	hr.handler = func(w http.ResponseWriter, body map[string]any) {
		if body["type"] == "start" {
			// the remote smart-streamer reacts by publishing
			go func() {
				media0 := test.UniqueMediaH264()
				c := &gortsplib.Client{}
				err := c.StartRecording("rtsp://localhost:8554/cmp1-loc1/cam1",
					&description.Session{Medias: []*description.Media{media0}})
				if err != nil {
					return
				}
				publisherMutex.Lock()
				publisher = c
				publisherMutex.Unlock()

				c.WritePacketRTP(media0, &rtp.Packet{ //nolint:errcheck
					Version:     2,
					PayloadType: 96,
					Payload:     []byte{5, 1, 2, 3, 4},
				})
			}()
		}
		w.WriteHeader(http.StatusOK)
	}

	s := hr.server(t)
	defer s.Close()

	p, ok := newInstance(t, httpHookConf(s.URL,
		"    runOnDemandStartTimeout: 5s\n"+
			"    runOnDemandCloseAfter: 1s\n"))
	require.Equal(t, true, ok)
	defer p.Close()

	reader := gortsplib.Client{Scheme: "rtsp", Host: "localhost:8554"}
	err := reader.Start()
	require.NoError(t, err)

	desc, _, err := reader.Describe(mustParseURL("rtsp://localhost:8554/cmp1-loc1/cam1"))
	require.NoError(t, err)

	err = reader.SetupAll(desc.BaseURL, desc.Medias)
	require.NoError(t, err)

	_, err = reader.Play(nil)
	require.NoError(t, err)

	// the start request must have fired, carrying the camera ID from $G3
	hr.waitForTypes(t, []string{"start"}, 5*time.Second)
	require.Equal(t, []string{"cam1"}, hr.cameras())
	require.Equal(t, []string{"secret123"}, hr.keys)

	// no stop while the reader is attached
	time.Sleep(1500 * time.Millisecond)
	require.Equal(t, []string{"start"}, hr.types())

	reader.Close()

	publisherMutex.Lock()
	if publisher != nil {
		publisher.Close()
	}
	publisherMutex.Unlock()

	// once the last reader leaves, stop must follow, after stop
	hr.waitForTypes(t, []string{"start", "stop"}, 10*time.Second)
	require.Equal(t, []string{"cam1", "cam1"}, hr.cameras())
}

// a failing start is retried; a reader that waits long enough still gets served.
func TestPathRunOnDemandHTTPRetriesStart(t *testing.T) {
	hr := &hookRecorder{}

	var attempts int
	hr.handler = func(w http.ResponseWriter, body map[string]any) {
		if body["type"] == "start" {
			attempts++
			if attempts <= 2 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	}

	s := hr.server(t)
	defer s.Close()

	p, ok := newInstance(t, httpHookConf(s.URL,
		"    runOnDemandStartTimeout: 5s\n"+
			"    runOnDemandCloseAfter: 1s\n"+
			"    runOnDemandHTTPRetries: 5\n"+
			"    runOnDemandHTTPRetryInterval: 200ms\n"))
	require.Equal(t, true, ok)
	defer p.Close()

	go func() {
		c := gortsplib.Client{Scheme: "rtsp", Host: "localhost:8554"}
		if err := c.Start(); err != nil {
			return
		}
		defer c.Close()
		c.Describe(mustParseURL("rtsp://localhost:8554/cmp1-loc1/cam1")) //nolint:errcheck
	}()

	// three start attempts, then eventually a stop once the demand times out
	hr.waitForTypes(t, []string{"start", "start", "start"}, 10*time.Second)
}

// a start that is definitively refused must not be followed by a stop.
func TestPathRunOnDemandHTTPRejectedStartSendsNoStop(t *testing.T) {
	hr := &hookRecorder{}

	hr.handler = func(w http.ResponseWriter, body map[string]any) {
		if body["type"] == "start" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}

	s := hr.server(t)
	defer s.Close()

	p, ok := newInstance(t, httpHookConf(s.URL,
		"    runOnDemandStartTimeout: 1s\n"+
			"    runOnDemandCloseAfter: 1s\n"))
	require.Equal(t, true, ok)
	defer p.Close()

	func() {
		c := gortsplib.Client{Scheme: "rtsp", Host: "localhost:8554"}
		err := c.Start()
		require.NoError(t, err)
		defer c.Close()
		// no publisher appears, so this fails after runOnDemandStartTimeout
		c.Describe(mustParseURL("rtsp://localhost:8554/cmp1-loc1/cam1")) //nolint:errcheck
	}()

	hr.waitForTypes(t, []string{"start"}, 5*time.Second)

	// the start was refused, so the remote never started: no stop is owed
	time.Sleep(2 * time.Second)
	require.Equal(t, []string{"start"}, hr.types())
}

// start and stop for the same camera must never be reordered, even across
// path destruction and recreation.
func TestPathRunOnDemandHTTPOrderingAcrossEpisodes(t *testing.T) {
	hr := &hookRecorder{}

	hr.handler = func(w http.ResponseWriter, body map[string]any) {
		// make start slow so a stop would overtake it without serialization
		if body["type"] == "start" {
			time.Sleep(300 * time.Millisecond)
		}
		w.WriteHeader(http.StatusOK)
	}

	s := hr.server(t)
	defer s.Close()

	p, ok := newInstance(t, httpHookConf(s.URL,
		"    runOnDemandStartTimeout: 1s\n"+
			"    runOnDemandCloseAfter: 1s\n"))
	require.Equal(t, true, ok)
	defer p.Close()

	for range 2 {
		c := gortsplib.Client{Scheme: "rtsp", Host: "localhost:8554"}
		err := c.Start()
		require.NoError(t, err)
		c.Describe(mustParseURL("rtsp://localhost:8554/cmp1-loc1/cam1")) //nolint:errcheck
		c.Close()
	}

	hr.waitForTypes(t, []string{"start", "stop", "start", "stop"}, 20*time.Second)

	// every stop must be preceded by a start
	got := hr.types()
	depth := 0
	for _, ty := range got {
		if ty == "start" {
			depth++
		} else {
			depth--
		}
		require.GreaterOrEqual(t, depth, 0, "a stop preceded its start: %v", got)
		require.LessOrEqual(t, depth, 1, "two starts without a stop: %v", got)
	}
}

func mustParseURL(s string) *base.URL {
	u, err := base.ParseURL(s)
	if err != nil {
		panic(err)
	}
	return u
}

// a request marked as a health check must not summon a publisher: it fails
// fast instead of being held, and no API call is made.
func TestPathRunOnDemandHTTPExcludedQueryDoesNotStart(t *testing.T) {
	hr := &hookRecorder{}
	s := hr.server(t)
	defer s.Close()

	p, ok := newInstance(t, httpHookConf(s.URL,
		"    runOnDemandStartTimeout: 10s\n"+
			"    runOnDemandCloseAfter: 1s\n"+
			"    runOnDemandExcludeQuery: 'type=healthcheck'\n"))
	require.Equal(t, true, ok)
	defer p.Close()

	c := gortsplib.Client{Scheme: "rtsp", Host: "localhost:8554"}
	require.NoError(t, c.Start())
	defer c.Close()

	start := time.Now()
	_, _, err := c.Describe(mustParseURL(
		"rtsp://localhost:8554/cmp1-loc1/cam1?type=healthcheck"))

	// refused, and well before runOnDemandStartTimeout would have elapsed
	require.Error(t, err)
	require.Less(t, time.Since(start), 5*time.Second)

	time.Sleep(500 * time.Millisecond)
	require.Empty(t, hr.types(), "a health check must not trigger an API call")
}

// a non-matching query still starts the publisher.
func TestPathRunOnDemandHTTPNormalQueryStillStarts(t *testing.T) {
	hr := &hookRecorder{}
	s := hr.server(t)
	defer s.Close()

	p, ok := newInstance(t, httpHookConf(s.URL,
		"    runOnDemandStartTimeout: 2s\n"+
			"    runOnDemandCloseAfter: 1s\n"+
			"    runOnDemandExcludeQuery: 'type=healthcheck'\n"))
	require.Equal(t, true, ok)
	defer p.Close()

	go func() {
		c := gortsplib.Client{Scheme: "rtsp", Host: "localhost:8554"}
		if err := c.Start(); err != nil {
			return
		}
		defer c.Close()
		c.Describe(mustParseURL("rtsp://localhost:8554/cmp1-loc1/cam1?type=live")) //nolint:errcheck
	}()

	hr.waitForTypes(t, []string{"start"}, 5*time.Second)
}

// a health check that reads a live stream must not keep it alive: once the
// real viewer leaves, the stop must still fire.
func TestPathRunOnDemandHTTPExcludedQueryDoesNotSustain(t *testing.T) {
	hr := &hookRecorder{}

	var publisher *gortsplib.Client
	var publisherMutex sync.Mutex
	media0 := test.UniqueMediaH264()

	hr.handler = func(w http.ResponseWriter, body map[string]any) {
		if body["type"] == "start" {
			go func() {
				c := &gortsplib.Client{}
				err := c.StartRecording("rtsp://localhost:8554/cmp1-loc1/cam1",
					&description.Session{Medias: []*description.Media{media0}})
				if err != nil {
					return
				}
				publisherMutex.Lock()
				publisher = c
				publisherMutex.Unlock()
				c.WritePacketRTP(media0, &rtp.Packet{ //nolint:errcheck
					Version: 2, PayloadType: 96, Payload: []byte{5, 1, 2, 3, 4},
				})
			}()
		}
		w.WriteHeader(http.StatusOK)
	}

	s := hr.server(t)
	defer s.Close()

	p, ok := newInstance(t, httpHookConf(s.URL,
		"    runOnDemandStartTimeout: 5s\n"+
			"    runOnDemandCloseAfter: 2s\n"+
			"    runOnDemandExcludeQuery: 'type=healthcheck'\n"))
	require.Equal(t, true, ok)
	defer p.Close()

	// a real viewer brings the stream up
	viewer := gortsplib.Client{Scheme: "rtsp", Host: "localhost:8554"}
	require.NoError(t, viewer.Start())

	desc, _, err := viewer.Describe(mustParseURL("rtsp://localhost:8554/cmp1-loc1/cam1"))
	require.NoError(t, err)
	require.NoError(t, viewer.SetupAll(desc.BaseURL, desc.Medias))
	_, err = viewer.Play(nil)
	require.NoError(t, err)

	hr.waitForTypes(t, []string{"start"}, 5*time.Second)

	// probes keep arriving every ~500ms, well inside runOnDemandCloseAfter,
	// and keep arriving for longer than the assertion window below. If a probe
	// were allowed to cancel the pending close, the stop would never fire.
	stopProbes := make(chan struct{})
	probesDone := make(chan struct{})
	go func() {
		defer close(probesDone)
		for {
			select {
			case <-stopProbes:
				return
			default:
			}

			func() {
				pc := gortsplib.Client{Scheme: "rtsp", Host: "localhost:8554"}
				if pc.Start() != nil {
					return
				}
				defer pc.Close()

				// a real health check fetches frames, so it becomes a reader
				pdesc, _, err2 := pc.Describe(mustParseURL(
					"rtsp://localhost:8554/cmp1-loc1/cam1?type=healthcheck"))
				if err2 != nil {
					return
				}
				if pc.SetupAll(pdesc.BaseURL, pdesc.Medias) != nil {
					return
				}
				pc.Play(nil) //nolint:errcheck
				time.Sleep(100 * time.Millisecond)
			}()

			select {
			case <-stopProbes:
				return
			case <-time.After(400 * time.Millisecond):
			}
		}
	}()

	// the real viewer leaves; the probes must not hold the stream open
	viewer.Close()

	hr.waitForTypes(t, []string{"start", "stop"}, 8*time.Second)

	close(stopProbes)
	<-probesDone
	publisherMutex.Lock()
	if publisher != nil {
		publisher.Close()
	}
	publisherMutex.Unlock()
}
