# Prometheus BMC Exporter

This is a [Prometheus](https://prometheus.io/docs/introduction/overview/) [exporter](https://prometheus.io/docs/instrumenting/exporters/) for Baseboard Management Controllers, or BMCs.
It is special in that there is no dependency on `ipmitool`, `freeipmi` or `openipmi` - the underlying communication is implemented in pure Go.
This allows not only the exporter to run very efficiently, but reduces the load on BMCs by keeping sessions open and caching discovered capabilities, meaning fewer commands need to be ran each scrape.

## What's a BMC?

BMCs are dedicated hardware found on server-grade motherboards, providing useful data including temperatures, fan speed and possibly power consumption, even when the system is in standby. BMCs are accessible over the network, usually via a dedicated network interface.
BMCs implement the [IPMI](https://www.intel.co.uk/content/www/uk/en/servers/ipmi/ipmi-home.html) protocol, which clients use to interact with them via "IPMI over LAN".
Although IPMI was first released in 1998, and no updates have been made to the protocol since 2015, it remains the industry standard way to manage servers.
Dell refers to their BMC as [iDRAC](https://www.dell.com/support/article/uk/en/ukbsdt1/sln129295/dell-poweredge-how-to-configure-the-idrac-system-management-options-on-servers?lang=en), HP calls theirs [iLO](https://www.hpe.com/uk/en/servers/integrated-lights-out-ilo.html), and Oracle calls theirs [ILOM](https://docs.oracle.com/cd/E19203-01/819-1160-13/overview.html), but really they are all just IPMI implementations, which this exporter can talk to.

## Usage

Binaries can be downloaded from the [releases](https://github.com/gebn/bmc_exporter/releases) page.
The exporter requires IPMI *User* privileges on each BMC it scrapes.

## Session Providers

Only a very limited set of data is available over IPMI without establishing a session, so establishing one is necessary for any useful client.
The exporter is IPMI version agnostic, so to abstract the core logic away from session establishment, there is the concept of *session providers*.
Providers implement the method `Session(addr string) (bmc.Session, error)`.
While there is provision in the specification for anonymous authentication, and this is supported, most BMCs will have a username and password configured, if only by the manufacturer.
Currently, there is a single provider implementation that reads usernames and passwords from a config file (`secrets.yml`), however if you have stronger security, you can implement one to retrieve them by any means, e.g. over the network.

## FAQs

### How much load does the exporter place on a BMC?

The exporter will open a maximum of one session with each BMC, and reuse it between scrapes.
If two concurrent scrapes arrive for a given BMC, the exporter will serialise them.
The IPMI specification recommends a 60s (+/-3s) timeout on sessions, so provided your scrape interval is below this, this will be the only session for the lifetime of the exporter process.
Because 
This efficient use of sessions is important, as BMCs are only required to support 4 sessions between their console and LAN channels.
On `SIGINT`, the exporter will cleanly close all open sessions before exiting.

### Which IPMI versions are supported?

The exporter is version agnostic, supporting both v1.5 and v2.0.
IPMI v1.5 was the first to feature LAN support.
Any mildly recent BMC likely supports v2.0.

IPMI itself does not specify any commands for retrieving overall power use of the system - only motherboard rails.
If the BMC supports [DCMI](https://www.intel.com/content/dam/www/public/us/en/documents/technical-specifications/dcmi-v1-5-rev-spec.pdf) (an extension of IPMI v2.0), and the machine's PSU supports PMBus, this exporter will expose the machine's power consumption.
If you do not see the `chassis_power_consumption_watts` metric, it is because the BMC does not satisfy one of these criteria.
You can use the underlying [`bmc`](https://github.com/gebn/bmc) library to investigate this further.
