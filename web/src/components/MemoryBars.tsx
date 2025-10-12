import type { FunctionalComponent } from 'preact';
import type { StatsSample } from '@/types';
import { formatBytes, formatPercent } from '@/lib/format';

interface Props {
  sample?: StatsSample;
}

function ratio(used: number | null, total: number | null): number | null {
  if (used == null || total == null || total <= 0) {
    return null;
  }
  return Math.min(1, Math.max(0, used / total));
}

const MemoryBars: FunctionalComponent<Props> = ({ sample }) => {
  if (!sample) {
    return null;
  }

  const { metrics } = sample;
  const loadRatio =
    metrics.gpu_busy_pct == null || Number.isNaN(metrics.gpu_busy_pct)
      ? null
      : Math.min(1, Math.max(0, metrics.gpu_busy_pct / 100));
  const vramRatio = ratio(metrics.vram_used_bytes, metrics.vram_total_bytes);
  const gttRatio = ratio(metrics.gtt_used_bytes, metrics.gtt_total_bytes);

  const memoryRows = [
    {
      key: 'vram',
      label: 'VRAM Usage',
      used: metrics.vram_used_bytes,
      total: metrics.vram_total_bytes,
      ratio: vramRatio
    },
    {
      key: 'gtt',
      label: 'GTT Usage',
      used: metrics.gtt_used_bytes,
      total: metrics.gtt_total_bytes,
      ratio: gttRatio
    }
  ];

  return (
    <section class="grid usage-grid">
      <article class="metric-card metric-card--compact">
        <div class="metric-card__row">
          <h3>GPU Load</h3>
          <span class="metric-inline-value">{formatPercent(metrics.gpu_busy_pct, 1)}</span>
        </div>
        <div
          class="progress progress--thin"
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round((loadRatio ?? 0) * 100)}
        >
          <span style={`width: ${(loadRatio ?? 0) * 100}%`}></span>
        </div>
      </article>
      {memoryRows.map((row) => {
        const usedText =
          row.used != null && row.total != null
            ? `${formatBytes(row.used)} / ${formatBytes(row.total)}`
            : formatBytes(row.used);

        return (
          <article key={row.key} class="metric-card metric-card--compact">
            <div class="metric-card__row">
              <h3>{row.label}</h3>
              <span class="metric-inline-value">{usedText}</span>
            </div>
            <div
              class="progress progress--thin"
              role="progressbar"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={Math.round((row.ratio ?? 0) * 100)}
            >
              <span style={`width: ${(row.ratio ?? 0) * 100}%`}></span>
            </div>
          </article>
        );
      })}
    </section>
  );
};

export default MemoryBars;
