package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ccremer/fronius-exporter/pkg/fronius"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestPowerFlowMetrics(t *testing.T) {
	data := &fronius.SymoData{Inverters: map[string]fronius.Inverter{"coverage": {Power: 123, BatterySoC: 40}}}
	data.Site.PowerPhotovoltaic = 100
	data.Site.PowerGrid = -25
	data.Site.RelativeAutonomy = 75
	data.Site.RelativeSelfConsumption = 60
	data.Site.EnergyDay = 1234
	parsePowerFlowMetrics(data)
	require.Equal(t, 123.0, metricValue(t, inverterPowerGaugeVec.WithLabelValues("coverage")))
	require.Equal(t, 0.4, metricValue(t, inverterBatteryChargeGaugeVec.WithLabelValues("coverage")))
	require.Equal(t, -25.0, metricValue(t, sitePowerGridGauge))
	require.Equal(t, 0.75, metricValue(t, siteAutonomyRatioGauge))
	require.Equal(t, 0.6, metricValue(t, siteSelfConsumptionRatioGauge))
	require.Equal(t, 1234.0, metricValue(t, siteEnergyGaugeVec.WithLabelValues("day")))
	data.Site.PowerPhotovoltaic = 0
	parsePowerFlowMetrics(data)
	require.Equal(t, 1.0, metricValue(t, siteSelfConsumptionRatioGauge))
}

func TestOtherMetricMappings(t *testing.T) {
	inverter := &fronius.SymoInverterRealtimeData{DcCurrentMPPT1: fronius.RealTimeDataPoint{Value: 2.5}, DcVoltageMPPT2: fronius.RealTimeDataPoint{Value: 400}, AcPower: fronius.RealTimeDataPoint{Value: 500}}
	parseInverterRealtimeData(inverter)
	require.Equal(t, 2.5, metricValue(t, siteRealtimeDataDcCurrentMPPT1Gauge))
	require.Equal(t, 400.0, metricValue(t, siteRealtimeDataDcVoltageMPPT2Gauge))
	require.Equal(t, 500.0, metricValue(t, siteRealtimeDataAcPowerGauge))
	storage := &fronius.SymoStorageRealtimeData{}
	storage.Controller.Temperature_Cell = 31.5
	parseStorageRealtimeData(storage)
	require.Equal(t, 31.5, metricValue(t, siteStorageRealTimeDataTemperatureCell))
	archive := fronius.InverterArchive{}
	archive.Data.CurrentDCString1.Values = map[string]float64{"0": 12}
	archive.Data.VoltageDCString2.Values = map[string]float64{"0": 420}
	parseArchiveMetrics(map[string]fronius.InverterArchive{"inverter/coverage": archive})
	require.Equal(t, 12.0, metricValue(t, siteMPPTCurrentDCGaugeVec.WithLabelValues("coverage", "1")))
	require.Equal(t, 420.0, metricValue(t, siteMPPTVoltageGaugeVec.WithLabelValues("coverage", "2")))
}

func TestCollectMetricsWithFixtureServer(t *testing.T) {
	files := map[string]string{
		"/solar_api/v1/GetPowerFlowRealtimeData.fcgi": "example_1.json",
		"/solar_api/v1/GetArchiveData.cgi":            "test_archive_data.json",
		"/solar_api/v1/GetInverterRealtimeData.cgi":   "realtimedata.json",
		"/solar_api/v1/GetMeterRealtimeData.cgi":      "meterrealtimedata.json",
		"/solar_api/v1/GetStorageRealtimeData.cgi":    "storagerealtimedata.json",
	}
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		file, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile("pkg/fronius/testdata/" + file)
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	client, err := fronius.NewSymoClient(fronius.ClientOptions{URL: server.URL, PowerFlowEnabled: true, ArchiveEnabled: true, InverterRealtimeEnabled: true, MeterRealtimeEnabled: true, StorageRealtimeEnabled: true})
	require.NoError(t, err)
	before := metricValue(t, scrapeErrorCount)
	response := httptest.NewRecorder()
	metricsHandler(client).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, before, metricValue(t, scrapeErrorCount))
	for path := range files {
		require.Equal(t, 1, requests[path], path)
	}
	require.Equal(t, http.StatusOK, response.Code)
	for _, expected := range []string{"fronius_site_power_grid 611.4", "fronius_site_storage_real_time_data_temperature_cell 30.5", "fronius_site_meter_real_time_data_energy_real_wac_sum_produced 12345.67"} {
		require.True(t, strings.Contains(response.Body.String(), expected), fmt.Sprintf("missing metric %s", expected))
	}
}

func TestCollectorDisabledAndFailure(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = w.Write([]byte("invalid json")) }))
	defer server.Close()
	client, err := fronius.NewSymoClient(fronius.ClientOptions{URL: server.URL})
	require.NoError(t, err)
	before := metricValue(t, scrapeErrorCount)
	collectMetricsFromTarget(client)
	require.Zero(t, calls)
	require.Equal(t, before, metricValue(t, scrapeErrorCount))
	client.Options.PowerFlowEnabled = true
	collectMetricsFromTarget(client)
	require.Equal(t, 1, calls)
	require.Equal(t, before+1, metricValue(t, scrapeErrorCount))
}

func metricValue(t *testing.T, metric prometheus.Metric) float64 {
	t.Helper()
	var value dto.Metric
	require.NoError(t, metric.Write(&value))
	if value.Gauge != nil {
		return value.Gauge.GetValue()
	}
	return value.Counter.GetValue()
}
