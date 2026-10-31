package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ccremer/fronius-exporter/pkg/fronius"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

func meterSample(ts string, values ...float64) *fronius.SymoMeterRealtimeData {
	d := &fronius.SymoMeterRealtimeData{TimeStamp: json.RawMessage(ts)}
	if len(values) > 0 {
		d.VoltageACPhase1 = &values[0]
	}
	if len(values) > 1 {
		d.VoltageACPhase2 = &values[1]
	}
	if len(values) > 2 {
		d.VoltageACPhase3 = &values[2]
	}
	return d
}

func TestVoltageSampleValidation(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		ts     string
		values []float64
		valid  bool
	}{
		{"1700000000", []float64{230, 0, 501}, true}, {"1700000600", []float64{230}, true}, {"1699999400", []float64{230}, true},
		{"1700000601", []float64{230}, false}, {"1699999399", []float64{230}, false}, {"0", []float64{230}, false}, {`"bad"`, []float64{230}, false}, {"null", []float64{230}, false}, {"1700000000", []float64{math.NaN(), math.Inf(1), 0}, false},
	} {
		_, ok := sampleVoltage(meterSample(tc.ts, tc.values...), now)
		require.Equal(t, tc.valid, ok, tc.ts)
	}
	s, ok := sampleVoltage(meterSample("1700000000", 230, 0, 501), now)
	require.True(t, ok)
	require.Equal(t, [3]float64{230, -1, -1}, s.voltages)
}

func TestVoltageStatistics(t *testing.T) {
	var a voltageAccumulator
	a.add([3]float64{220, -1, 240})
	a.add([3]float64{240, 230, -1})
	a.add([3]float64{230, 240, -1})
	stats := a.snapshot()
	require.Equal(t, 220.0, stats[0].min)
	require.Equal(t, 240.0, stats[0].max)
	require.Equal(t, 230.0, stats[0].average())
	require.Equal(t, 235.0, stats[1].average())
	require.Equal(t, 240.0, stats[2].average())
	a.reset()
	require.Equal(t, 0, a.snapshot()[0].count)
}

func TestReporterIntervalAndPayload(t *testing.T) {
	requests := make(chan map[string]any, 4)
	reporter := newVoltageReporter(52.5, 13.4, &http.Client{Timeout: time.Second, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		requests <- payload
		return &http.Response{StatusCode: 202, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}, "http://example.test")
	now := time.Unix(1700000000, 0)
	reporter.Observe(meterSample("1700000000", 230, 0, 240), now)
	first := <-requests
	require.Equal(t, 230.0, first["l1_v"])
	require.Equal(t, -1.0, first["l2_v"])
	require.NotContains(t, first, "l1_v_avg")
	require.NotContains(t, first, "grid_frequency_hz")
	require.Eventually(t, func() bool { reporter.mu.Lock(); defer reporter.mu.Unlock(); return !reporter.inFlight }, time.Second, time.Millisecond)
	reporter.Observe(meterSample("1700000100", 220, 230), now.Add(100*time.Second))
	select {
	case <-requests:
		t.Fatal("sent too early")
	default:
	}
	reporter.Observe(meterSample("1700000300", 240, 240), now.Add(300*time.Second))
	next := <-requests
	require.Equal(t, 220.0, next["l1_v_min"])
	require.Equal(t, 240.0, next["l1_v_max"])
	require.Equal(t, 230.0, next["l1_v_avg"])
	require.Equal(t, 235.0, next["l2_v_avg"])
	require.NotContains(t, next, "l3_v_avg")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReporterConcurrentAndFailedAttempts(t *testing.T) {
	started := make(chan struct{}, 4)
	unblock := make(chan struct{})
	reporter := newVoltageReporter(0, 0, &http.Client{Timeout: time.Second, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-unblock
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}, "http://example.test")
	now := time.Unix(1700000000, 0)
	reporter.Observe(meterSample("1700000000", 230), now)
	<-started
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); reporter.Observe(meterSample("1700000300", 220), now.Add(300*time.Second)) }()
	}
	wg.Wait()
	select {
	case <-started:
		t.Fatal("overlapping request")
	default:
	}
	unblock <- struct{}{}
	require.Eventually(t, func() bool { reporter.mu.Lock(); defer reporter.mu.Unlock(); return !reporter.inFlight }, time.Second, time.Millisecond)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); reporter.Observe(meterSample("1700000300", 240), now.Add(300*time.Second)) }()
	}
	wg.Wait()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("no second attempt")
	}
	select {
	case <-started:
		t.Fatal("duplicate attempt")
	default:
	}
	unblock <- struct{}{}
}

func TestAverageStaysWithinObservedRange(t *testing.T) {
	var a voltageAccumulator
	for i := 0; i < 10; i++ {
		a.add([3]float64{230.1, -1, -1})
	}
	s := a.snapshot()[0]
	require.GreaterOrEqual(t, s.average(), s.min)
	require.LessOrEqual(t, s.average(), s.max)
}

func TestReporterDiscardsFailedAttempts(t *testing.T) {
	for _, networkError := range []bool{false, true} {
		t.Run(fmt.Sprint(networkError), func(t *testing.T) {
			sent := make(chan map[string]any, 4)
			reporter := newVoltageReporter(0, 0, &http.Client{Timeout: time.Second, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var p map[string]any
				_ = json.NewDecoder(r.Body).Decode(&p)
				sent <- p
				if networkError {
					return nil, fmt.Errorf("network failure")
				}
				return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			})}, "http://example.test")
			now := time.Unix(1700000000, 0)
			observe := func(ts string, seconds int, v float64) {
				reporter.Observe(meterSample(ts, v), now.Add(time.Duration(seconds)*time.Second))
			}
			wait := func() {
				require.Eventually(t, func() bool { reporter.mu.Lock(); defer reporter.mu.Unlock(); return !reporter.inFlight }, time.Second, time.Millisecond)
			}
			observe("1700000000", 0, 210)
			<-sent
			wait()
			observe("1700000100", 100, 220)
			select {
			case <-sent:
				t.Fatal("retried too early")
			default:
			}
			observe("1700000300", 300, 240)
			p := <-sent
			require.Equal(t, 220.0, p["l1_v_min"])
			wait()
			observe("1700000600", 600, 250)
			p = <-sent
			require.Equal(t, 250.0, p["l1_v_min"])
			wait()
		})
	}
}

func TestReporterLogsOutcomeAndInvalidSamples(t *testing.T) {
	oldHooks := log.StandardLogger().ReplaceHooks(make(log.LevelHooks))
	hook := logtest.NewGlobal()
	oldLevel := log.GetLevel()
	log.SetLevel(log.DebugLevel)
	defer func() { log.StandardLogger().ReplaceHooks(oldHooks); log.SetLevel(oldLevel) }()
	now := time.Unix(1700000000, 0)
	reporter := newVoltageReporter(1, 2, &http.Client{Timeout: time.Second, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 202, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}, "http://example.test")
	reporter.Observe(meterSample(`"bad"`, 230), now)
	require.Contains(t, hook.LastEntry().Message, "timestamp")
	reporter.Observe(meterSample("1699990000", 230), now)
	require.Contains(t, hook.LastEntry().Message, "timestamp")
	reporter.Observe(meterSample("1700000000", 0), now)
	require.Contains(t, hook.LastEntry().Message, "voltage")
	reporter.Observe(meterSample("1700000000", 230), now)
	require.Eventually(t, func() bool { reporter.mu.Lock(); defer reporter.mu.Unlock(); return !reporter.inFlight }, time.Second, time.Millisecond)
	messages := []string{}
	for _, entry := range hook.AllEntries() {
		messages = append(messages, entry.Message)
	}
	require.Contains(t, messages, "Sending Ortsnetz measurement")
	require.Contains(t, messages, "Ortsnetz measurement accepted")
}
