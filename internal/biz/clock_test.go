package biz

import (
	"testing"
	"time"
)

func TestNewSystemClockProvidesCurrentTime(t *testing.T) {
	before := time.Now().Add(-time.Second)
	clock := NewSystemClock()
	after := time.Now().Add(time.Second)
	if now := clock.Now(); now.Before(before) || now.After(after) {
		t.Errorf("Now() = %v, want current time between %v and %v", now, before, after)
	}
}
