import type { FunctionalComponent } from 'preact';
import type { StatsSample } from '@/types';
import { formatBytes } from '@/lib/format';

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
  const vramRatio = ratio(metrics.vram_used_bytes, metrics.vram_total_bytes);
  const gttRatio = ratio(metrics.gtt_used_bytes, metrics.gtt_total_bytes);

  const rows = [
    {
      label: 'VRAM',
      used: metrics.vram_used_bytes,
      total: metrics.vram_total_bytes,
      ratio: vramRatio
    },
    {
      label: 'GTT',
      used: metrics.gtt_used_bytes,
      total: metrics.gtt_total_bytes,
      ratio: gttRatio
    }
  ];

  return (
    <section class="grid" style="margin-top: 1.5rem;">
      {rows.map((row) => (
        <article key={row.label} class="metric-card">
          <h3>{row.label} Usage</h3>
          <div class="progress" aria-hidden="true">
            <span style={`width: ${(row.ratio ?? 0) * 100}%`}></span>
          </div>
          <small class="muted">
            {formatBytes(row.used)} / {formatBytes(row.total)}
          </small>
        </article>
      ))}
    </section>
  );
};

export default MemoryBars;
