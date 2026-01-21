import type { FunctionalComponent } from 'preact';
import { useMemo } from 'preact/hooks';
import UPlotChart from '@/components/UPlotChart';
import type { ChartHistory, ChartMetricKey } from '@/lib/chartHistory';
import { buildChartSeries } from '@/lib/chartHistory';
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
  title: string;
  stroke: string;
  formatValue: (value: number | null) => string;
}

const CHARTS: ChartDefinition[] = [
  {
    key: 'gpuLoad',
    title: 'GPU Load',
    stroke: 'rgba(108, 92, 231, 0.9)',
    formatValue: (value) => formatPercent(value, 1)
  },
  {
    key: 'vramUsed',
    title: 'VRAM Usage',
    stroke: 'rgba(129, 236, 236, 0.85)',
    formatValue: (value) => formatBytes(value, 1)
  },
  {
    key: 'gttUsed',
    title: 'GTT Usage',
    stroke: 'rgba(252, 211, 77, 0.85)',
    formatValue: (value) => formatBytes(value, 1)
  },
  {
    key: 'sclk',
    title: 'Core Clock',
    stroke: 'rgba(255, 118, 117, 0.85)',
    formatValue: (value) => formatMHz(value)
  },
  {
    key: 'mclk',
    title: 'Memory Clock',
    stroke: 'rgba(162, 155, 254, 0.85)',
    formatValue: (value) => formatMHz(value)
  },
  {
    key: 'temp',
    title: 'Temperature',
    stroke: 'rgba(255, 204, 112, 0.85)',
    formatValue: (value) => formatTemperature(value)
  },
  {
    key: 'power',
    title: 'Power',
    stroke: 'rgba(116, 185, 255, 0.85)',
    formatValue: (value) => formatPower(value)
  },
  {
    key: 'fan',
    title: 'Fan',
    stroke: 'rgba(214, 48, 49, 0.85)',
    formatValue: (value) => formatRPM(value)
  }
];

interface Props {
  history: ChartHistory;
  windowPoints: number;
  intervalMs: number;
}

const ChartsPanel: FunctionalComponent<Props> = ({ history, windowPoints, intervalMs }) => {
  const chartData = useMemo(() => {
    return CHARTS.map((chart) => ({
      chart,
      series: buildChartSeries(history, windowPoints, intervalMs, chart.key)
    }));
  }, [history, intervalMs, windowPoints]);

  return (
    <div class="charts-grid">
      {chartData.map(({ chart, series }) => (
        <UPlotChart
          key={chart.key}
          title={chart.title}
          data={[series.x, series.y]}
          stroke={chart.stroke}
          valueFormatter={chart.formatValue}
        />
      ))}
    </div>
  );
};

export default ChartsPanel;
