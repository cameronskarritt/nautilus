package tracer

import (
	"context"
	"time"
)

type Tracer interface {
	Start(ctx context.Context, name string, opts ...StartOption) (context.Context, Span)
}

type Span interface {
	SetAttributes(attrs ...Attribute)
	RecordError(err error)
	AddEvent(name string, opts ...EventOption)
	SetStatus(status Status, description string)
	End(opts ...EndOption)
}

type Attribute struct {
	Key   string
	Value any // string, int, bool, float64, etc.
}

type Status int

const (
	StatusUnset Status = iota
	StatusOk
	StatusError
)

type StartOption func(*StartConfig)

type StartConfig struct {
	Attributes []Attribute
}

type EventOption func(*EventConfig)

type EventConfig struct {
	Attributes []Attribute
	Timestamp  time.Time
}

type EndOption func(*EndConfig)

type EndConfig struct {
	Timestamp time.Time
}

func StringAttr(key, value string) Attribute {
	return Attribute{Key: key, Value: value}
}

func IntAttr(key string, value int) Attribute {
	return Attribute{Key: key, Value: value}
}

func Int64Attr(key string, value int64) Attribute {
	return Attribute{Key: key, Value: value}
}

func BoolAttr(key string, value bool) Attribute {
	return Attribute{Key: key, Value: value}
}

func Float64Attr(key string, value float64) Attribute {
	return Attribute{Key: key, Value: value}
}

func WithAttributes(attrs ...Attribute) EventOption {
	return func(c *EventConfig) {
		c.Attributes = append(c.Attributes, attrs...)
	}
}

func WithTimestamp(t time.Time) EventOption {
	return func(c *EventConfig) {
		c.Timestamp = t
	}
}

func WithStartAttributes(attrs ...Attribute) StartOption {
	return func(c *StartConfig) {
		c.Attributes = append(c.Attributes, attrs...)
	}
}

func WithEndTimestamp(t time.Time) EndOption {
	return func(c *EndConfig) {
		c.Timestamp = t
	}
}

func ApplyStartOptions(opts ...StartOption) StartConfig {
	var config StartConfig
	for _, opt := range opts {
		opt(&config)
	}
	return config
}

func ApplyEventOptions(opts ...EventOption) EventConfig {
	var config EventConfig
	for _, opt := range opts {
		opt(&config)
	}
	return config
}

func ApplyEndOptions(opts ...EndOption) EndConfig {
	var config EndConfig
	for _, opt := range opts {
		opt(&config)
	}
	return config
}
