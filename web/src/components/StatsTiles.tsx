import type { FunctionalComponent } from 'preact';
import { useMemo } from 'preact/hooks';
import type { MetricCapabilities, Metrics, StatsSample } from '@/types';
import { metricSupported } from '@/lib/capabilities';
import { formatMHz, formatPercent, formatPower, formatTemperature, formatRPM, formatTimeAgo } from '@/lib/format';

interface TileDefinition {
  metric: keyof MetricCapabilities;
  label: string;
  title: string;
  format: (metrics: Metrics) => string;
}

const TILES: TileDefinition[] = [
  {
    metric: 'gpu_busy_pct',
    label: 'Load',
    title: 'Overall GPU engine utilization percentage',
    format: (metrics) => formatPercent(metrics.gpu_busy_pct, 1)
  },
  {
    metric: 'mem_busy_pct',
    label: 'Memory',
    title: 'Memory controller busy percentage',
    format: (metrics) => formatPercent(metrics.mem_busy_pct, 1)
  },
  {
    metric: 'sclk_mhz',
    label: 'Core',
    title: 'Graphics core clock frequency',
    format: (metrics) => formatMHz(metrics.sclk_mhz)
  },
  {
    metric: 'mclk_mhz',
    label: 'MemClk',
    title: 'Memory clock frequency',
    format: (metrics) => formatMHz(metrics.mclk_mhz)
  },
  {
    metric: 'temp_c',
    label: 'Temp',
    title: 'GPU temperature',
    format: (metrics) => formatTemperature(metrics.temp_c)
  },
  {
    metric: 'fan_rpm',
    label: 'Fan',
    title: 'Fan speed in RPM',
    format: (metrics) => formatRPM(metrics.fan_rpm)
  },
  {
    metric: 'power_w',
    label: 'Power',
    title: 'Instantaneous power draw',
    format: (metrics) => formatPower(metrics.power_w)
  }
];

interface Props {
  sample?: StatsSample;
  capabilities?: MetricCapabilities | null;
  nowMs: number;
}

const StatsTiles: FunctionalComponent<Props> = ({ sample, capabilities, nowMs }) => {
  const updatedLabel = useMemo(() => {
    if (!sample?.ts) {
      return '—';
    }
    const sampleTs = Date.parse(sample.ts);
    if (Number.isNaN(sampleTs)) {
      return '—';
    }
    const effectiveNowMs = nowMs < sampleTs ? Date.now() : nowMs;
    return formatTimeAgo(sample.ts, effectiveNowMs);
  }, [nowMs, sample?.ts]);

  if (!sample) {
    return (
      <div class="empty-state">
        <strong>No telemetry yet</strong>
        <p>Waiting for the first metrics sample from sampler.</p>
      </div>
    );
  }

  const { metrics } = sample;
  const visibleTiles = TILES.filter((tile) => metricSupported(capabilities, tile.metric));

  if (visibleTiles.length === 0) {
    return null;
  }

  return (
    <>
      <section class="grid stats-grid">
        {visibleTiles.map((tile) => (
          <article key={tile.metric} class="metric-card metric-card--inline" title={tile.title}>
            <div class="metric-card__row">
              <h3>{tile.label}</h3>
              <span class="metric-value">{tile.format(metrics)}</span>
            </div>
          </article>
        ))}
      </section>
      <small class="muted stats-updated">Last update {updatedLabel}</small>
    </>
  );
};

export default StatsTiles;
