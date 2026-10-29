package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ccremer/fronius-exporter/pkg/fronius"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMeterVoltageMetrics(t *testing.T) {
	body := `{"Body":{"Data":{"0":{"Meter_Location_Current":0,"Voltage_AC_Phase_1":232.1,"Voltage_AC_Phase_2":229.3,"Voltage_AC_Phase_3":234.9}}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer server.Close()
	client, err := fronius.NewSymoClient(fronius.ClientOptions{URL: server.URL, MeterRealtimeEnabled: true})
	require.NoError(t, err)
	collectMeterRealtimeData(client)
	checkMeterVoltages(t, map[string]float64{"1": 232.1, "2": 229.3, "3": 234.9})
	body = `{"Body":{"Data":{"0":{"Meter_Location_Current":0,"Voltage_AC_Phase_1":0}}}}`
	collectMeterRealtimeData(client)
	checkMeterVoltages(t, map[string]float64{"1": 0})
}

func checkMeterVoltages(t *testing.T, want map[string]float64) {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	got := map[string]float64{}
	for _, family := range families {
		if family.GetName() == "fronius_site_meter_voltage_volts" {
			for _, metric := range family.Metric {
				got[metric.Label[0].GetValue()] = metric.Gauge.GetValue()
			}
		}
	}
	assert.Equal(t, want, got)
}

func TestRunCollectors_WhenSequential_ThenRunsInOrderWithoutOverlap(t *testing.T) {
	client := &fronius.SymoClient{}

	var orderMu sync.Mutex
	order := []int{}
	var current int64
	var maxConcurrent int64

	collectors := []collectorFunc{
		func(*fronius.SymoClient) {
			recordCollector(1, &orderMu, &order, &current, &maxConcurrent)
		},
		func(*fronius.SymoClient) {
			recordCollector(2, &orderMu, &order, &current, &maxConcurrent)
		},
		func(*fronius.SymoClient) {
			recordCollector(3, &orderMu, &order, &current, &maxConcurrent)
		},
	}

	runCollectors(client, "sequential", collectors)

	assert.Equal(t, []int{1, 2, 3}, order)
	assert.Equal(t, int64(1), maxConcurrent)
}

func TestRunCollectors_WhenParallel_ThenRunsWithOverlap(t *testing.T) {
	client := &fronius.SymoClient{}

	var ran int64
	var current int64
	var maxConcurrent int64

	collectors := []collectorFunc{
		func(*fronius.SymoClient) {
			parallelCollector(&ran, &current, &maxConcurrent)
		},
		func(*fronius.SymoClient) {
			parallelCollector(&ran, &current, &maxConcurrent)
		},
		func(*fronius.SymoClient) {
			parallelCollector(&ran, &current, &maxConcurrent)
		},
	}

	runCollectors(client, "parallel", collectors)

	assert.Equal(t, int64(3), ran)
	assert.Greater(t, maxConcurrent, int64(1))
}

func recordCollector(id int, orderMu *sync.Mutex, order *[]int, current *int64, maxConcurrent *int64) {
	c := atomic.AddInt64(current, 1)
	updateMax(maxConcurrent, c)
	orderMu.Lock()
	*order = append(*order, id)
	orderMu.Unlock()
	time.Sleep(25 * time.Millisecond)
	atomic.AddInt64(current, -1)
}

func parallelCollector(ran *int64, current *int64, maxConcurrent *int64) {
	c := atomic.AddInt64(current, 1)
	updateMax(maxConcurrent, c)
	time.Sleep(75 * time.Millisecond)
	atomic.AddInt64(current, -1)
	atomic.AddInt64(ran, 1)
}

func updateMax(maxConcurrent *int64, current int64) {
	for {
		existing := atomic.LoadInt64(maxConcurrent)
		if current <= existing {
			return
		}
		if atomic.CompareAndSwapInt64(maxConcurrent, existing, current) {
			return
		}
	}
}
