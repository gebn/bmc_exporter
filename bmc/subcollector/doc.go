// Package subcollector houses implementations of collector.Subcollector that
// know how to identify and gather a subset of a BMC's metrics. These are used
// by collector.Collector to encapsulate this logic, to make it easier to add
// new ones without inadvertently breaking something.
package subcollector
