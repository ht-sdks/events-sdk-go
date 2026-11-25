package htevents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Helper type used to implement the io.Reader interface on function values.
type readFunc func([]byte) (int, error)

func (f readFunc) Read(b []byte) (int, error) { return f(b) }

// Helper type used to implement the http.RoundTripper interface on function
// values.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func (f roundTripperFunc) CancelRequest(r *http.Request) {}

// Instances of this type are used to mock the client callbacks in unit tests.
type testCallback struct {
	success func(Message)
	failure func(Message, error)
}

func (c testCallback) Success(m Message) {
	if c.success != nil {
		c.success(m)
	}
}

func (c testCallback) Failure(m Message, e error) {
	if c.failure != nil {
		c.failure(m, e)
	}
}

// Instances of this type are used to mock the client logger in unit tests.
type testLogger struct {
	logf   func(string, ...interface{})
	errorf func(string, ...interface{})
}

func (l testLogger) Logf(format string, args ...interface{}) {
	if l.logf != nil {
		l.logf(format, args...)
	}
}

func (l testLogger) Errorf(format string, args ...interface{}) {
	if l.errorf != nil {
		l.errorf(format, args...)
	}
}

var _ Message = (*testErrorMessage)(nil)

// Instances of this type are used to force message validation errors in unit
// tests.
type testErrorMessage struct{}

func (m testErrorMessage) Validate() error { return testError }

var (
	// A control error returned by mock functions to emulate a failure.
	testError = errors.New("test error")

	// HTTP transport that always succeeds.
	testTransportOK = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			Status:     http.StatusText(http.StatusOK),
			StatusCode: http.StatusOK,
			Proto:      r.Proto,
			ProtoMajor: r.ProtoMajor,
			ProtoMinor: r.ProtoMinor,
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	})

	// HTTP transport that sleeps for a little while and eventually succeeds.
	testTransportDelayed = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		time.Sleep(10 * time.Millisecond)
		return testTransportOK.RoundTrip(r)
	})

	// HTTP transport that always returns a 400.
	testTransportBadRequest = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			Status:     http.StatusText(http.StatusBadRequest),
			StatusCode: http.StatusBadRequest,
			Proto:      r.Proto,
			ProtoMajor: r.ProtoMajor,
			ProtoMinor: r.ProtoMinor,
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	})

	// HTTP transport that always returns a 400 with an erroring body reader.
	testTransportBodyError = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			Status:     http.StatusText(http.StatusBadRequest),
			StatusCode: http.StatusBadRequest,
			Proto:      r.Proto,
			ProtoMajor: r.ProtoMajor,
			ProtoMinor: r.ProtoMinor,
			Body:       io.NopCloser(readFunc(func(b []byte) (int, error) { return 0, testError })),
			Request:    r,
		}, nil
	})

	// HTTP transport that always return an error.
	testTransportError = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return nil, testError
	})
)

func fixture(name string) string {
	f, err := os.Open(filepath.Join("fixtures", name))
	if err != nil {
		panic(err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func mockId() string { return "I'm unique" }

func mockTime() time.Time {
	// time.Unix(0, 0) fails on Circle
	return time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
}

func mockServer() (chan []byte, *httptest.Server) {
	done := make(chan []byte, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := bytes.NewBuffer(nil)
		io.Copy(buf, r.Body)

		var v interface{}
		err := json.Unmarshal(buf.Bytes(), &v)
		if err != nil {
			panic(err)
		}

		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			panic(err)
		}

		done <- b
	}))

	return done, server
}

func ExampleTrack() {
	body, server := mockServer()
	defer server.Close()

	client, _ := NewWithConfig("<write_key>", Config{
		Endpoint:  server.URL,
		BatchSize: 1,
		now:       mockTime,
		uid:       mockId,
	})
	defer client.Close()

	client.Enqueue(Track{
		Event:  "Download",
		UserId: "123456",
		Properties: Properties{
			"application": "HT Desktop",
			"version":     "1.1.0",
			"platform":    "osx",
		},
	})

	fmt.Printf("%s\n", <-body)
	// Output:
	// {
	//   "batch": [
	//     {
	//       "event": "Download",
	//       "messageId": "I'm unique",
	//       "properties": {
	//         "application": "HT Desktop",
	//         "platform": "osx",
	//         "version": "1.1.0"
	//       },
	//       "timestamp": "2009-11-10T23:00:00Z",
	//       "type": "track",
	//       "userId": "123456"
	//     }
	//   ],
	//   "context": {
	//     "library": {
	//       "name": "events-sdk-go",
	//       "version": "0.0.3"
	//     }
	//   },
	//   "messageId": "I'm unique",
	//   "sentAt": "2009-11-10T23:00:00Z"
	// }
}

func TestEnqueue(t *testing.T) {
	tests := map[string]struct {
		ref string
		msg Message
	}{
		"alias": {
			fixture("test-enqueue-alias.json"),
			Alias{PreviousId: "A", UserId: "B"},
		},

		"group": {
			fixture("test-enqueue-group.json"),
			Group{GroupId: "A", UserId: "B"},
		},

		"identify": {
			fixture("test-enqueue-identify.json"),
			Identify{UserId: "B"},
		},

		"page": {
			fixture("test-enqueue-page.json"),
			Page{Name: "A", UserId: "B"},
		},

		"screen": {
			fixture("test-enqueue-screen.json"),
			Screen{Name: "A", UserId: "B"},
		},

		"track": {
			fixture("test-enqueue-track.json"),
			Track{
				Event:  "Download",
				UserId: "123456",
				Properties: Properties{
					"application": "HT Desktop",
					"version":     "1.1.0",
					"platform":    "osx",
				},
			},
		},
		"*alias": {
			fixture("test-enqueue-alias.json"),
			&Alias{PreviousId: "A", UserId: "B"},
		},

		"*group": {
			fixture("test-enqueue-group.json"),
			&Group{GroupId: "A", UserId: "B"},
		},

		"*identify": {
			fixture("test-enqueue-identify.json"),
			&Identify{UserId: "B"},
		},

		"*page": {
			fixture("test-enqueue-page.json"),
			&Page{Name: "A", UserId: "B"},
		},

		"*screen": {
			fixture("test-enqueue-screen.json"),
			&Screen{Name: "A", UserId: "B"},
		},

		"*track": {
			fixture("test-enqueue-track.json"),
			&Track{
				Event:  "Download",
				UserId: "123456",
				Properties: Properties{
					"application": "HT Desktop",
					"version":     "1.1.0",
					"platform":    "osx",
				},
			},
		},
	}

	body, server := mockServer()
	defer server.Close()

	client, _ := NewWithConfig("<write_key>", Config{
		Endpoint:  server.URL,
		Verbose:   true,
		Logger:    t,
		BatchSize: 1,
		now:       mockTime,
		uid:       mockId,
	})
	defer client.Close()

	for name, test := range tests {
		if err := client.Enqueue(test.msg); err != nil {
			t.Error(err)
			return
		}

		if res := string(<-body); res != test.ref {
			t.Errorf("%s: invalid response:\n- expected %s\n- received: %s", name, test.ref, res)
		}
	}
}

var _ Message = (*customMessage)(nil)

type customMessage struct {
}

func (c *customMessage) Validate() error {
	return nil
}

func TestEnqueuingCustomTypeFails(t *testing.T) {
	client := New("0123456789")
	err := client.Enqueue(&customMessage{})

	if err.Error() != "messages with custom types cannot be enqueued: *htevents.customMessage" {
		t.Errorf("invalid/missing error when queuing unsupported message: %v", err)
	}
}

func TestTrackWithInterval(t *testing.T) {
	const interval = 100 * time.Millisecond
	var ref = fixture("test-interval-track.json")

	body, server := mockServer()
	defer server.Close()

	t0 := time.Now()

	client, _ := NewWithConfig("<write_key>", Config{
		Endpoint: server.URL,
		Interval: interval,
		Verbose:  true,
		Logger:   t,
		now:      mockTime,
		uid:      mockId,
	})
	defer client.Close()

	client.Enqueue(Track{
		Event:  "Download",
		UserId: "123456",
		Properties: Properties{
			"application": "HT Desktop",
			"version":     "1.1.0",
			"platform":    "osx",
		},
	})

	// Will flush in 100 milliseconds
	if res := string(<-body); ref != res {
		t.Errorf("invalid response:\n- expected %s\n- received: %s", ref, res)
	}

	if t1 := time.Now(); t1.Sub(t0) < interval {
		t.Error("the flushing interval is too short:", interval)
	}
}

func TestTrackWithTimestamp(t *testing.T) {
	var ref = fixture("test-timestamp-track.json")

	body, server := mockServer()
	defer server.Close()

	client, _ := NewWithConfig("<write_key>", Config{
		Endpoint:  server.URL,
		Verbose:   true,
		Logger:    t,
		BatchSize: 1,
		now:       mockTime,
		uid:       mockId,
	})
	defer client.Close()

	client.Enqueue(Track{
		Event:  "Download",
		UserId: "123456",
		Properties: Properties{
			"application": "HT Desktop",
			"version":     "1.1.0",
			"platform":    "osx",
		},
		Timestamp: time.Date(2015, time.July, 10, 23, 0, 0, 0, time.UTC),
	})

	if res := string(<-body); ref != res {
		t.Errorf("invalid response:\n- expected %s\n- received: %s", ref, res)
	}
}

func TestTrackWithMessageId(t *testing.T) {
	var ref = fixture("test-messageid-track.json")

	body, server := mockServer()
	defer server.Close()

	client, _ := NewWithConfig("<write_key>", Config{
		Endpoint:  server.URL,
		Verbose:   true,
		Logger:    t,
		BatchSize: 1,
		now:       mockTime,
		uid:       mockId,
	})
	defer client.Close()

	client.Enqueue(Track{
		Event:  "Download",
		UserId: "123456",
		Properties: Properties{
			"application": "HT Desktop",
			"version":     "1.1.0",
			"platform":    "osx",
		},
		MessageId: "abc",
	})

	if res := string(<-body); ref != res {
		t.Errorf("invalid response:\n- expected %s\n- received: %s", ref, res)
	}
}

func TestTrackWithContext(t *testing.T) {
	var ref = fixture("test-context-track.json")

	body, server := mockServer()
	defer server.Close()

	client, _ := NewWithConfig("<write_key>", Config{
		Endpoint:  server.URL,
		Verbose:   true,
		Logger:    t,
		BatchSize: 1,
		now:       mockTime,
		uid:       mockId,
	})
	defer client.Close()

	client.Enqueue(Track{
		Event:  "Download",
		UserId: "123456",
		Properties: Properties{
			"application": "HT Desktop",
			"version":     "1.1.0",
			"platform":    "osx",
		},
		Context: &Context{
			Extra: map[string]interface{}{
				"whatever": "here",
			},
		},
	})

	if res := string(<-body); ref != res {
		t.Errorf("invalid response:\n- expected %s\n- received: %s", ref, res)
	}
}

func TestTrackMany(t *testing.T) {
	var ref = fixture("test-many-track.json")

	body, server := mockServer()
	defer server.Close()

	client, _ := NewWithConfig("<write_key>", Config{
		Endpoint:  server.URL,
		Verbose:   true,
		Logger:    t,
		BatchSize: 3,
		now:       mockTime,
		uid:       mockId,
	})
	defer client.Close()

	for i := 0; i < 5; i++ {
		client.Enqueue(Track{
			Event:  "Download",
			UserId: "123456",
			Properties: Properties{
				"application": "HT Desktop",
				"version":     i,
			},
		})
	}

	if res := string(<-body); ref != res {
		t.Errorf("invalid response:\n- expected %s\n- received: %s", ref, res)
	}
}

func TestTrackWithIntegrations(t *testing.T) {
	var ref = fixture("test-integrations-track.json")

	body, server := mockServer()
	defer server.Close()

	client, _ := NewWithConfig("<write_key>", Config{
		Endpoint:  server.URL,
		Verbose:   true,
		Logger:    t,
		BatchSize: 1,
		now:       mockTime,
		uid:       mockId,
	})
	defer client.Close()

	client.Enqueue(Track{
		Event:  "Download",
		UserId: "123456",
		Properties: Properties{
			"application": "HT Desktop",
			"version":     "1.1.0",
			"platform":    "osx",
		},
		Integrations: Integrations{
			"All":      true,
			"Intercom": false,
			"Mixpanel": true,
		},
	})

	if res := string(<-body); ref != res {
		t.Errorf("invalid response:\n- expected %s\n- received: %s", ref, res)
	}
}

func TestClientCloseTwice(t *testing.T) {
	client := New("0123456789")

	if err := client.Close(); err != nil {
		t.Error("closing a client should not a return an error")
	}

	if err := client.Close(); err != ErrClosed {
		t.Error("closing a client a second time should return ErrClosed:", err)
	}

	if err := client.Enqueue(Track{UserId: "1", Event: "A"}); err != ErrClosed {
		t.Error("using a client after it was closed should return ErrClosed:", err)
	}
}

func TestClientConfigError(t *testing.T) {
	client, err := NewWithConfig("0123456789", Config{
		Interval: -1 * time.Second,
	})

	if err == nil {
		t.Error("no error returned when creating a client with an invalid config")
	}

	if _, ok := err.(ConfigError); !ok {
		t.Errorf("invalid error type returned when creating a client with an invalid config: %T", err)
	}

	if client != nil {
		t.Error("invalid non-nil client object returned when creating a client with and invalid config:", client)
		client.Close()
	}
}

func TestClientEnqueueError(t *testing.T) {
	client := New("0123456789")
	defer client.Close()

	if err := client.Enqueue(testErrorMessage{}); err != testError {
		t.Error("invlaid error returned when queueing an invalid message:", err)
	}
}

func TestClientCallback(t *testing.T) {
	reschan := make(chan bool, 1)
	errchan := make(chan error, 1)

	client, _ := NewWithConfig("0123456789", Config{
		Logger: testLogger{t.Logf, t.Logf},
		Callback: testCallback{
			func(m Message) { reschan <- true },
			func(m Message, e error) { errchan <- e },
		},
		Transport: testTransportOK,
	})

	client.Enqueue(Track{
		UserId: "A",
		Event:  "B",
	})
	client.Close()

	select {
	case <-reschan:
	case err := <-errchan:
		t.Error("failure callback triggered:", err)
	}
}

func TestClientMarshalMessageError(t *testing.T) {
	errchan := make(chan error, 1)

	client, _ := NewWithConfig("0123456789", Config{
		Logger: testLogger{t.Logf, t.Logf},
		Callback: testCallback{
			nil,
			func(m Message, e error) { errchan <- e },
		},
		Transport: testTransportOK,
	})

	// Functions cannot be serializable, this should break the JSON marshaling
	// and trigger the failure callback.
	client.Enqueue(Track{
		UserId:     "A",
		Event:      "B",
		Properties: Properties{"invalid": func() {}},
	})
	client.Close()

	if err := <-errchan; err == nil {
		t.Error("failure callback not triggered for unserializable message")

	} else if _, ok := err.(*json.UnsupportedTypeError); !ok {
		t.Errorf("invalid error type returned by unserializable message: %T", err)
	}
}

func TestClientMarshalContextError(t *testing.T) {
	errchan := make(chan error, 1)

	client, _ := NewWithConfig("0123456789", Config{
		Logger: testLogger{t.Logf, t.Logf},
		Callback: testCallback{
			nil,
			func(m Message, e error) { errchan <- e },
		},
		DefaultContext: &Context{
			// The context set on the batch message is invalid this should also
			// cause the batched message to fail to be serialized and call the
			// failure callback.
			Extra: map[string]interface{}{"invalid": func() {}},
		},
		Transport: testTransportOK,
	})

	client.Enqueue(Track{UserId: "A", Event: "B"})
	client.Close()

	if err := <-errchan; err == nil {
		t.Error("failure callback not triggered for unserializable context")

	} else if _, ok := err.(*json.MarshalerError); !ok {
		t.Errorf("invalid error type returned by unserializable context: %T", err)
	}
}

func TestClientNewRequestError(t *testing.T) {
	for _, endpoint := range []string{"://localhost:80", "ftp://localhost"} {
		t.Run(endpoint, func(t *testing.T) {
			errchan := make(chan error, 1)

			attempts := 0
			client := newRecoveryClient(t, "0123456789", Config{
				Endpoint:   endpoint,
				BatchSize:  1,
				RetryAfter: func(int) time.Duration { attempts++; return 0 },
				Logger:     testLogger{t.Logf, t.Logf},
				Callback: testCallback{
					nil,
					func(m Message, e error) { errchan <- e },
				},
			})

			client.Enqueue(Track{UserId: "A", Event: "B"})
			select {
			case err := <-errchan:
				if err == nil || attempts != fastAttempts {
					t.Fatalf("attempts=%d, error=%v", attempts, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("malformed endpoint entered deferred recovery")
			}
		})
	}
}

func TestClientRoundTripperError(t *testing.T) {
	errchan := make(chan error, 1)

	client, _ := NewWithConfig("0123456789", Config{
		Logger: testLogger{t.Logf, t.Logf},
		Callback: testCallback{
			nil,
			func(m Message, e error) { errchan <- e },
		},
		Transport: testTransportError,
	})

	client.Enqueue(Track{UserId: "A", Event: "B"})
	client.Close()

	if err := <-errchan; err == nil {
		t.Error("failure callback not triggered for an invalid request")

	} else if e, ok := err.(*url.Error); !ok {
		t.Errorf("invalid error returned by round tripper: %T: %s", err, err)

	} else if e.Err != testError {
		t.Errorf("invalid error returned by round tripper: %T: %s", e.Err, e.Err)
	}
}

func TestClientRetryError(t *testing.T) {
	errchan := make(chan error, 1)

	client, _ := NewWithConfig("0123456789", Config{
		Logger: testLogger{t.Logf, t.Logf},
		Callback: testCallback{
			nil,
			func(m Message, e error) { errchan <- e },
		},
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return nil, testError
		}),
		BatchSize:  1,
		RetryAfter: func(i int) time.Duration { return time.Millisecond },
	})

	client.Enqueue(Track{UserId: "A", Event: "B"})

	// Allow the fast retries to exhaust before closing the deferred batch.
	time.Sleep(50 * time.Millisecond)
	client.Close()

	if err := <-errchan; err == nil {
		t.Error("failure callback not triggered when closing a deferred upload")

	} else if e, ok := err.(*url.Error); !ok {
		t.Errorf("invalid error returned by round tripper: %T: %s", err, err)

	} else if e.Err != testError {
		t.Errorf("invalid error returned by round tripper: %T: %s", e.Err, e.Err)
	}

	client.Close()
}

func newRecoveryClient(t *testing.T, key string, config Config) Client {
	t.Helper()
	c, err := NewWithConfig(key, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func enqueueRecoveryTrack(t *testing.T, c Client, track Track) {
	t.Helper()
	if err := c.Enqueue(track); err != nil {
		t.Fatal(err)
	}
}

func TestUnauthorizedExhaustsOriginalAttempts(t *testing.T) {
	done := make(chan error, 1)
	requests := 0
	c := newRecoveryClient(t, "original", Config{
		BatchSize: 1, RetryAfter: func(int) time.Duration { return 0 },
		Callback: testCallback{nil, func(_ Message, err error) { done <- err }},
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(""))}, nil
		}),
	})
	enqueueRecoveryTrack(t, c, Track{UserId: "user", Event: "track"})
	select {
	case err := <-done:
		if err == nil || requests != 10 {
			t.Fatalf("requests=%d, error=%v", requests, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unauthorized batch never finished")
	}
}

// Signal when the tenth failure schedules deferred delivery, before enqueuing fresh work.
func signalDeferredRetry(deferred chan<- struct{}, delay time.Duration) func(int) time.Duration {
	return func(attempt int) time.Duration {
		if attempt == 9 {
			select {
			case deferred <- struct{}{}:
			default:
			}
		}
		return delay
	}
}

func TestDeferredUploadsRecoverAfterFastRetries(t *testing.T) {
	done := make(chan error, 3)
	deferred := make(chan struct{}, 1)
	var requests []time.Time
	c := newRecoveryClient(t, "original", Config{
		BatchSize: 1, maxConcurrentRequests: 1,
		RetryAfter: signalDeferredRetry(deferred, 0),
		Callback:   testCallback{func(Message) { done <- nil }, func(_ Message, err error) { done <- err }},
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			requests = append(requests, time.Now())
			status := http.StatusServiceUnavailable
			// The original batch recovers; the fresh second batch fails its first probe.
			if len(requests) > fastAttempts && len(requests) != fastAttempts+2 {
				status = http.StatusOK
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
		}),
	})
	send := func(id string) {
		enqueueRecoveryTrack(t, c, Track{UserId: "user", Event: "track", MessageId: id})
	}
	send("first")
	select {
	case <-deferred:
	case <-time.After(3 * time.Second):
		t.Fatal("fast retries never exhausted")
	}
	send("second")
	send("third")
	for i := 0; i < 3; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("pending events did not recover")
		}
	}
	if len(requests) != fastAttempts+4 || requests[fastAttempts+2].Sub(requests[fastAttempts+1]) < time.Second {
		t.Fatal("fresh batch failure did not gate the next probe", requests)
	}
}

func TestOrdinaryTransientRetryLeavesOtherUploadSlotsAvailable(t *testing.T) {
	waiting := make(chan struct{}, 1)
	delivered := make(chan Message, 2)
	c := newRecoveryClient(t, "key", Config{
		BatchSize: 1, maxConcurrentRequests: 2,
		RetryAfter: func(int) time.Duration { waiting <- struct{}{}; return 2 * time.Minute },
		Callback:   testCallback{func(m Message) { delivered <- m }, nil},
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			var payload struct{ Batch []Track }
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				return nil, err
			}
			status := http.StatusOK
			if payload.Batch[0].Event == "A" {
				status = http.StatusServiceUnavailable
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
		}),
	})
	enqueueRecoveryTrack(t, c, Track{UserId: "user", Event: "A"})
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("A did not enter ordinary backoff")
	}
	enqueueRecoveryTrack(t, c, Track{UserId: "user", Event: "B"})
	select {
	case m := <-delivered:
		if m.(Track).Event != "B" {
			t.Fatalf("unexpected delivery: %v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("A's ordinary retry blocked B")
	}
}

func TestDeferredBatchYieldsAfterSourceCooldown(t *testing.T) {
	t.Run("new batch", func(t *testing.T) { testDeferredBatchYields(t, false) })
	t.Run("batch in ordinary backoff", func(t *testing.T) { testDeferredBatchYields(t, true) })
}

func testDeferredBatchYields(t *testing.T, retrying bool) {
	type attempt struct {
		event string
		at    time.Time
	}
	attempts := make(chan attempt, 32)
	firstAttempts := 0
	deferred := make(chan struct{}, 1)
	slots := 1
	retryAfter := signalDeferredRetry(deferred, 10*time.Millisecond)
	waiting := make(chan struct{})
	resume := make(chan struct{}, 1)
	if retrying {
		slots = 2
		ordinaryBackoff := retryAfter
		retryAfter = func(attempt int) time.Duration {
			// B enters backoff before A is enqueued.
			select {
			case <-waiting:
			default:
				close(waiting)
				<-resume
			}
			return ordinaryBackoff(attempt)
		}
	}
	failB := retrying
	c := newRecoveryClient(t, "original", Config{
		BatchSize: 1, maxConcurrentRequests: slots,
		RetryAfter: retryAfter,
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			var payload struct{ Batch []Track }
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				return nil, err
			}
			event := payload.Batch[0].Event
			status := http.StatusOK
			if event == "A" {
				firstAttempts++
				if firstAttempts <= 11 {
					status = http.StatusTooManyRequests
				}
			} else if failB {
				failB = false
				status = http.StatusServiceUnavailable
			}
			attempts <- attempt{event, time.Now()}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
		}),
	})
	next := func() attempt {
		select {
		case attempt := <-attempts:
			return attempt
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for upload")
		}
		return attempt{}
	}
	defer close(resume)
	if retrying {
		enqueueRecoveryTrack(t, c, Track{UserId: "user", Event: "B"})
		next()
		select {
		case <-waiting:
		case <-time.After(3 * time.Second):
			t.Fatal("B did not enter ordinary backoff")
		}
	}
	enqueueRecoveryTrack(t, c, Track{UserId: "user", Event: "A"})
	for i := 0; i < 10; i++ {
		next()
	}
	select {
	case <-deferred:
	case <-time.After(3 * time.Second):
		t.Fatal("batch did not reach deferred delivery")
	}
	if retrying {
		resume <- struct{}{}
	} else {
		enqueueRecoveryTrack(t, c, Track{UserId: "user", Event: "B"})
	}
	last := next()
	if last.event != "A" {
		t.Fatalf("expected deferred attempt of A, got %s", last.event)
	}
	healthy := next()
	if healthy.event != "B" || healthy.at.Sub(last.at) < time.Second {
		t.Fatalf("expected B after the source cooldown, got %s after %s", healthy.event, healthy.at.Sub(last.at))
	}
	if got := next(); got.event != "A" {
		t.Errorf("expected A to recover, got %s", got.event)
	}
}

func TestClientResponse400(t *testing.T) {
	errchan := make(chan error, 1)

	client, _ := NewWithConfig("0123456789", Config{
		Logger: testLogger{t.Logf, t.Logf},
		Callback: testCallback{
			nil,
			func(m Message, e error) { errchan <- e },
		},
		// This HTTP transport always return 400's.
		Transport: testTransportBadRequest,
	})

	client.Enqueue(Track{UserId: "A", Event: "B"})
	client.Close()

	if err := <-errchan; err == nil {
		t.Error("failure callback not triggered for a 400 response")
	}
}

func TestClientResponseBodyError(t *testing.T) {
	errchan := make(chan error, 1)

	client, _ := NewWithConfig("0123456789", Config{
		Logger: testLogger{t.Logf, t.Logf},
		Callback: testCallback{
			nil,
			func(m Message, e error) { errchan <- e },
		},
		// This HTTP transport always return 400's with an erroring body.
		Transport: testTransportBodyError,
	})

	client.Enqueue(Track{UserId: "A", Event: "B"})
	client.Close()

	if err := <-errchan; err == nil {
		t.Error("failure callback not triggered for a 400 response")

	} else if err != testError {
		t.Errorf("invalid error returned by erroring response body: %T: %s", err, err)
	}
}

func TestUploadCallbackCanFillInputQueue(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(fmt.Sprintf("success=%v", success), func(t *testing.T) {
			var c Client
			done := make(chan error, 1)
			callback := func(m Message) {
				if m.(Track).Event != "first" {
					return
				}
				for i := 0; i <= 100; i++ {
					if err := c.Enqueue(Track{UserId: "user", Event: "next"}); err != nil {
						done <- err
						return
					}
				}
				done <- nil
			}
			transport := testTransportOK
			if !success {
				transport = testTransportBadRequest
			}
			c = newRecoveryClient(t, "key", Config{
				BatchSize: 1, Transport: transport, RetryAfter: func(int) time.Duration { return 0 },
				Callback: testCallback{callback, func(m Message, _ error) { callback(m) }},
			})
			enqueueRecoveryTrack(t, c, Track{UserId: "user", Event: "first"})
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("upload callback blocked the input queue")
			}
		})
	}
}

func TestClientMaxConcurrentRequests(t *testing.T) {
	reschan := make(chan bool, 1)
	errchan := make(chan error, 1)

	logged := make(chan string, 1)
	client, _ := NewWithConfig("0123456789", Config{
		Logger: testLogger{t.Logf, func(format string, args ...interface{}) { logged <- fmt.Sprintf(format, args...) }},
		Callback: testCallback{
			func(m Message) { reschan <- true },
			func(m Message, e error) { errchan <- e },
		},
		Transport: testTransportDelayed,
		// Only one concurreny request can be submitted, because the transport
		// introduces a short delay one of the uploads should fail.
		BatchSize:             1,
		maxConcurrentRequests: 1,
	})

	client.Enqueue(Track{UserId: "A", Event: "B"})
	client.Enqueue(Track{UserId: "A", Event: "B"})
	client.Close()

	select {
	case msg := <-logged:
		if !strings.Contains(msg, ErrTooManyRequests.Error()) {
			t.Errorf("unexpected saturation log: %s", msg)
		}
	default:
		t.Error("missing error log for saturated upload slots")
	}
	if _, ok := <-reschan; !ok {
		t.Error("one of the requests should have succeeded but the result channel was empty")
	}

	if err := <-errchan; err == nil {
		t.Error("failure callback not triggered after reaching the request limit")

	} else if err != ErrTooManyRequests {
		t.Errorf("invalid error returned by erroring response body: %T: %s", err, err)
	}
}
