import type { FunctionalComponent } from 'preact';
import { useMemo } from 'preact/hooks';
import UPlotChart from '@/components/UPlotChart';
import type { ChartHistory, ChartMetricKey } from '@/lib/chartHistory';
import { buildChartSeries } from '@/lib/chartHistory';
import { metricSupported } from '@/lib/capabilities';
import { getCssVar, type ResolvedTheme } from '@/lib/theme';
import { useAppStore } from '@/store';
import type { MetricCapabilities } from '@/types';
import {
  formatBytes,
  formatMHz,
  formatPercent,
  formatPower,
  formatRPM,
  formatTemperature
} from '@/lib/format';

interface ChartDefinition {
  key: ChartMetricKey;
  metric: keyof MetricCapabilities;
  title: string;
  strokeVar: string;
  fallbackStroke: string;
  formatValue: (value: number | null) => string;
}

const CHARTS: ChartDefinition[] = [
  {
    key: 'gpuLoad',
    metric: 'gpu_busy_pct',
    title: 'GPU Load',
    strokeVar: '--chart-load',
    fallbackStroke: 'rgba(108, 92, 231, 0.9)',
    formatValue: (value) => formatPercent(value, 1)
  },
  {
    key: 'vramUsed',
    metric: 'vram',
    title: 'VRAM Usage',
    strokeVar: '--chart-vram',
    fallbackStroke: 'rgba(129, 236, 236, 0.85)',
    formatValue: (value) => formatBytes(value, 1)
  },
  {
    key: 'gttUsed',
    metric: 'gtt',
    title: 'GTT Usage',
    strokeVar: '--chart-gtt',
    fallbackStroke: 'rgba(252, 211, 77, 0.85)',
    formatValue: (value) => formatBytes(value, 1)
  },
  {
    key: 'sclk',
    metric: 'sclk_mhz',
    title: 'Core Clock',
    strokeVar: '--chart-sclk',
    fallbackStroke: 'rgba(255, 118, 117, 0.85)',
    formatValue: (value) => formatMHz(value)
  },
  {
    key: 'mclk',
    metric: 'mclk_mhz',
    title: 'Memory Clock',
    strokeVar: '--chart-mclk',
    fallbackStroke: 'rgba(162, 155, 254, 0.85)',
    formatValue: (value) => formatMHz(value)
  },
  {
    key: 'temp',
    metric: 'temp_c',
    title: 'Temperature',
    strokeVar: '--chart-temp',
    fallbackStroke: 'rgba(255, 204, 112, 0.85)',
    formatValue: (value) => formatTemperature(value)
  },
  {
    key: 'power',
    metric: 'power_w',
    title: 'Power',
    strokeVar: '--chart-power',
    fallbackStroke: 'rgba(116, 185, 255, 0.85)',
    formatValue: (value) => formatPower(value)
  },
  {
    key: 'fan',
    metric: 'fan_rpm',
    title: 'Fan',
    strokeVar: '--chart-fan',
    fallbackStroke: 'rgba(214, 48, 49, 0.85)',
    formatValue: (value) => formatRPM(value)
  }
];

interface Props {
  history: ChartHistory;
  windowPoints: number;
  intervalMs: number;
  capabilities?: MetricCapabilities | null;
}

const ChartsPanel: FunctionalComponent<Props> = ({ history, windowPoints, intervalMs, capabilities }) => {
  const resolvedTheme = useAppStore((state) => state.resolvedTheme);

  const charts = useMemo(() => {
    return CHARTS.filter((chart) => metricSupported(capabilities, chart.metric));
  }, [capabilities]);

  const chartData = useMemo(() => {
    return charts.map((chart) => ({
      chart,
      series: buildChartSeries(history, windowPoints, intervalMs, chart.key)
    }));
  }, [charts, history, intervalMs, windowPoints]);

  // Series colors come from CSS variables so they follow the active theme;
  // re-resolve whenever it changes.
  const strokes = useMemo(() => {
    return new Map<ChartMetricKey, string>(
      charts.map((chart) => [chart.key, getCssVar(chart.strokeVar, chart.fallbackStroke)])
    );
    // `resolvedTheme` is a dependency because the CSS variable values change with it.
  }, [charts, resolvedTheme]);

  return (
    <div class="charts-grid">
      {chartData.map(({ chart, series }) => (
        <UPlotChart
          key={chart.key}
          title={chart.title}
          data={[series.x, series.y]}
          stroke={strokes.get(chart.key) ?? chart.fallbackStroke}
          theme={resolvedTheme}
          valueFormatter={chart.formatValue}
        />
      ))}
    </div>
  );
};

export default ChartsPanel;
