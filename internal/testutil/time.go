package testutil

import "time"

type MockTime struct {
	t time.Time
}

func NewMockTime(t time.Time) *MockTime {
	return &MockTime{t: t}
}

func (m *MockTime) Now() time.Time {
	return m.t
}

func (m *MockTime) Set(t time.Time) {
	m.t = t
}

func (m *MockTime) Advance(d time.Duration) {
	m.t = m.t.Add(d)
}
