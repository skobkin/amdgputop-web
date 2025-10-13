package httpserver

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/skobkin/amdgputop-web/internal/gpu"
	"github.com/skobkin/amdgputop-web/internal/sampler"
)

type gpuMetricsCollector struct {
	sampler *sampler.Manager
	gpus    []gpu.Info
	metrics []gpuMetric
}

type gpuMetric struct {
	desc      *prometheus.Desc
	valueType prometheus.ValueType
	extract   func(sample sampler.Sample) (float64, bool)
}

func newGPUMetricsCollector(gpus []gpu.Info, samplerManager *sampler.Manager) prometheus.Collector {
	if samplerManager == nil || len(gpus) == 0 {
		return nil
	}

	collector := &gpuMetricsCollector{
		sampler: samplerManager,
		gpus:    append([]gpu.Info(nil), gpus...),
	}

	desc := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc(
			prometheus.BuildFQName("amdgputop", "gpu", name),
			help,
			[]string{"gpu_id"},
			nil,
		)
	}

	collector.metrics = []gpuMetric{
		{
			desc:      desc("busy_percent", "Current graphics engine busy percentage."),
			valueType: prometheus.GaugeValue,
			extract: func(sample sampler.Sample) (float64, bool) {
				if sample.Metrics.GPUBusyPct == nil {
					return 0, false
				}
				return *sample.Metrics.GPUBusyPct, true
			},
		},
		{
			desc:      desc("mem_busy_percent", "Current memory controller busy percentage."),
			valueType: prometheus.GaugeValue,
			extract: func(sample sampler.Sample) (float64, bool) {
				if sample.Metrics.MemBusyPct == nil {
					return 0, false
				}
				return *sample.Metrics.MemBusyPct, true
			},
		},
		{
			desc:      desc("sclk_mhz", "Current shader clock in MHz."),
			valueType: prometheus.GaugeValue,
			extract: func(sample sampler.Sample) (float64, bool) {
				if sample.Metrics.SCLKMHz == nil {
					return 0, false
				}
				return *sample.Metrics.SCLKMHz, true
			},
		},
		{
			desc:      desc("mclk_mhz", "Current memory clock in MHz."),
			valueType: prometheus.GaugeValue,
			extract: func(sample sampler.Sample) (float64, bool) {
				if sample.Metrics.MCLKMHz == nil {
					return 0, false
				}
				return *sample.Metrics.MCLKMHz, true
			},
		},
		{
			desc:      desc("temperature_celsius", "Current GPU temperature in Celsius."),
			valueType: prometheus.GaugeValue,
			extract: func(sample sampler.Sample) (float64, bool) {
				if sample.Metrics.TempC == nil {
					return 0, false
				}
				return *sample.Metrics.TempC, true
			},
		},
		{
			desc:      desc("fan_rpm", "Current fan speed in RPM."),
			valueType: prometheus.GaugeValue,
			extract: func(sample sampler.Sample) (float64, bool) {
				if sample.Metrics.FanRPM == nil {
					return 0, false
				}
				return *sample.Metrics.FanRPM, true
			},
		},
		{
			desc:      desc("power_watts", "Current GPU power draw in Watts."),
			valueType: prometheus.GaugeValue,
			extract: func(sample sampler.Sample) (float64, bool) {
				if sample.Metrics.PowerW == nil {
					return 0, false
				}
				return *sample.Metrics.PowerW, true
			},
		},
		{
			desc:      desc("vram_used_bytes", "Current VRAM usage in bytes."),
			valueType: prometheus.GaugeValue,
			extract: func(sample sampler.Sample) (float64, bool) {
				if sample.Metrics.VRAMUsedBytes == nil {
					return 0, false
				}
				return float64(*sample.Metrics.VRAMUsedBytes), true
			},
		},
		{
			desc:      desc("vram_total_bytes", "Total VRAM capacity in bytes."),
			valueType: prometheus.GaugeValue,
			extract: func(sample sampler.Sample) (float64, bool) {
				if sample.Metrics.VRAMTotalBytes == nil {
					return 0, false
				}
				return float64(*sample.Metrics.VRAMTotalBytes), true
			},
		},
		{
			desc:      desc("gtt_used_bytes", "Current GTT usage in bytes."),
			valueType: prometheus.GaugeValue,
			extract: func(sample sampler.Sample) (float64, bool) {
				if sample.Metrics.GTTUsedBytes == nil {
					return 0, false
				}
				return float64(*sample.Metrics.GTTUsedBytes), true
			},
		},
		{
			desc:      desc("gtt_total_bytes", "Total GTT capacity in bytes."),
			valueType: prometheus.GaugeValue,
			extract: func(sample sampler.Sample) (float64, bool) {
				if sample.Metrics.GTTTotalBytes == nil {
					return 0, false
				}
				return float64(*sample.Metrics.GTTTotalBytes), true
			},
		},
		{
			desc:      desc("sample_timestamp_seconds", "Unix timestamp of the latest GPU sample."),
			valueType: prometheus.GaugeValue,
			extract: func(sample sampler.Sample) (float64, bool) {
				if sample.Timestamp.IsZero() {
					return 0, false
				}
				return float64(sample.Timestamp.Unix()), true
			},
		},
		{
			desc:      desc("sample_age_seconds", "Seconds elapsed since the latest GPU sample was collected."),
			valueType: prometheus.GaugeValue,
			extract: func(sample sampler.Sample) (float64, bool) {
				if sample.Timestamp.IsZero() {
					return 0, false
				}
				age := time.Since(sample.Timestamp).Seconds()
				if age < 0 {
					age = 0
				}
				return age, true
			},
		},
	}

	return collector
}

func (c *gpuMetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, metric := range c.metrics {
		ch <- metric.desc
	}
}

func (c *gpuMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	if c.sampler == nil {
		return
	}
	for _, info := range c.gpus {
		sample, ok := c.sampler.Latest(info.ID)
		if !ok {
			continue
		}
		for _, metric := range c.metrics {
			value, ok := metric.extract(sample)
			if !ok {
				continue
			}
			ch <- prometheus.MustNewConstMetric(metric.desc, metric.valueType, value, info.ID)
		}
	}
}
