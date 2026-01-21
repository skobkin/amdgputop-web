import type { StatsSample } from '@/types';

export type ChartMetricKey =
  | 'gpuLoad'
  | 'vramUsed'
  | 'gttUsed'
  | 'sclk'
  | 'mclk'
  | 'temp'
  | 'power'
  | 'fan';

export interface ChartHistory {
  capacity: number;
  size: number;
  cursor: number;
  timestamps: Array<number | null>;
  gpuLoad: Array<number | null>;
  vramUsed: Array<number | null>;
  gttUsed: Array<number | null>;
  sclk: Array<number | null>;
  mclk: Array<number | null>;
  temp: Array<number | null>;
  power: Array<number | null>;
  fan: Array<number | null>;
}

export function createChartHistory(capacity: number): ChartHistory {
  return {
    capacity,
    size: 0,
    cursor: 0,
    timestamps: new Array(capacity),
    gpuLoad: new Array(capacity),
    vramUsed: new Array(capacity),
    gttUsed: new Array(capacity),
    sclk: new Array(capacity),
    mclk: new Array(capacity),
    temp: new Array(capacity),
    power: new Array(capacity),
    fan: new Array(capacity)
  };
}

export function appendChartSample(
  history: ChartHistory | undefined,
  sample: StatsSample,
  capacity: number
): ChartHistory {
  const next = history && history.capacity === capacity ? { ...history } : createChartHistory(capacity);
  const index = next.cursor;
  next.timestamps[index] = Date.parse(sample.ts);
  next.gpuLoad[index] = sample.metrics.gpu_busy_pct ?? null;
  next.vramUsed[index] = sample.metrics.vram_used_bytes ?? null;
  next.gttUsed[index] = sample.metrics.gtt_used_bytes ?? null;
  next.sclk[index] = sample.metrics.sclk_mhz ?? null;
  next.mclk[index] = sample.metrics.mclk_mhz ?? null;
  next.temp[index] = sample.metrics.temp_c ?? null;
  next.power[index] = sample.metrics.power_w ?? null;
  next.fan[index] = sample.metrics.fan_rpm ?? null;
  next.cursor = (index + 1) % next.capacity;
  next.size = Math.min(next.size + 1, next.capacity);
  return next;
}

export function buildChartSeries(
  history: ChartHistory,
  windowPoints: number,
  key: ChartMetricKey
): { x: number[]; y: Array<number | null> } {
  const count = Math.min(history.size, windowPoints);
  if (count <= 0) {
    return { x: [], y: [] };
  }
  const x: number[] = new Array(count);
  const y: Array<number | null> = new Array(count);
  const start = (history.cursor - count + history.capacity) % history.capacity;
  for (let i = 0; i < count; i += 1) {
    const idx = (start + i) % history.capacity;
    x[i] = history.timestamps[idx] ?? Date.now();
    y[i] = history[key][idx] ?? null;
  }
  return { x, y };
}
