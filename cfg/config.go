package cfg

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/koanf/v2"

	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/posflag"
	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
)

// ParseConfig overrides internal config defaults with up CLI flags, environment variables and ensures basic validation.
func ParseConfig(version, commit, date string, fs *flag.FlagSet, args []string) *Configuration {
	config := NewDefaultConfig()

	setupCliFlags(fmt.Sprintf("version %s, %s, %s", version, commit, date), fs, config)

	loadConfigHierarchy(fs, args, config)

	postLoadProcess(config)

	log.WithField("config", *config).Debug("Parsed config")
	return config
}

func setupCliFlags(version string, fs *flag.FlagSet, config *Configuration) {
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s (%s):\n", os.Args[0], version)
		fs.PrintDefaults()
	}
	fs.String("bind-addr", config.BindAddr, "IP Address to bind to listen for Prometheus scrapes.")
	fs.String("ortsnetz.location", config.Ortsnetz.Location, "Enable Ortsnetz reporting with latitude,longitude.")
	fs.String("log.level", config.Log.Level, "Logging level.")
	fs.BoolP("log.verbose", "v", config.Log.Verbose, "Shortcut for --log.level=debug.")
	fs.StringSlice("symo.header", config.Symo.Headers,
		"List of \"key: value\" headers to append to the requests going to Fronius Symo. Example: --symo.header \"authorization=Basic <base64>\".")
	fs.StringP("symo.url", "u", config.Symo.URL, "Target base URL of Fronius Symo device.")
	fs.Int64("symo.timeout", int64(config.Symo.Timeout.Seconds()),
		"Timeout in seconds when collecting metrics from Fronius Symo. Should not be larger than the scrape interval.")
	fs.String("symo.request-mode", config.Symo.RequestMode, "Request mode for Fronius API calls. Supported values: sequential, parallel.")
	fs.Bool("symo.enable-power-flow", config.Symo.PowerFlowEnabled, "Enable/disable scraping of power flow data")
	fs.Bool("symo.enable-archive", config.Symo.ArchiveEnabled, "Enable/disable scraping of archive data")
	fs.Bool("symo.enable-inverter-realtime", config.Symo.InverterRealtimeEnabled, "Enable/disable scraping of inverter real time data")
	fs.Bool("symo.enable-meter-realtime", config.Symo.MeterRealtimeEnabled, "Enable/disable scraping of meter real time data")
	fs.Bool("symo.enable-storage-realtime", config.Symo.StorageRealtimeEnabled, "Enable/disable scraping of storage real time data")
}

// ParseOrtsnetzLocation validates the optional reporting location.
func ParseOrtsnetzLocation(location string, meterEnabled bool) (float64, float64, bool, error) {
	if strings.TrimSpace(location) == "" {
		return 0, 0, false, nil
	}
	if !meterEnabled {
		return 0, 0, true, fmt.Errorf("ortsnetz.location requires symo.enable-meter-realtime")
	}
	parts := strings.Split(location, ",")
	if len(parts) != 2 {
		return 0, 0, true, fmt.Errorf("ortsnetz.location must be latitude,longitude")
	}
	lat, errLat := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, errLon := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if errLat != nil || errLon != nil || math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) || math.Abs(lat) > 90 || math.Abs(lon) > 180 {
		return 0, 0, true, fmt.Errorf("invalid ortsnetz.location: expected valid latitude,longitude")
	}
	return lat, lon, true, nil
}

func postLoadProcess(config *Configuration) {
	config.Symo.Timeout *= time.Second
	if config.Log.Verbose {
		config.Log.Level = "debug"
	}

	var parsedHeaders []string
	for _, header := range config.Symo.Headers {
		parsedHeaders = splitHeaderStrings(header, parsedHeaders)
	}
	config.Symo.Headers = parsedHeaders
	config.Symo.RequestMode = strings.ToLower(strings.TrimSpace(config.Symo.RequestMode))
	if config.Symo.RequestMode != "sequential" && config.Symo.RequestMode != "parallel" {
		log.WithField("symo.request-mode", config.Symo.RequestMode).Warn("Unsupported request mode, falling back to sequential")
		config.Symo.RequestMode = "sequential"
	}

	level, err := log.ParseLevel(config.Log.Level)
	if err != nil {
		log.WithError(err).Warn("Could not parse log level, fallback to info level")
		config.Log.Level = "info"
		log.SetLevel(log.InfoLevel)
	} else {
		log.SetLevel(level)
	}
}

func splitHeaderStrings(rest string, headers []string) []string {
	s := strings.TrimPrefix(rest, ",")
	arr := strings.SplitN(s, ",", 2)
	if v := arr[0]; v != "" {
		headers = append(headers, strings.TrimSpace(v))
	}
	if len(arr) < 2 {
		// No more key-value pairs to parse
		return headers
	}
	return splitHeaderStrings(arr[1], headers)
}

func loadConfigHierarchy(fs *flag.FlagSet, args []string, config *Configuration) {
	koanfInstance := koanf.New(".")

	// Environment variables
	if err := koanfInstance.Load(env.Provider(".", env.Opt{TransformFunc: func(k, v string) (string, any) {
		/*
			Configuration can contain hierarchies (YAML, etc.) and CLI flags dashes.
			To read environment variables with hierarchies and dashes we replace the hierarchy delimiter with double underscore and dashes with single underscore.
			So that parent.child-with-dash becomes PARENT__CHILD_WITH_DASH
		*/
		k = strings.Replace(strings.ToLower(k), "__", ".", -1)
		k = strings.Replace(strings.ToLower(k), "_", "-", -1)
		return k, v
	}}), nil); err != nil {
		log.WithError(err).Fatal("Could not parse flags")
	}

	// CLI Flags
	if err := fs.Parse(args); err != nil {
		log.WithError(err).Fatal("Could not parse flags")
	}
	if err := koanfInstance.Load(posflag.Provider(fs, ".", koanfInstance), nil); err != nil {
		log.WithError(err).Fatal("Could not process flags")
	}

	if err := koanfInstance.Unmarshal("", &config); err != nil {
		log.WithError(err).Fatal("Could not merge defaults with settings from environment variables")
	}
}

// ConvertHeaders takes a list of `key=value` headers and adds those trimmed to the specified header struct. It ignores
// any malformed entries.
func ConvertHeaders(headers []string, header *http.Header) {
	for _, hd := range headers {
		arr := strings.SplitN(hd, "=", 2)
		if len(arr) < 2 {
			log.WithFields(log.Fields{
				"arg":   hd,
				"error": "cannot split: missing equal sign",
			}).Warn("Could not parse header, ignoring")
			continue
		}
		key := strings.TrimSpace(arr[0])
		value := strings.TrimSpace(arr[1])
		log.WithFields(log.Fields{
			"key":   key,
			"value": value,
		}).Debug("Using header")
		header.Set(key, value)
	}
}
