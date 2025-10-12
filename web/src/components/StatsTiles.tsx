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
        <article
          class="metric-card metric-card--inline"
          title="Overall GPU engine utilization percentage"
        >
          <div class="metric-card__row">
            <h3>Load</h3>
            <span class="metric-value">{formatPercent(metrics.gpu_busy_pct, 1)}</span>
          </div>
        </article>
        <article
          class="metric-card metric-card--inline"
          title="Memory controller busy percentage"
        >
          <div class="metric-card__row">
            <h3>VRAM</h3>
            <span class="metric-value">{formatPercent(metrics.mem_busy_pct, 1)}</span>
          </div>
        </article>
        <article
          class="metric-card metric-card--inline"
          title="Graphics core clock frequency"
        >
          <div class="metric-card__row">
            <h3>Core</h3>
            <span class="metric-value">{formatMHz(metrics.sclk_mhz)}</span>
          </div>
        </article>
        <article
          class="metric-card metric-card--inline"
          title="Memory clock frequency"
        >
          <div class="metric-card__row">
            <h3>Memory</h3>
            <span class="metric-value">{formatMHz(metrics.mclk_mhz)}</span>
          </div>
        </article>
        <article class="metric-card metric-card--inline" title="GPU temperature">
          <div class="metric-card__row">
            <h3>Temp</h3>
            <span class="metric-value">{formatTemperature(metrics.temp_c)}</span>
          </div>
        </article>
        <article class="metric-card metric-card--inline" title="Fan speed in RPM">
          <div class="metric-card__row">
            <h3>Fan</h3>
            <span class="metric-value">{formatRPM(metrics.fan_rpm)}</span>
          </div>
        </article>
        <article class="metric-card metric-card--inline" title="Instantaneous power draw">
          <div class="metric-card__row">
            <h3>Power</h3>
            <span class="metric-value">{formatPower(metrics.power_w)}</span>
          </div>
        </article>
      </section>
      <small class="muted stats-updated">Last update {formatTimeAgo(ts)}</small>
    </>
  );
};

export default StatsTiles;
