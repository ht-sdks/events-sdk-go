package htevents

import (
	"fmt"
	"io"
	"strconv"
	"sync"

	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

// Version of the client.
const Version = "0.0.3"

// This interface is the main API exposed by the htevents package.
// Values that satsify this interface are returned by the client constructors
// provided by the package and provide a way to send messages via the HTTP API.
type Client interface {
	io.Closer

	// Queues a message to be sent by the client when the conditions for a batch
	// upload are met.
	// This is the main method you'll be using, a typical flow would look like
	// this:
	//
	//	client := htevents.New(writeKey)
	//	...
	//	client.Enqueue(htevents.Track{ ... })
	//	...
	//	client.Close()
	//
	// The method returns an error if the message queue not be queued, which
	// happens if the client was already closed at the time the method was
	// called or if the message was malformed.
	Enqueue(Message) error
}

type client struct {
	Config
	key string

	// This channel is where the `Enqueue` method writes messages so they can be
	// picked up and pushed by the backend goroutine taking care of applying the
	// batching rules.
	msgs       chan Message
	queueSlots chan struct{}
	admission  sync.RWMutex

	// These two channels are used to synchronize the client shutting down when
	// `Close` is called.
	// The first channel is closed to signal the backend goroutine that it has
	// to stop, then the second one is closed by the backend goroutine to signal
	// that it has finished flushing all queued messages.
	quit     chan struct{}
	shutdown chan struct{}

	// This HTTP client is used to send requests to the backend, it uses the
	// HTTP transport provided in the configuration.
	http http.Client
}

// Instantiate a new client that uses the write key passed as first argument to
// send messages to the backend.
// The client is created with the default configuration.
func New(writeKey string) Client {
	// Here we can ignore the error because the default config is always valid.
	c, _ := NewWithConfig(writeKey, Config{})
	return c
}

// Instantiate a new client that uses the write key and configuration passed as
// arguments to send messages to the backend.
// The function will return an error if the configuration contained impossible
// values (like a negative flush interval for example).
// When the function returns an error the returned client will always be nil.
func NewWithConfig(writeKey string, config Config) (cli Client, err error) {
	if err = config.validate(); err != nil {
		return
	}

	c := &client{
		Config:   makeConfig(config),
		key:      writeKey,
		msgs:     make(chan Message, 100),
		quit:     make(chan struct{}),
		shutdown: make(chan struct{}),
		http:     makeHttpClient(config.Transport),
	}

	// Count messages waiting in the input channel, batch builder or recovery queue together.
	c.queueSlots = make(chan struct{}, cap(c.msgs)+c.BatchSize)
	go c.loop()

	cli = c
	return
}

func makeHttpClient(transport http.RoundTripper) http.Client {
	httpClient := http.Client{
		Transport: transport,
	}
	if supportsTimeout(transport) {
		httpClient.Timeout = 10 * time.Second
	}
	return httpClient
}

func dereferenceMessage(msg Message) Message {
	switch m := msg.(type) {
	case *Alias:
		if m == nil {
			return nil
		}
		return *m
	case *Group:
		if m == nil {
			return nil
		}
		return *m
	case *Identify:
		if m == nil {
			return nil
		}
		return *m
	case *Page:
		if m == nil {
			return nil
		}
		return *m
	case *Screen:
		if m == nil {
			return nil
		}
		return *m
	case *Track:
		if m == nil {
			return nil
		}
		return *m
	}

	return msg
}

func (c *client) Enqueue(msg Message) (err error) {
	msg = dereferenceMessage(msg)
	if err = msg.Validate(); err != nil {
		return
	}

	var id = c.uid()
	var ts = c.now()

	switch m := msg.(type) {
	case Alias:
		m.Type = "alias"
		m.MessageId = makeMessageId(m.MessageId, id)
		m.Timestamp = makeTimestamp(m.Timestamp, ts)
		msg = m

	case Group:
		m.Type = "group"
		m.MessageId = makeMessageId(m.MessageId, id)
		m.Timestamp = makeTimestamp(m.Timestamp, ts)
		msg = m

	case Identify:
		m.Type = "identify"
		m.MessageId = makeMessageId(m.MessageId, id)
		m.Timestamp = makeTimestamp(m.Timestamp, ts)
		msg = m

	case Page:
		m.Type = "page"
		m.MessageId = makeMessageId(m.MessageId, id)
		m.Timestamp = makeTimestamp(m.Timestamp, ts)
		msg = m

	case Screen:
		m.Type = "screen"
		m.MessageId = makeMessageId(m.MessageId, id)
		m.Timestamp = makeTimestamp(m.Timestamp, ts)
		msg = m

	case Track:
		m.Type = "track"
		m.MessageId = makeMessageId(m.MessageId, id)
		m.Timestamp = makeTimestamp(m.Timestamp, ts)
		msg = m

	default:
		err = fmt.Errorf("messages with custom types cannot be enqueued: %T", msg)
		return
	}

	c.admission.RLock()
	defer c.admission.RUnlock()
	select {
	case <-c.quit:
		return ErrClosed
	default:
	}
	select {
	case c.queueSlots <- struct{}{}:
	case <-c.quit:
		return ErrClosed
	}
	select {
	case c.msgs <- msg:
		return nil
	case <-c.quit:
		<-c.queueSlots
		return ErrClosed
	}
}

// Close and flush metrics.
func (c *client) Close() (err error) {
	defer func() {
		// Always recover, a panic could be raised if `c`.quit was closed which
		// means the method was called more than once.
		if recover() != nil {
			err = ErrClosed
		}
	}()
	close(c.quit)
	<-c.shutdown
	return
}

const fastAttempts = 10

type pendingUpload struct {
	queued     bool
	retryReady bool
	messages   []message
	body       []byte
	err        error
	attempt    int
}

func (c *client) prepare(msgs []message) *pendingUpload {
	b, err := json.Marshal(batch{
		MessageId: c.uid(),
		SentAt:    c.now(),
		Messages:  msgs,
		Context:   c.DefaultContext,
	})

	if err != nil {
		c.errorf("marshalling messages - %s", err)
		c.notifyFailure(msgs, err)
		return nil
	}
	return &pendingUpload{messages: msgs, body: b}
}

// Send ordinary attempts on the same worker. A closed pause channel sends
// the next eligible retry back to the queue. A nil channel identifies a deferred probe.
func (c *client) send(batch *pendingUpload, pause <-chan struct{}, completed chan<- *pendingUpload) {
	defer func() {
		if r := recover(); r != nil {
			batch.err = fmt.Errorf("upload panic: %v", r)
		}
		completed <- batch
	}()

	for {
		if batch.err = c.upload(batch.body); batch.err == nil {
			return
		}
		if (pause == nil || batch.attempt >= fastAttempts-1) && recoverable(batch.err) {
			return
		}

		// Wait for either a retry timeout or the client to be closed.
		select {
		case <-time.After(c.retryInterval(batch.attempt)):
		case <-c.quit:
			return
		}
		if batch.attempt >= fastAttempts-1 {
			return
		}
		batch.attempt++
		select {
		case <-c.quit:
			return
		case <-pause:
			batch.retryReady = true
			return
		default:
		}
	}
}

func recoverable(err error) bool {
	if parseErr, ok := err.(*url.Error); ok && parseErr.Op == "parse" {
		return false
	}
	status, ok := err.(uploadStatusError)
	return !ok || status.code == 408 || status.code == 429 ||
		(status.code >= 500 && status.code < 600)
}

// Once fast retries are exhausted, reuse the last configured interval.
func (c *client) retryInterval(attempt int) time.Duration {
	if attempt >= fastAttempts {
		attempt = fastAttempts - 1
	}
	return c.RetryAfter(attempt)
}

// Upload serialized batch message.
func (c *client) upload(b []byte) error {
	url := c.Endpoint + "/v1/batch"
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		c.errorf("creating request - %s", err)
		return err
	}

	req.Header.Add("User-Agent", "events-sdk-go (version: "+Version+")")
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Content-Length", strconv.Itoa(len(b)))
	req.SetBasicAuth(c.key, "")

	res, err := c.http.Do(req)

	if err != nil {
		c.errorf("sending request - %s", err)
		return err
	}

	defer res.Body.Close()
	return c.report(res)
}

type uploadStatusError struct {
	code   int
	status string
}

func (e uploadStatusError) Error() string { return fmt.Sprintf("%d %s", e.code, e.status) }

// Report on response body.
func (c *client) report(res *http.Response) (err error) {
	var body []byte

	if res.StatusCode < 300 {
		c.debugf("response %s", res.Status)
		return
	}

	if body, err = io.ReadAll(res.Body); err != nil {
		c.errorf("response %d %s - %s", res.StatusCode, res.Status, err)
		return
	}

	c.logf("response %d %s – %s", res.StatusCode, res.Status, string(body))
	return uploadStatusError{res.StatusCode, res.Status}
}

// Batch loop.
func (c *client) loop() {
	defer close(c.shutdown)

	tick := time.NewTicker(c.Interval)
	defer tick.Stop()
	mq := messageQueue{
		maxBatchSize:  c.BatchSize,
		maxBatchBytes: c.maxBatchBytes(),
	}

	// The loop owns admission and the deferred queue. One timer schedules
	// its next probe without occupying an upload worker.
	completed := make(chan *pendingUpload, c.maxConcurrentRequests)
	var pending []*pendingUpload
	pause := make(chan struct{})
	var retry <-chan time.Time
	active := 0
	quit := c.quit
	draining := false
	var closeError error

	upload := func(batch *pendingUpload, pause <-chan struct{}) {
		active++
		go c.send(batch, pause, completed)
	}
	releaseSlots := func(msgs []message) {
		for range msgs {
			<-c.queueSlots
		}
	}
	submit := func(msgs []message) {
		batch := c.prepare(msgs)
		if batch != nil && closeError == nil && len(pending) != 0 {
			batch.queued = true
			pending = append(pending, batch)
			return
		}
		releaseSlots(msgs)
		if batch == nil {
			return
		}
		if closeError != nil {
			c.notifyFailure(msgs, closeError)
		} else if active < c.maxConcurrentRequests {
			upload(batch, pause)
		} else {
			c.notifyFailure(msgs, ErrTooManyRequests)
		}
	}
	for {
		closing := quit == nil
		incoming := c.msgs
		if closing || (draining && len(pending) == 0 && active >= c.maxConcurrentRequests) {
			// Wait for upload capacity when draining the last queued work.
			incoming = nil
		}
		if closing && active == 0 {
			return
		}
		select {
		case msg := <-incoming:
			c.push(&mq, msg, submit)

		case <-tick.C:
			c.flush(&mq, submit)

		case <-retry:
			retry = nil
			upload(pending[0], nil)
		case result := <-completed:
			active--
			if result.retryReady && !closing {
				result.retryReady = false
				if len(pending) > 0 {
					pending = append(pending, result)
				} else {
					upload(result, pause)
				}
				continue
			}
			recovering := len(pending) > 0
			wasProbe := recovering && pending[0] == result
			terminal := result.err == nil || closing || !recoverable(result.err)
			if wasProbe {
				pending[0] = nil
				pending = pending[1:]
				if terminal && result.queued {
					releaseSlots(result.messages)
				}
			}
			if result.err == nil {
				c.notifySuccess(result.messages)
			} else if terminal {
				c.notifyFailure(result.messages, result.err)
			} else {
				if !recovering {
					close(pause)
				}
				draining = true
				result.attempt++
				pending = append(pending, result)
			}
			if wasProbe && len(pending) == 0 {
				pause = make(chan struct{})
			}
			if len(pending) == 0 && len(c.msgs) == 0 {
				draining = false
			}
			if !closing && (!recovering || wasProbe) && len(pending) != 0 {
				index := result.attempt - 1
				if index < 0 {
					index = 0
				}
				delay := c.retryInterval(index)
				if delay > time.Minute {
					delay = time.Minute
				}
				if !terminal && delay < time.Second {
					delay = time.Second
				}
				if wasProbe && result.err == nil {
					delay = 0
				}
				retry = time.After(delay)
			}
		case <-quit:
			quit = nil
			if len(pending) > 0 {
				closeError = ErrClosed
				if retry == nil {
					// The worker will report the in-flight probe.
					pending = pending[1:]
				}
			}
			for _, batch := range pending {
				err := batch.err
				if err == nil {
					err = ErrClosed
				}
				c.notifyFailure(batch.messages, err)
			}
			pending = nil
			retry = nil
			c.admission.Lock()
			c.admission.Unlock() // All concurrent enqueues have observed quit or finished sending.
			for len(c.msgs) > 0 {
				c.push(&mq, <-c.msgs, submit)
			}
			c.flush(&mq, submit)
		}
	}
}

func (c *client) push(q *messageQueue, m Message, submit func([]message)) {
	var msg message
	var err error

	if msg, err = makeMessage(m, maxMessageBytes); err != nil {
		<-c.queueSlots
		c.errorf("%s - %v", err, m)
		c.notifyFailure([]message{{m, nil}}, err)
		return
	}

	c.debugf("buffer (%d/%d) %v", len(q.pending), c.BatchSize, m)

	if msgs := q.push(msg); msgs != nil {
		c.debugf("exceeded messages batch limit with batch of %d messages – flushing", len(msgs))
		submit(msgs)
	}
}

func (c *client) flush(q *messageQueue, submit func([]message)) {
	if msgs := q.flush(); msgs != nil {
		c.debugf("flushing %d messages", len(msgs))
		submit(msgs)
	}
}

func (c *client) debugf(format string, args ...interface{}) {
	if c.Verbose {
		c.logf(format, args...)
	}
}

func (c *client) logf(format string, args ...interface{}) {
	c.Logger.Logf(format, args...)
}

func (c *client) errorf(format string, args ...interface{}) {
	c.Logger.Errorf(format, args...)
}

func (c *client) maxBatchBytes() int {
	b, _ := json.Marshal(batch{
		MessageId: c.uid(),
		SentAt:    c.now(),
		Context:   c.DefaultContext,
	})
	return maxBatchBytes - len(b)
}

func (c *client) recoverCallback() {
	if r := recover(); r != nil {
		c.errorf("panic in delivery callback: %v", r)
	}
}

func (c *client) notifySuccess(msgs []message) {
	defer c.recoverCallback()
	if c.Callback != nil {
		for _, m := range msgs {
			c.Callback.Success(m.msg)
		}
	}
}

func (c *client) notifyFailure(msgs []message, err error) {
	defer c.recoverCallback()
	if c.Callback != nil {
		for _, m := range msgs {
			c.Callback.Failure(m.msg, err)
		}
	}
}
