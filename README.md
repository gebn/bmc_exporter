# Prometheus BMC Exporter

[![Build Status](https://travis-ci.org/gebn/bmc_exporter.svg?branch=master)](https://travis-ci.org/gebn/bmc_exporter)
[![Docker Hub](https://img.shields.io/docker/pulls/gebn/bmc_exporter.svg)](https://hub.docker.com/r/gebn/bmc_exporter)
[![GoDoc](https://godoc.org/github.com/gebn/bmc_exporter?status.svg)](https://godoc.org/github.com/gebn/bmc_exporter)
[![Go Report Card](https://goreportcard.com/badge/github.com/gebn/bmc_exporter)](https://goreportcard.com/report/github.com/gebn/bmc_exporter)

Baseboard Management Controllers (BMCs) are embedded devices found on server motherboards.
They are sometimes referred to by a vendor-specific name, e.g. iDRAC (Dell), iLO (HP), IMM2 (IBM) or ILOM (Oracle).
BMCs have access to various sensors (e.g. CPU temperature and mains power draw), properties of the system (e.g. the serial number) and fault flags (e.g. fan or PSU failure), making them a valuable source of information about a server estate.

This [exporter](https://prometheus.io/docs/instrumenting/exporters/) allows a subset of these metrics to be ingested into [Prometheus](https://prometheus.io/docs/introduction/overview/) in a normalised, safe, and efficient way, that scales to tens of thousands of machines per instance.
It uses a [native implementation](https://github.com/gebn/bmc) of [IPMI](https://www.intel.com/content/dam/www/public/us/en/documents/specification-updates/ipmi-intelligent-platform-mgt-interface-spec-2nd-gen-v2-0-spec-update.pdf), the protocol spoken by BMCs, written with observability in mind, combined with long-lived sessions, to efficiently send the required commands each scrape.
Guarantees can also be provided around commands in-flight to a single BMC and connection limits, both to avoid overwhelming a machine, and to be a considerate neighbour to existing management software.
The tool has no dependency on any of the conventional IPMI command line tools, and never leaves Go.

## Getting Started

Download the [latest release](https://github.com/gebn/bmc_exporter/releases/latest) for your platform.
Create a `secrets.yml` file mapping a sample BMC to its credentials in the following way:

```yaml
10.0.0.1:623:                # target configured in Prometheus
  username: <username>       # must have USER IPMI privileges
  password: <password>
```

Start the exporter with `./bmc_exporter [--secrets.static secrets.yml]`.
Navigate to [http://localhost:9622](http://localhost:9622), and copy the target on the first line of your YAML file into the *Target* text field (e.g. `10.0.0.1:623`), then click *Scrape*.
You will be directed to `/bmc?target=<your target>`, hopefully resembling the following:

    # HELP bmc_info Provides the BMC's GUID, firmware, and the version of IPMI used to scrape it. Constant 1.
    # TYPE bmc_info gauge
    bmc_info{firmware="3.45.01",guid="04d298z2-8178-11e5-adf4-54ab3a0a0baa",ipmi="2.0"} 1
    # HELP bmc_scrape_duration_seconds The time taken to collect all metrics, measured by the exporter.
    # TYPE bmc_scrape_duration_seconds gauge
    bmc_scrape_duration_seconds 0.014362754
    # HELP bmc_up 1 if the exporter was able to establish a session, 0 otherwise.
    # TYPE bmc_up gauge
    bmc_up 1
    # HELP chassis_cooling_fault Whether a cooling or fan fault has been detected, according to Get Chassis Status.
    # TYPE chassis_cooling_fault gauge
    chassis_cooling_fault 0
    # HELP chassis_drive_fault Whether a disk drive in the system is faulty, according to Get Chassis Status.
    # TYPE chassis_drive_fault gauge
    chassis_drive_fault 0
    # HELP chassis_intrusion Whether the system cover is open, according to Get Chassis Status.
    # TYPE chassis_intrusion gauge
    chassis_intrusion 0
    # HELP chassis_power_fault Whether a fault has been detected in the main power subsystem, according to Get Chassis Status.
    # TYPE chassis_power_fault gauge
    chassis_power_fault 0
    # HELP chassis_powered_on Whether the system is currently turned on, according to Get Chassis Status. If 0, the system could be in S4/S5, or mechanical off.
    # TYPE chassis_powered_on gauge
    chassis_powered_on 1
    # HELP power_draw_watts The instantaneous amount of electricity being used by the machine, broken down by PSU where possible.
    # TYPE power_draw_watts gauge
    power_draw_watts{psu="1"} 70
    power_draw_watts{psu="2"} 105
    # HELP processor_temperature_celsius The temperature of each CPU in degrees celsius.
    # TYPE processor_temperature_celsius gauge
    processor_temperature_celsius{cpu="1"} 42
    processor_temperature_celsius{cpu="2"} 44

If `bmc_up` has a value of `0`, double check the BMC is reachable from your machine and the credentials are correct using `ipmitool`:

    ipmitool -I lanplus -H <ip> -U <username> -P <password> -L USER sdr

If `bmc_up` is `1`, congrats, you've scraped your first BMC!
The Prometheus `scrape_config` for this job would look like this:

```yaml
- job_name: bmc
  scrape_interval: 30s                  # a 30s scrape interval is recommended
  metrics_path: /bmc                    # the exporter exposes its own metrics at /metrics
  static_configs:
  - targets:
    - 10.0.0.1:623                      # the same string used as the key in secrets.yml
  relabel_configs:
  - source_labels: [__address__]
    target_label: __param_target
  - source_labels: [__param_target]
    target_label: instance
  - target_label: __address__
    replacement: localhost:9622         # the location of the exporter to Prometheus
```

## Metrics

The exporter may return the following metrics in response to a request to `/bmc`.
To prevent Prometheus timing out and throwing everything away, the exporter may return a subset if it does not have time to gather everything.

| Metric | Description |
|-|-|
| `bmc_up` | A boolean indicating whether the BMC is healthy. This means a session could be established, the exporter could retrieve the entire SDR repository, and subcollectors had time to do their initialisation. If this is `0`, it is likely to be on the first scrape, as subsequent scrapes reuse the session. |
| `bmc_scrape_duration_seconds` | This effectively a stopwatch on the `Collect()` method in the exporter. It may differ widely from Prometheus, as the exporter serialises collections for each BMC (some BMCs appear to use a single buffer for all requests, so scraping them simultaneously causes corrupted responses). The time a request spends waiting for the target's event loop to pick it up is not included in this value, however it is tracked by the `bmc_target_scrape_dispatch_latency_seconds` histogram. |
| `bmc_info` |  A constant `1`, providing the `firmware` version and `guid` of the BMC in labels, along with the `version` of IPMI being used by the exporter to interact with it (which will currently always be `2.0`). Each BMC vendor includes different supplementary version information, which is used to create the version string on a best-effort basis. The GUID label uses the original byte order; this can be in any format, and any byte order, so cannot be interpreted reliably without additional knowledge. Treating the original bytes as a GUID seems to work fairly well. On Dell this matches the smbiosGUID field in the iDRAC UI, and on Quanta it produces a valid version 1 GUID. These values are all obtained from the `Get Device ID` and `Get System GUID` commands. |
| `chassis_powered_on` | A boolean indicating whether the system power is on. If `0`, it could be in S4/S5, or mechanical off. This value is returned in the `Get Chassis Status` command. |
| `chassis_cooling_fault` | A boolean indicating whether a cooling or fan fault has been detected. Obtained via `Get Chassis Status`. |
| `chassis_drive_fault` | A boolean indicating whether a disk drive in the system is faulty. Obtained via `Get Chassis Status`. |
| `chassis_power_fault` | A boolean indicating whether a fault has been detected in the main power subsystem. Obtained via `Get Chassis Status`. |
| `chassis_intrusion` | A boolean indicating whether the chassis is currently open. Retrieved via `Get Chassis Status`. |
| `power_draw_watts` | One gauge for each wattage sensor instance under the *power supply* SDR entity, in which case the `psu` label is the instance ID, so may not be 0-based or continuous (treat these as opaque strings). If power usage isn't available in the SDR, this will fall back to issuing a `Get Power Reading` DCMI command, which returns a label-less aggregate draw for the entire machine. The power supplies must support PMBus for either mechanism to work. Values could theoretically have a fractional component, however all values observed have been integers. |
| `processor_temperature_celsius` | One gauge for each temperature sensor under the *processor* SDR entity. This usually corresponds to one sensor per die rather than per core. We prefer sensors with the IPMI entity ID (`0x3`), falling back to the deprecated DCMI variant (`0x41`). We never combine sensors from both in order to avoid duplication. Only sensors with a unit of celsius are currently considered. Values could theoretically have a fractional component, however all values observed have been integers. |

### Interesting Queries

Number of machines by BMC firmware:

    sum by (firmware) (bmc_info)

Number of machines with a cooling fault:

    sum(chassis_cooling_fault == bool 1)

Ratio of machines currently powered on:

    sum(chassis_powered_on == bool 1) / count(chassis_powered_on)

It is strongly recommended to set appropriate target labels for the manufacturer, model and location of each machine.
This allows more interesting aggregations, e.g. viewing the different firmware versions installed for a single model, or power usage by data centre field.
By `count()`ing the `*_fault` metrics, you could also see which model is proving most troublesome overall, and eventually trends of all of the above over time.

## Next Steps

The exporter listens on `:9622` by default, which can be overridden with `--web.listen-address`.
A homepage is exposed at `/` with the exporter's version, and a simple form to scrape a given address.
This also doubles as a liveness and/or readiness probe endpoint.
The exporter's own metrics, including total IPMI commands sent and various latency and traffic statistics, are exposed at `/metrics`.
This can be scraped more frequently than a given BMC if required.
To scrape a BMC, request `/bmc?target=<IP[:port]>`.
Note that the target parameter is passed verbatim to the session provider.
Although the port defaults to 623, for consistency and to avoid confusion, it is recommended to be explicit and include the port after the IP address wherever it appears.

### Ulimit

The exporter requires one file descriptor per BMC for the UDP socket, so you may need to increase the limit.
To see the current number of file descriptors a process may open, run `ulimit -n`.
You can maximise the number of fds the exporter may use by running `ulimit -Sn $(ulimit -Hn)`.

### Scrape Interval

A scrape interval of 30s is recommended.
The IPMI specification recommends a 60s (+/-3s) timeout for sessions on the BMC, so provided your scrape interval is below this, this should be the only session for the lifetime of the exporter process.
If deployed in a pair as recommended in the [Deployment](#Deployment) section, this will result in each exporter scraping every 60s, assuming perfect round-robin.

### Scrape Timeout

Scraping a healthy, available BMC from within the data centre takes a fraction of a second, however this will never be the case for all targets with any sizeable fleet.
The exporter will retry commands outside a session in an exponential back-off, but retrying inside a session has been known to cause corrupted responses (#29), so we re-establish the session after 5 seconds.
Each scrape also has a timeout within the exporter, defaulting to 8s (to allow wiggle room before the default Prometheus scrape timeout of 10s) and overridable via `--scrape.timeout`.
The idea behind this is that *some* data is better than no data.
If a BMC is excruciatingly slow, it is better to return a subset of metrics than nothing whatsoever.
This can only be done if the exporter knows to give up on the BMC before Prometheus gives up on the exporter.
All targets in Prometheus that hit the exporter should show as `UP`, regardless of the underlying machine.
If this is not the case, it suggests something wrong with the exporter or Prometheus configuration rather the BMC.

## Deployment

Firing up a single instance of the exporter will work just fine for evaluation.
As the IPMI protocol operates in lock-step, similar to TFTP, a small increase in latency is multiplied by the number of packets required to perform the scrape.
It is strongly recommended to locate the exporter in the same region as the BMCs it scrapes.
Attempting to scrape across the Atlantic or further will slow down scrapes significantly, and the effects of any packet loss will be magnified.

The exporter has been designed around only having one open socket and session to a given BMC, and sending only one command at once to it.
This is important, as the specification only requires support for two concurrent sessions between the LAN and console channels, and a network buffer with capacity for two packets.
It is recommended to have a pair of exporters in each region, behind the same DNS record or K8s service, and point Prometheus at that alias.
This will result in two sessions being established with the BMC, which allows room for use by other system management software, while having N+1 resiliency.
A single exporter will serialise multiple scrape requests for a given BMC arriving at the same time, so it is fine to point multiple Prometheis at it.
However, in this case, it is recommended to increase the scrape timeout, as if all N scrapes arrive simultaneously for an unresponsive BMC, the last one will take `N * --scrape.timeout` seconds.
If in doubt, set the scrape timeout to equal the scrape interval.

If you have too many BMCs to scrape with one exporter, or would like to spread the load more thinly, it is recommended to shard targets across multiple pairs of exporters, e.g. half go to one pair, half to another.
This maintains the property of having at most two sessions per BMC, and means a single exporter dying results in loss of resiliency for a smaller subset of BMCs.

On `SIGINT` or `SIGTERM`, the exporter will shut down its web server, then wait for all in-progress scrapes to finish before closing all BMC connections and sockets as cleanly as possible.
This can take some time with thousands of BMCs, especially if some are unresponsive.
There is no timeout built into the exporter; it relies on the environment to kill it when it is fed up waiting.

### Alerts

In addition to alerting on BMC metrics, you may want to be notified of an unhealthy exporter. Both the exporter and its underlying `bmc` library were written with Prometheus in mind, so you have lots of metrics, from collection latency to to number of each IPMI command attempted. This section is intended to provide a guide for what to look at. Exact alert thresholds will depend on the number of targets in the estate and how the exporter is deployed.

#### BMC

Alerts for metrics exposed at `/bmc`. The key one here is `bmc_up`, however you may want to alert on others (e.g. `chassis_cooling_fault`) depending on your environment. Be wary of using IPMI to alert on `chassis_power_fault`, as any significant problem will cause the BMC to also lose power! It is better to use `bmc_up` as an indication of an unhealthy machine.

| Metric | Description |
|-|-|
| `bmc_up` | Every BMC where this is `0` is a monitoring gap, as no other metrics (besides `bmc_scrape_duration_seconds`) are exposed. It can be caused by incorrect credentials (check `bmc_session_open_failures_total`), or running out of time while initialising after the session is established, typically because of high latency (see `bmc_collector_initialise_timeouts_total` below). |

#### Exporter

These metrics are exposed at `/metrics`, so are an overall view of all scrapes going through the exporter and do not concern any single BMC. They can indicate a configuration error in Prometheus or the exporter, and general network issues. Note that [pprof](https://golang.org/pkg/runtime/pprof/) is enabled on the exporter at the default paths, so profiles can be retrieved at any time.

| Metric | Description |
|-|-|
| `bmc_collector_initialise_timeouts_total` | If this increases too rapidly, it suggests BMCs have too high latency to complete initialisation before Prometheus times out the scrape. This causes a kind of crash looping behaviour where the BMC never manages to be ready for scraping. The solution is to increase the scrape timeout, or move the exporter closer to the BMC. |
| `bmc_collector_session_expiries_total` | The specification recommends a timeout of 60s +/- 3s, so if you have deployed the exporter in a pair and scrape every 30s, a high rate of increase indicates a load balancing issue. When the session expires, the exporter will attempt to establish a new one, so this is not a problem in itself; it just results in a few more requests and higher load on BMCs. If your scrape interval is 2m, you would expect every scrape to require a new session. |
| `bmc_provider_credential_failures_total` | Any increase here indicates the credential provider is struggling to fulfil requests, and BMCs cannot be logged into. The only bundled implementation is the file provider, so these errors will not be temporary, and indicates the exporter is being asked to scrape a set of BMCs that has drifted from its secrets config file. |
| `bmc_target_abandoned_requests_total` | A high rate of abandoned requests indicates contention for access to BMCs. This is most likely to be caused by multiple Prometheis scraping a single exporter with a short scrape timeout. These requests did not have time to begin a collection, let alone initialise a session. |
| `process_open_fds` | The exporter requires one file descriptor per BMC, plus 15-20% depending on the scrape interval. You'll want to alert if `process_open_fds / process_max_fds` approaches `1`. |

## Limitations

 - Only power draw and processor temperature sensor data is currently available. Other sensors are far less standardised, so normalising them in the exporter's output - a key feature - is much harder. Next up is `chassis_(intake|exhaust)_temperature_celsius`.
 - IPMI v1.5, the first to feature IPMI-over-LAN support, is currently unimplemented in the underlying library. Given IPMI v2.0 was first published in 2004, this is hopefully not relevant to most, however for the sake of legacy devices and completeness, it will be added after non-power sensor data is retrievable. The exporter itself is already version-agnostic.
