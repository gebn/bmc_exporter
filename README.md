# Prometheus BMC Exporter

[![Build Status](https://travis-ci.org/gebn/bmc_exporter.svg?branch=master)](https://travis-ci.org/gebn/bmc_exporter)
[![GoDoc](https://godoc.org/github.com/gebn/bmc_exporter?status.svg)](https://godoc.org/github.com/gebn/bmc_exporter)
[![Go Report Card](https://goreportcard.com/badge/github.com/gebn/bmc_exporter)](https://goreportcard.com/report/github.com/gebn/bmc_exporter)

This project implements a [Prometheus](https://prometheus.io/docs/introduction/overview/) [exporter](https://prometheus.io/docs/instrumenting/exporters/) for Baseboard Management Controllers, or BMCs.
BMCs are dedicated hardware found on server-grade motherboards, available even when the machine is in standby, whose metrics can be retrieved remotely over the network via the [Intelligent Platform Management Interface](https://www.intel.com/content/www/us/en/products/docs/servers/ipmi/ipmi-home.html) (IPMI) protocol.
They are sometimes referred to by a vendor-specific name, e.g. iDRAC (Dell), iLO (HP) or ILOM (Oracle).

The exporter is built on top of the [`bmc`](https://github.com/gebn/bmc) library, which is a pure-Go implementation of IPMI, with no dependency on `ipmitool` and others.
Re-implementing a remote console in a safe language, with observability being a key requirement, affords the exporter several benefits.
It is heavily instrumented using the native Prometheus libraries and conventions, covering IPMI commands, sessions and raw network traffic.
Having a long running process allows caching sessions and detected capabilities for each BMC, reducing the load on them (a scrape only requires 4 packets each way on the wire).
Guarantees can also be provided around commands in-flight to a single BMC, and connection limits, both to avoid overwhelming a machine, and to be a considerate neighbour to existing software querying BMCs.
As there is no need to fork each scrape, memory churn is heavily reduced and hardware requirements for the exporter are also low.
Assuming a 30s scrape interval, 10,000 BMCs can be scraped with a single core and .5 GiB of memory - with pprof enabled!

## Usage

The exporter requires one file descriptor per target for the UDP socket, so you may need to increase the limit.
To see the current number of file descriptors a process may open, run `ulimit -n`.
You can maximise the number of fds the exporter may use by running `ulimit -Sn $(ulimit -Hn)`.

The exporter listens on `:9622` by default, this can be overridden with `--web.listen-address`.
A homepage is exposed at `/` with the exporter's version, and a simple form to scrape a BMC.
This also doubles as a health check endpoint.
The exporter's own metrics, including total IPMI commands sent and various latency and traffic statistics, are exposed at `/metrics`.
This can be scraped more frequently than a given BMC if required.
To scrape a BMC, request `/bmc?target=<IP[:port]>`.
Note that the target parameter is passed verbatim to the session provider.
Although the port defaults to 623, to avoid confusion, is is recommended to be explicit and include the port after the IP address wherever it appears.

### Session Providers

Although IPMI allows a small number of commands to be sent outside a session, these cannot be used to retrieve data like sensor values, so in practice a session is needed.
To separate scrape logic from session establishment, which is more bespoke, the exporter uses the concept of a *session provider*.
This is an [interface](https://godoc.org/github.com/gebn/bmc_exporter/session#Provider) which returns a [`bmc.Session`](https://godoc.org/github.com/gebn/bmc#Session) struct (through which commands can be sent) for the provided target string.
The specifics of IPMI version, obtaining credentials, and supported algorithms are all hidden from the exporter behind this interface.
As the exporter does not attempt to make any modifications to a BMC, it is strongly recommended for the session to be established at the *User* privilege level (effectively "read-only").
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

If this is not workable for your requirements, you can implement a custom provider to do practically anything.
Abstractions have been created to make this easier, including one that only requires implementing the equivalent of a `Credentials(target) (username, password)` method.
See the `session` package for more details.

### Scrape Interval

A scrape interval of 30s is recommended.
The IPMI specification recommends a 60s (+/-3s) timeout on sessions, so provided your scrape interval is below this, ceteris paribus, this will be the only session for the lifetime of the exporter process.
If deployed in a pair as recommended in the [Deployment](#Deployment) section, this will result in a scrape every 60s to each exporter assuming perfect round-robin.
If `bmc_collector_session_expiries_total` increases at any kind of constant rate, it is an indication that the scrape interval is too large.

## Metrics

Below is a typical response to `/bmc`.

    # HELP bmc_info Provides the BMC's GUID, firmware, and the version of IPMI used to scrape it. Constant 1.
    # TYPE bmc_info gauge
    bmc_info{firmware="3.45.01",guid="04d298z2-8178-11e5-adf4-54ab3a0a0baa",ipmi="2.0"} 1
    # HELP bmc_last_scrape When the BMC was last scraped by this exporter, expressed as seconds since the Unix epoch. This metric will not be present in the first scrape of a given BMC, including after GC.
    # TYPE bmc_last_scrape gauge
    bmc_last_scrape 1.56674752208897e+09
    # HELP bmc_scrape_duration_seconds The time taken to collect all metrics, measured by the exporter.
    # TYPE bmc_scrape_duration_seconds gauge
    bmc_scrape_duration_seconds 0.018077478
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
    chassis_power_draw_watts 245
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

    count(chassis_cooling_fault == 1)

Ratio of machines currently powered on:

    count(chassis_powered_on == 1) / count(chassis_powered_on)

It is strongly recommended to set appropriate target labels for the manufacturer, model and location of each machine.
This allows more interesting aggregations, e.g. viewing the different firmware versions installed for a single model, or power usage by data centre field.
By `count()`ing the `*_fault` metrics, you could also see which model is proving most troublesome overall, and eventually trends of all of the above over time.

## Deployment

Firing up a single instance of the exporter will work just fine for evaluation.
As the IPMI protocol operates in lock-step, similar to TFTP, a small increase in latency is multiplied by the number of packets required to perform the scrape.
It is strongly recommended to locate the exporter in the same region as the BMCs it scrapes.
Attempting to scrape across the Atlantic or further will slow down scrapes significantly, and the effects of any packet loss will be magnified.

The exporter has been designed around only having one open socket, session, and sending only one command at once to a given BMC.
This is important, as the specification only requires support for 4 concurrent sessions between the LAN and console channels, and a packet buffer of depth 2.
It is recommended to have a pair of exporters in each region, behind the same DNS record or K8s service, and point Prometheus at that alias.
This will result in 2 sessions being established with the BMC, which allows room for use by other system management software, while having N+1 resiliency.
It is fine if your targets are scraped by multiple [Prometheis](https://prometheus.io/docs/introduction/faq/#what-is-the-plural-of-prometheus).
A single exporter will serialise scrapes, and as there are only 2, the BMC's packet buffer cannot be overwhelmed (other SMS notwithstanding).

If you have more BMCs than you are willing to scale vertically, it is recommended to shard them across multiple multiple pairs, e.g. half go to one pair, half to another.
This maintains the max 2 sessions per BMC property, and means a single exporter dying results in loss of resiliency for a smaller subset of BMCs.

On `SIGINT` or `SIGTERM`, the exporter will shut down its web server, then wait for all in-progress scrapes to finish before cleanly closing all BMC connections and sockets.

## Limitations

 - Sensor data besides power use via DCMI is currently unavailable. Issue #12 tracks the progress; unfortunately this requires delving into SDRs, which has so far been avoidable. It will likely take the form of `chassis_(intake|exhaust)_temperature_celsius` and `cpu_temperature_celsius{socket="#"}` metrics.
 - IPMI v1.5, the first to feature IPMI-over-LAN support, is currently unimplemented in the underlying library. Given IPMI v2.0 was first published in 2004, this is hopefully not relevant to most, however for the sake of legacy devices and completeness, it will be added after non-power sensor data is retrievable. The exporter itself is already version-agnostic.
