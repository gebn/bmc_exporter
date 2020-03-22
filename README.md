# Prometheus BMC Exporter

[![Build Status](https://travis-ci.org/gebn/bmc_exporter.svg?branch=master)](https://travis-ci.org/gebn/bmc_exporter)
[![GoDoc](https://godoc.org/github.com/gebn/bmc_exporter?status.svg)](https://godoc.org/github.com/gebn/bmc_exporter)
[![Go Report Card](https://goreportcard.com/badge/github.com/gebn/bmc_exporter)](https://goreportcard.com/report/github.com/gebn/bmc_exporter)

This project implements a [Prometheus](https://prometheus.io/docs/introduction/overview/) [exporter](https://prometheus.io/docs/instrumenting/exporters/) for Baseboard Management Controllers, or BMCs.
BMCs are embedded computers found on server-grade motherboards, used to managed and monitor the rest of the machine, even when it is in standby.
Commands can be sent to the BMC over the network via the [Intelligent Platform Management Interface](https://www.intel.com/content/www/us/en/products/docs/servers/ipmi/ipmi-home.html) (IPMI) protocol.
They are sometimes referred to by a vendor-specific name, e.g. iDRAC (Dell), iLO (HP), IMM2 (IBM) or ILOM (Oracle).

The exporter is built on top of the [`bmc`](https://github.com/gebn/bmc) library, which is a pure-Go implementation of IPMI, with no dependency on `ipmitool` or others.
Re-implementing a remote console in a safe language, with observability being a key requirement, affords several benefits.
It is heavily instrumented using the native Prometheus libraries and conventions, covering IPMI commands, sessions and raw network traffic.
Having a long running process allows caching sessions and detected capabilities for each BMC, reducing the load on them (a scrape only requires 4 packets each way on the wire).
Guarantees can also be provided around commands in-flight to a single BMC, and connection limits, both to avoid overwhelming a machine, and to be a considerate neighbour to existing software querying BMCs.
As there is no need to fork each scrape, memory churn is heavily reduced and hardware requirements for the exporter are also low.
Assuming a 30s scrape interval, 10,000 BMCs can be scraped with a single core and .5 GiB of memory - with pprof enabled!

## Usage

The exporter requires one file descriptor per BMC for the UDP socket, so you may need to increase the limit.
To see the current number of file descriptors a process may open, run `ulimit -n`.
You can maximise the number of fds the exporter may use by running `ulimit -Sn $(ulimit -Hn)`.

The exporter listens on `:9622` by default, which can be overridden with `--web.listen-address`.
A homepage is exposed at `/` with the exporter's version, and a simple form to scrape a given address.
This also doubles as a liveness and/or readiness probe endpoint.
The exporter's own metrics, including total IPMI commands sent and various latency and traffic statistics, are exposed at `/metrics`.
This can be scraped more frequently than a given BMC if required.
To scrape a BMC, request `/bmc?target=<IP[:port]>`.
Note that the target parameter is passed verbatim to the session provider.
Although the port defaults to 623, for consistency and to avoid confusion, it is recommended to be explicit and include the port after the IP address wherever it appears.

### Session Providers

Although IPMI allows a small number of commands to be sent outside a session, these cannot be used to retrieve data like sensor values, so in practice a session is needed.
To separate scrape logic from session establishment, which is more bespoke to a given manufacturer, model and organisation, the exporter uses the concept of a *session provider*.
This is an [interface](https://godoc.org/github.com/gebn/bmc_exporter/session#Provider) which returns a [`bmc.Session`](https://godoc.org/github.com/gebn/bmc#Session) struct (through which commands can be sent) for the provided target string.
The specifics of IPMI version, obtaining credentials, and supported algorithms are all hidden from the exporter behind this interface.
As the exporter does not attempt to make any modifications to a BMC, and we go out of our way to avoid *Operator* DCMI commands, it is strongly recommended for the session to be established at the *User* privilege level (effectively "read-only").
Any lower, the exporter will not be able to carry out its tasks, and any higher breaks the principle of least privilege.

The exporter defaults to a file session provider, which reads credentials from a file of the following form:

```yml
10.22.1.3:623:
  username: foo
  password: bar
10.22.1.4:623:
  username: baz
  password: bat
...
```

The location of this file defaults to `secrets.yml` in the current working directory, and can be overridden with `--session.static.secrets`.
Anonymous login and the presence of a "BMC key" (IPMI v2.0 only), while supported by the underlying library, are not currently implemented.

If you require something different, you can implement a custom provider to do practically anything, e.g. retrieve the credentials dynamically from [Vault](https://www.vaultproject.io/).
Abstractions have been created to make this easier, including one that only requires implementing the equivalent of a `Credentials(target) (username, password)` method.
See the `session` package for more details.

### Scrape Interval

A scrape interval of 30s is recommended.
The IPMI specification recommends a 60s (+/-3s) timeout for sessions on the BMC, so provided your scrape interval is below this, ceteris paribus, this will be the only session for the lifetime of the exporter process.
If deployed in a pair as recommended in the [Deployment](#Deployment) section, this will result in each exporter scraping every 60s, assuming perfect round-robin.
If `bmc_collector_session_expiries_total` increases at a constant rate, this may indicate the scrape interval is too large, but could also be caused by contended or non-conformant BMCs.

### Scrape Timeout

Scraping a healthy, available BMC from within the data centre takes a fraction of a second, however this will never be the case for all targets with any sizeable fleet.
The exporter will retry commands outside a session in an exponential back-off, but retrying inside a session has been known to cause corrupted responses (#29), so we re-establish the session after 5 seconds.
Each scrape also has a timeout within the exporter, defaulting to 8s (to allow wiggle room before the default Prometheus scrape timeout of 10s) and overridable via `--scrape.timeout`.
The idea behind this is that *some* data is better than no data.
If a BMC is excruciatingly slow, it is better to return a subset of metrics than nothing whatsoever.
This can only be done if the exporter knows to give up on the BMC before Prometheus gives up on the exporter.
The exporter always exposes a `bmc_up` metric with whether it finished the scrape successfully.
All targets in Prometheus that hit the exporter should show as `up`, regardless of the underlying machine.
If this is not the case, it suggests something wrong with the exporter or Prometheus configuration rather the BMC.

## Metrics

Below is a typical response to `/bmc` for a given target.

    # HELP bmc_info Provides the BMC's GUID, firmware, and the version of IPMI used to scrape it. Constant 1.
    # TYPE bmc_info gauge
    bmc_info{firmware="3.45.01",guid="04d298z2-8178-11e5-adf4-54ab3a0a0baa",ipmi="2.0"} 1
    # HELP bmc_scrape_duration_seconds The time taken to collect all metrics, measured by the exporter.
    # TYPE bmc_scrape_duration_seconds gauge
    bmc_scrape_duration_seconds 0.014362754
    # HELP bmc_up 1 if the exporter was able to gather all desired metrics this scrape, 0 otherwise.
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
    # HELP chassis_power_draw_watts The instantaneous amount of electricity being used by the machine.
    # TYPE chassis_power_draw_watts gauge
    chassis_power_draw_watts 259
    # HELP chassis_power_fault Whether a fault has been detected in the main power subsystem, according to Get Chassis Status.
    # TYPE chassis_power_fault gauge
    chassis_power_fault 0
    # HELP chassis_powered_on Whether the system is currently turned on, according to Get Chassis Status. If 0, the system could be in S4/S5, or mechanical off.
    # TYPE chassis_powered_on gauge
    chassis_powered_on 1

If the BMC supports [DCMI](https://www.intel.com/content/dam/www/public/us/en/documents/technical-specifications/dcmi-v1-5-rev-spec.pdf) (an extension of IPMI v2.0), and the machine's PSU supports PMBus, this exporter will expose the machine's power consumption in a `chassis_power_draw_watts` metric.
If this is not present in the scrape output, it is because the BMC does not satisfy one of these criteria.
You can use the underlying [`bmc`](https://github.com/gebn/bmc) library to investigate further.

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

## Deployment

Firing up a single instance of the exporter will work just fine for evaluation.
As the IPMI protocol operates in lock-step, similar to TFTP, a small increase in latency is multiplied by the number of packets required to perform the scrape.
It is strongly recommended to locate the exporter in the same region as the BMCs it scrapes.
Attempting to scrape across the Atlantic or further will slow down scrapes significantly, and the effects of any packet loss will be magnified.

The exporter has been designed around only having one open socket and session to a given BMC, and sending only one command at once to it.
This is important, as the specification only requires support for two concurrent sessions between the LAN and console channels, and a network buffer with capacity for two packets.
It is recommended to have a pair of exporters in each region, behind the same DNS record or K8s service, and point Prometheus at that alias.
This will result in two sessions being established with the BMC, which allows room for use by other system management software, while having N+1 resiliency.
It is fine if your targets are scraped by multiple [Prometheis](https://prometheus.io/docs/introduction/faq/#what-is-the-plural-of-prometheus).
A single exporter will serialise scrapes, and as there are only two, the BMC's packet buffer cannot be overwhelmed (other SMS notwithstanding).
However, in this case, it is recommended to increase the scrape timeout, as if all N scrapes arrive at a single exporter simultaneously for an unresponsive BMC, the last one will take N * `--scrape-timeout`.
If in doubt, set the scrape timeout to equal the scrape interval.

If you have too many BMCs to scrape with one exporter, or would like to spread the load more thinly, it is recommended to shard targets across multiple pairs of exporters, e.g. half go to one pair, half to another.
This maintains the property of having at most two sessions per BMC, and means a single exporter dying results in loss of resiliency for a smaller subset of BMCs.

On `SIGINT` or `SIGTERM`, the exporter will shut down its web server, then wait for all in-progress scrapes to finish before closing all BMC connections and sockets as cleanly as possible.
This can take some time with thousands of BMCs, especially if some are unresponsive.
There is no timeout built into the exporter; we rely on our environment killing us when it is fed up of waiting.

## Limitations

 - Sensor data besides power use via DCMI is currently unavailable. Issue [#12](https://github.com/gebn/bmc_exporter/issues/12) tracks the progress; unfortunately this requires delving into SDRs, which has so far been avoidable. It will likely take the form of `chassis_(intake|exhaust)_temperature_celsius` and `cpu_temperature_celsius{socket="#"}` metrics.
 - IPMI v1.5, the first to feature IPMI-over-LAN support, is currently unimplemented in the underlying library. Given IPMI v2.0 was first published in 2004, this is hopefully not relevant to most, however for the sake of legacy devices and completeness, it will be added after non-power sensor data is retrievable. The exporter itself is already version-agnostic.
