package kafkax

import "github.com/twmb/franz-go/pkg/kgo"

// HeaderCarrier adapts a Kafka record's headers to
// propagation.TextMapCarrier so W3C trace context travels with the record.
type HeaderCarrier struct {
	Record *kgo.Record
}

// Get returns the first header value for key, or "".
func (c HeaderCarrier) Get(key string) string {
	for _, header := range c.Record.Headers {
		if header.Key == key {
			return string(header.Value)
		}
	}

	return ""
}

// Set replaces the value of an existing header key or appends a new header.
func (c HeaderCarrier) Set(key, value string) {
	for i := range c.Record.Headers {
		if c.Record.Headers[i].Key == key {
			c.Record.Headers[i].Value = []byte(value)

			return
		}
	}

	c.Record.Headers = append(c.Record.Headers, kgo.RecordHeader{Key: key, Value: []byte(value)})
}

// Keys returns every header key in record order.
func (c HeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c.Record.Headers))
	for _, header := range c.Record.Headers {
		keys = append(keys, header.Key)
	}

	return keys
}
