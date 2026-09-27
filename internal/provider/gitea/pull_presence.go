package gitea

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

type pullPresence struct {
	mergeable           *bool
	allowMaintainerEdit *bool
}

type pullPresenceShape byte

const (
	pullPresenceObject pullPresenceShape = iota + 1
	pullPresenceArray
)

type pullPresenceContextKey struct{}

type pullPresenceCollector struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	failed   bool
	finished bool
}

func withPullPresenceCollector(ctx context.Context) (context.Context, *pullPresenceCollector) {
	collector := &pullPresenceCollector{}
	return context.WithValue(ctx, pullPresenceContextKey{}, collector), collector
}

func (c *pullPresenceCollector) fail() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.finished {
		c.failed = true
		c.buffer.Reset()
	}
}

func (c *pullPresenceCollector) write(value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished || c.failed {
		return
	}
	if int64(c.buffer.Len()+len(value)) > maxResponseBytes {
		c.failed = true
		c.buffer.Reset()
		return
	}
	_, _ = c.buffer.Write(value)
}

func (c *pullPresenceCollector) finish(shape pullPresenceShape) (map[int64]pullPresence, error) {
	c.mu.Lock()
	if c.finished {
		c.mu.Unlock()
		return nil, fmt.Errorf("presence collector already finished")
	}
	c.finished = true
	failed := c.failed
	data := append([]byte(nil), c.buffer.Bytes()...)
	c.buffer.Reset()
	c.mu.Unlock()
	if failed {
		return nil, fmt.Errorf("invalid pull presence response")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	result := map[int64]pullPresence{}
	if shape == pullPresenceArray {
		token, err := decoder.Token()
		if err != nil || token != json.Delim('[') {
			return nil, fmt.Errorf("invalid pull presence response")
		}
		for decoder.More() {
			index, presence, err := decodePullPresenceObject(decoder)
			if err != nil {
				return nil, err
			}
			if _, exists := result[index]; exists {
				return nil, fmt.Errorf("invalid pull presence response")
			}
			result[index] = presence
		}
		token, err = decoder.Token()
		if err != nil || token != json.Delim(']') {
			return nil, fmt.Errorf("invalid pull presence response")
		}
	} else if shape == pullPresenceObject {
		index, presence, err := decodePullPresenceObject(decoder)
		if err != nil {
			return nil, err
		}
		result[index] = presence
	} else {
		return nil, fmt.Errorf("invalid pull presence shape")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("invalid pull presence response")
	}
	return result, nil
}

func decodePullPresenceObject(decoder *json.Decoder) (int64, pullPresence, error) {
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return 0, pullPresence{}, fmt.Errorf("invalid pull presence response")
	}
	var index int64
	seenIndex := false
	seenMergeable := false
	seenAllow := false
	result := pullPresence{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return 0, pullPresence{}, fmt.Errorf("invalid pull presence response")
		}
		key, ok := keyToken.(string)
		if !ok {
			return 0, pullPresence{}, fmt.Errorf("invalid pull presence response")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return 0, pullPresence{}, fmt.Errorf("invalid pull presence response")
		}
		switch key {
		case "number":
			if seenIndex {
				return 0, pullPresence{}, fmt.Errorf("invalid pull presence response")
			}
			seenIndex = true
			var number json.Number
			d := json.NewDecoder(bytes.NewReader(raw))
			d.UseNumber()
			if err := d.Decode(&number); err != nil {
				return 0, pullPresence{}, fmt.Errorf("invalid pull presence response")
			}
			index, err = number.Int64()
			if err != nil || index <= 0 {
				return 0, pullPresence{}, fmt.Errorf("invalid pull presence response")
			}
		case "mergeable":
			if seenMergeable {
				return 0, pullPresence{}, fmt.Errorf("invalid pull presence response")
			}
			seenMergeable = true
			result.mergeable, err = decodeOptionalBool(raw)
			if err != nil {
				return 0, pullPresence{}, err
			}
		case "allow_maintainer_edit":
			if seenAllow {
				return 0, pullPresence{}, fmt.Errorf("invalid pull presence response")
			}
			seenAllow = true
			result.allowMaintainerEdit, err = decodeOptionalBool(raw)
			if err != nil {
				return 0, pullPresence{}, err
			}
		}
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') || !seenIndex {
		return 0, pullPresence{}, fmt.Errorf("invalid pull presence response")
	}
	return index, result, nil
}
func decodeOptionalBool(raw json.RawMessage) (*bool, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("invalid pull presence response")
	}
	return &value, nil
}

type pullPresenceTransport struct{ base http.RoundTripper }

func (t *pullPresenceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil || response == nil || response.StatusCode/100 != 2 {
		return response, err
	}
	collector, _ := request.Context().Value(pullPresenceContextKey{}).(*pullPresenceCollector)
	if collector == nil || response.Body == nil {
		return response, nil
	}
	response.Body = &presenceReadCloser{base: response.Body, collector: collector}
	return response, nil
}

type presenceReadCloser struct {
	base      io.ReadCloser
	collector *pullPresenceCollector
}

func (r *presenceReadCloser) Read(p []byte) (int, error) {
	n, err := r.base.Read(p)
	if n > 0 {
		r.collector.write(p[:n])
	}
	return n, err
}
func (r *presenceReadCloser) Close() error {
	err := r.base.Close()
	if err != nil {
		r.collector.fail()
	}
	return err
}
