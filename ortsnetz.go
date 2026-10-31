package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/ccremer/fronius-exporter/pkg/fronius"
	log "github.com/sirupsen/logrus"
)

type voltageSample struct {
	observedAt time.Time
	voltages   [3]float64
}

func sampleVoltage(data *fronius.SymoMeterRealtimeData, now time.Time) (voltageSample, bool) {
	var timestamp int64
	if data == nil || json.Unmarshal(data.TimeStamp, &timestamp) != nil || timestamp <= 0 {
		log.Debug("Skipping Ortsnetz measurement: missing or invalid meter timestamp")
		return voltageSample{}, false
	}
	observed := time.Unix(timestamp, 0).UTC()
	delta := now.Sub(observed)
	if delta > 600*time.Second || delta < -600*time.Second {
		log.WithField("observed_at", observed).Debug("Skipping Ortsnetz measurement: meter timestamp outside allowed range")
		return voltageSample{}, false
	}
	s := voltageSample{observedAt: observed, voltages: [3]float64{-1, -1, -1}}
	valid := false
	for i, v := range []*float64{data.VoltageACPhase1, data.VoltageACPhase2, data.VoltageACPhase3} {
		if v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v > 0 && *v <= 500 {
			s.voltages[i] = *v
			valid = true
		}
	}
	if !valid {
		log.Debug("Skipping Ortsnetz measurement: no valid phase voltage")
	}
	return s, valid
}

type voltageStats struct {
	min, max, sum float64
	count         int
}

func (s voltageStats) average() float64 {
	return math.Max(s.min, math.Min(s.max, s.sum/float64(s.count)))
}

type voltageAccumulator struct{ phases [3]voltageStats }

func (a *voltageAccumulator) add(values [3]float64) {
	for i, v := range values {
		if v == -1 {
			continue
		}
		s := &a.phases[i]
		if s.count == 0 || v < s.min {
			s.min = v
		}
		if s.count == 0 || v > s.max {
			s.max = v
		}
		s.sum += v
		s.count++
	}
}
func (a *voltageAccumulator) snapshot() [3]voltageStats { return a.phases }
func (a *voltageAccumulator) reset()                    { a.phases = [3]voltageStats{} }

const ortsnetzInterval = 300 * time.Second
const ortsnetzURL = "https://www.ortsnetz-auslastung.de/v1/measurements"

type voltageReporter struct {
	mu                  sync.Mutex
	latitude, longitude float64
	client              *http.Client
	endpoint            string
	lastAttempt         time.Time
	inFlight            bool
	samples             voltageAccumulator
}

func newVoltageReporter(lat, lon float64, client *http.Client, endpoint string) *voltageReporter {
	return &voltageReporter{latitude: lat, longitude: lon, client: client, endpoint: endpoint}
}

func (r *voltageReporter) Observe(data *fronius.SymoMeterRealtimeData, now time.Time) {
	sample, ok := sampleVoltage(data, now)
	if !ok {
		return
	}
	r.mu.Lock()
	first := r.lastAttempt.IsZero()
	if !first {
		r.samples.add(sample.voltages)
	}
	if r.inFlight || (!first && now.Sub(r.lastAttempt) < ortsnetzInterval) {
		r.mu.Unlock()
		return
	}
	payload := map[string]any{"observed_at": sample.observedAt.Format(time.RFC3339), "latitude": r.latitude, "longitude": r.longitude, "l1_v": sample.voltages[0], "l2_v": sample.voltages[1], "l3_v": sample.voltages[2]}
	if !first {
		for i, stat := range r.samples.snapshot() {
			if stat.count > 0 {
				prefix := fmt.Sprintf("l%d_v_", i+1)
				payload[prefix+"min"] = stat.min
				payload[prefix+"max"] = stat.max
				payload[prefix+"avg"] = stat.average()
			}
		}
	}
	r.samples.reset()
	r.lastAttempt = now
	r.inFlight = true
	r.mu.Unlock()
	go r.send(payload)
}

func (r *voltageReporter) send(payload map[string]any) {
	defer func() { r.mu.Lock(); r.inFlight = false; r.mu.Unlock() }()
	body, err := json.Marshal(payload)
	if err != nil {
		log.WithError(err).Warn("Could not encode Ortsnetz measurement")
		return
	}
	req, err := http.NewRequest(http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		log.WithError(err).Warn("Could not create Ortsnetz request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	fields := log.Fields{"observed_at": payload["observed_at"], "l1_v": payload["l1_v"], "l2_v": payload["l2_v"], "l3_v": payload["l3_v"]}
	for i := 1; i <= 3; i++ {
		for _, stat := range []string{"min", "max", "avg"} {
			key := fmt.Sprintf("l%d_v_%s", i, stat)
			if value, ok := payload[key]; ok {
				fields[key] = value
			}
		}
	}
	log.WithFields(fields).Debug("Sending Ortsnetz measurement")
	response, err := r.client.Do(req)
	if err != nil {
		log.WithError(err).Warn("Could not send Ortsnetz measurement")
		return
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		log.WithError(err).Warn("Could not read Ortsnetz response")
	}
	result := log.WithFields(log.Fields{"status": response.StatusCode, "response": string(responseBody)})
	if response.StatusCode != http.StatusAccepted {
		result.Warn("Ortsnetz measurement was rejected")
	} else {
		result.Debug("Ortsnetz measurement accepted")
	}
}
