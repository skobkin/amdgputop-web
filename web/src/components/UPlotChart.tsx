import { useEffect, useMemo, useRef, useState } from 'preact/hooks';

interface ChartTooltip {
  time: string;
  value: string;
}

interface Props {
  title: string;
  data: [number[], Array<number | null>];
  height?: number;
  stroke: string;
  valueFormatter: (value: number | null) => string;
}

const formatTime = new Intl.DateTimeFormat(undefined, {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit'
});

const UPlotChart = ({ title, data, height = 140, stroke, valueFormatter }: Props) => {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const plotRef = useRef<any>(null);
  const resizeObserverRef = useRef<ResizeObserver | null>(null);
  const [uPlotModule, setUPlotModule] = useState<any>(null);
  const [tooltip, setTooltip] = useState<ChartTooltip | null>(null);

  useEffect(() => {
    let active = true;
    void import('uplot').then((mod) => {
      if (!active) {
        return;
      }
      setUPlotModule(mod.default ?? mod);
    });
    return () => {
      active = false;
    };
  }, []);

  const options = useMemo(() => {
    const plugin = {
      hooks: {
        setCursor: (u: any) => {
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
            time: formatTime.format(new Date(ts)),
            value: valueFormatter(val ?? null)
          });
        }
      }
    };

    return {
      title: null,
      width: 0,
      height,
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
          stroke: 'rgba(255, 255, 255, 0.5)',
          grid: {
            stroke: 'rgba(255, 255, 255, 0.06)'
          }
        },
        {
          stroke: 'rgba(255, 255, 255, 0.55)',
          grid: {
            stroke: 'rgba(255, 255, 255, 0.06)'
          }
        }
      ],
      series: [
        {},
        {
          label: title,
          stroke,
          width: 1.6
        }
      ],
      cursor: {
        drag: {
          x: false,
          y: false
        },
        points: {
          show: true,
          size: 5
        }
      },
      plugins: [plugin]
    };
  }, [height, stroke, title, valueFormatter]);

  useEffect(() => {
    if (!uPlotModule || !containerRef.current) {
      return;
    }

    const container = containerRef.current;
    const width = Math.max(1, container.clientWidth);
    const uPlotCtor = uPlotModule;

    if (!plotRef.current) {
      plotRef.current = new uPlotCtor({ ...options, width }, data, container);
    } else {
      plotRef.current.setData(data);
    }

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
  }, [data, height, options, uPlotModule]);

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
