import uPlot from 'uplot';
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { getCssVar, type ResolvedTheme } from '@/lib/theme';

interface ChartTooltip {
  time: string;
  value: string;
}

interface Props {
  title: string;
  data: [number[], Array<number | null>];
  height?: number;
  stroke: string;
  theme: ResolvedTheme;
  valueFormatter: (value: number | null) => string;
}

const formatTooltipTime = new Intl.DateTimeFormat(undefined, {
  month: 'numeric',
  day: 'numeric',
  year: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit'
});
const formatXAxisTime = new Intl.DateTimeFormat(undefined, {
  hour: 'numeric',
  minute: '2-digit'
});

// uPlot renders y-axis tick labels right-aligned inside a gutter exactly as wide
// as the configured axis `size`; labels wider than that budget get clipped at
// the canvas edge (see issue #32). uPlot does not measure labels itself, so
// measure the formatted values here and size the gutter to the widest one.
const measureCtx = document.createElement('canvas').getContext('2d');

const yAxisSize = (u: any, values: string[], axisIdx: number): number => {
  // uPlot calls size() once at construction before any ticks exist and only
  // creates the axis element when the returned size is > 0, so fall back to a
  // safe fixed width and let the sizing cycle apply the measured width as soon
  // as data produces ticks.
  if (values == null || values.length === 0 || !measureCtx) {
    return 64;
  }
  const axis = u.axes[axisIdx];
  // axis.font[0] is scaled by devicePixelRatio, so convert the measurement back
  // to CSS pixels via the ratio uPlot itself renders with.
  measureCtx.font = axis.font[0];
  let widest = 0;
  for (const value of values) {
    widest = Math.max(widest, measureCtx.measureText(value).width);
  }
  // The label's right edge is inset by the tick length and the gap (mirrors
  // uPlot's own layout), plus one pixel of rounding headroom.
  const tickSize = axis.ticks?.show ? axis.ticks.size : 0;
  return Math.ceil(widest / uPlot.pxRatio) + axis.gap + tickSize + 1;
};

const UPlotChart = ({ title, data, height = 140, stroke, theme, valueFormatter }: Props) => {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const plotRef = useRef<any>(null);
  const resizeObserverRef = useRef<ResizeObserver | null>(null);
  const lastOptionsRef = useRef<object | null>(null);
  const [tooltip, setTooltip] = useState<ChartTooltip | null>(null);

  const options = useMemo(() => {
    const plugin = {
      hooks: {
        setCursor: (u: any) => {
          if (!u.cursor) {
            return;
          }
          const idx = u.cursor.idx;
          if (idx == null || idx < 0) {
            setTooltip(null);
            return;
          }
          const ts = u.data[0][idx] as number | null | undefined;
          const val = u.data[1][idx] as number | null | undefined;
          if (!ts || Number.isNaN(ts)) {
            setTooltip(null);
            return;
          }
          setTooltip({
            time: formatTooltipTime.format(new Date(ts)),
            value: valueFormatter(val ?? null)
          });
        }
      }
    };

    const formatAxisValue = (value: number) => {
      const formatted = valueFormatter(value);
      return formatted.replace(/\.0(?=[^\d]|$)/, '');
    };

    return {
      width: 0,
      height,
      // We feed timestamps in milliseconds (Date.parse / Date.now).
      ms: 1 as const,
      scales: {
        x: {
          time: true
        },
        y: {
          auto: true
        }
      },
      axes: [
        {
          stroke: getCssVar('--chart-axis', 'rgba(128, 128, 128, 0.85)'),
          values: (_u: any, ticks: number[]) =>
            ticks.map((tick) => formatXAxisTime.format(new Date(tick))),
          grid: {
            stroke: getCssVar('--chart-grid', 'rgba(128, 128, 128, 0.2)')
          }
        },
        {
          stroke: getCssVar('--chart-axis', 'rgba(128, 128, 128, 0.85)'),
          size: yAxisSize,
          values: (_u: any, ticks: number[]) => ticks.map((tick) => formatAxisValue(tick)),
          grid: {
            stroke: getCssVar('--chart-grid', 'rgba(128, 128, 128, 0.2)')
          }
        }
      ],
      legend: {
        show: false
      },
      series: [
        {},
        {
          label: title,
          stroke,
          width: 2
        }
      ],
      cursor: {
        show: true,
        drag: {
          x: false,
          y: false
        },
        points: {
          size: 5
        },
        sync: {
          key: 'gpu-charts'
        }
      },
      plugins: [plugin]
    };
    // `theme` is a dependency because axis/grid colors are resolved from CSS
    // variables at construction time and change with the active theme.
  }, [height, stroke, theme, title, valueFormatter]);

  useEffect(() => {
    if (!containerRef.current) {
      return;
    }

    const container = containerRef.current;
    const width = Math.max(1, container.clientWidth);
    const uPlotCtor = uPlot;

    if (!plotRef.current) {
      plotRef.current = new uPlotCtor({ ...options, width }, data, container);
    } else if (lastOptionsRef.current !== options) {
      // Options changed (e.g. theme switch): uPlot cannot re-theme in place,
      // so rebuild the plot with the current data.
      plotRef.current.destroy();
      plotRef.current = new uPlotCtor({ ...options, width }, data, container);
    } else {
      plotRef.current.setData(data);
    }
    lastOptionsRef.current = options;

    resizeObserverRef.current?.disconnect();
    resizeObserverRef.current = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const nextWidth = Math.max(1, Math.floor(entry.contentRect.width));
        plotRef.current?.setSize({ width: nextWidth, height });
      }
    });
    resizeObserverRef.current.observe(container);

    return () => {
      resizeObserverRef.current?.disconnect();
      resizeObserverRef.current = null;
    };
  }, [data, height, options]);

  useEffect(() => {
    return () => {
      resizeObserverRef.current?.disconnect();
      resizeObserverRef.current = null;
      plotRef.current?.destroy();
      plotRef.current = null;
    };
  }, []);

  return (
    <div class="chart-card">
      <div class="chart-card__header">
        <span>{title}</span>
        {tooltip ? (
          <span class="chart-tooltip">
            {tooltip.time} · {tooltip.value}
          </span>
        ) : (
          <span class="chart-tooltip muted">Hover for values</span>
        )}
      </div>
      <div class="chart-canvas" ref={containerRef} />
    </div>
  );
};

export default UPlotChart;
