# Project Status

This is a fork of https://github.com/ccremer/fronius-exporter I forked it for my personal use, because the original project is looking for a maintainer which makes it harder to provide merge requests. Once the issue is resolved, I'm happingly adding my changes to the original project!

See https://github.com/ccremer/fronius-exporter/issues/134

# Fronius Exporter

[![Releases](https://img.shields.io/github/v/release/chrikoch/fronius-exporter)](https://github.com/chrikoch/fronius-exporter/releases)
[![License](https://img.shields.io/github/license/chrikoch/fronius-exporter)](https://github.com/chrikoch/fronius-exporter/blob/master/LICENSE)

## About

Scrapes a Fronius Photovoltaic power installation and converts sensor data to Prometheus metrics.
It has been tested with Fronius Symo 8.2-3-M (Software version 3.14.1-10).

![Grafana](examples/grafana.png)

## Installing

### Binary

1. Download latest binary from the [Releases page](https://github.com/chrikoch/fronius-exporter/blob/master/LICENSE)
1. `chmod +x fronius-exporter && mv fronius-exporter /usr/local/bin/`

### Package

Comes with systemd unit file.

1. Download latest package from the [Releases page](https://github.com/chrikoch/fronius-exporter/blob/master/LICENSE)
1. `sudo dpkg -i fronius-exporter_linux_amd64.deb` (Debian/Ubuntu)
1. `sudo rpm -i fronius-exporter_linux_amd64.rpm` (CentOS)
1. `sudo systemctl daemon-reload`
1. Edit the settings in `/etc/default/fronius-exporter`
1. `sudo systemctl enable fronius-exporter`
1. `sudo systemctl restart fronius-exporter`

### Docker

1. `docker run -d --name fronius-exporter -p "8080:8080" ghcr.io/chrikoch/fronius-exporter --help`

### Helm Chart

With [fronius-exporter](https://ccremer.github.io/charts/fronius-exporter)

1. `helm repo add ccremer https://ccremer.github.io/charts`
1. `helm install fronius ccremer/fronius-exporter`

## Usage

```console
fronius-exporter --symo.url http://symo.ip.or.hostname
```

Upon each call to `/metrics`, the exporter will do a GET request on the given URL, and translate the JSON response to Prometheus metrics format.

When meter real-time collection is enabled with `--symo.enable-meter-realtime`,
`fronius_site_meter_voltage_volts` reports the grid connection point smart meter's
phase-to-neutral voltages in volts, labeled `phase="1"`, `"2"`, or `"3"`.
Phases absent from the meter response are omitted.

## Configuration

`fronius-exporter` can be configured with CLI flags.
Call the binary with `--help` to get a list of options.

**Tip:** All flags are also configurable with environment variables.

* Replace the `-` char with `_` in the names and uppercase the names
* Replace the `.` delimiter with `__`
* CLI flags take precedence

Following calls are equivalent:
```
fronius-exporter --symo.url http://...
SYMO__URL=http://... fronius-exporter
```

Fronius API request mode can be configured:

* `--symo.request-mode=sequential` (default)
* `--symo.request-mode=parallel`

Environment variable equivalent:

```
SYMO__REQUEST_MODE=parallel fronius-exporter
```

## As a client API

See [Example](examples/client.go)

## Developing

See [CONTRIBUTING.md](CONTRIBUTING.md#developing) for development instructions.
