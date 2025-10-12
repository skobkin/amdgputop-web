import type { FunctionalComponent } from 'preact';
import type { StatsSample } from '@/types';
import { formatMHz, formatPercent, formatPower, formatTemperature, formatRPM, formatTimeAgo } from '@/lib/format';

interface Props {
  sample?: StatsSample;
}

const StatsTiles: FunctionalComponent<Props> = ({ sample }) => {
  if (!sample) {
    return (
      <div class="empty-state">
        <strong>No telemetry yet</strong>
        <p>Waiting for the first metrics sample from sampler.</p>
      </div>
    );
  }

  const { metrics, ts } = sample;

  return (
    <>
      <section class="grid stats-grid">
        <article class="metric-card">
          <h3>GPU Busy</h3>
          <span class="metric-value">{formatPercent(metrics.gpu_busy_pct, 1)}</span>
        </article>
        <article class="metric-card">
          <h3>Memory Busy</h3>
          <span class="metric-value">{formatPercent(metrics.mem_busy_pct, 1)}</span>
        </article>
        <article class="metric-card">
          <h3>Core Clock</h3>
          <span class="metric-value">{formatMHz(metrics.sclk_mhz)}</span>
        </article>
        <article class="metric-card">
          <h3>Memory Clock</h3>
          <span class="metric-value">{formatMHz(metrics.mclk_mhz)}</span>
        </article>
        <article class="metric-card">
          <h3>Temperature</h3>
          <span class="metric-value">{formatTemperature(metrics.temp_c)}</span>
        </article>
        <article class="metric-card">
          <h3>Fan Speed</h3>
          <span class="metric-value">{formatRPM(metrics.fan_rpm)}</span>
        </article>
        <article class="metric-card">
          <h3>Power Draw</h3>
          <span class="metric-value">{formatPower(metrics.power_w)}</span>
        </article>
      </section>
      <small class="muted">Last update {formatTimeAgo(ts)}</small>
    </>
  );
};

export default StatsTiles;
