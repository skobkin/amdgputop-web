import { useCallback, useEffect, useMemo, useRef } from 'preact/hooks';
import GpuSelector from '@/components/GpuSelector';
import StatsTiles from '@/components/StatsTiles';
import MemoryBars from '@/components/MemoryBars';
import ProcTable from '@/components/ProcTable';
import { useAppStore, type UIScale } from './store';
import {
  createVersionInfo,
  type ServerMessage,
  type StatsSample,
  type ProcSnapshot,
  type VersionInfo,
  type VersionInfoPayload
} from './types';
import { formatTimeAgo } from './lib/format';

const WS_RECONNECT_DELAY_MS = 2000;
const WS_HEARTBEAT_INTERVAL_MS = 10000;
const UI_SCALE_OPTIONS: UIScale[] = ['smallest', 'small', 'compact', 'medium', 'comfortable', 'large'];

const App = () => {
  const gpus = useAppStore((state) => state.gpus);
  const selectedGpuId = useAppStore((state) => state.selectedGpuId);
  const statsByGpu = useAppStore((state) => state.statsByGpu);
  const procsByGpu = useAppStore((state) => state.procsByGpu);
  const connection = useAppStore((state) => state.connection);
  const features = useAppStore((state) => state.features);
  const sampleIntervalMs = useAppStore((state) => state.sampleIntervalMs);
  const chartsMaxPoints = useAppStore((state) => state.chartsMaxPoints);
  const chartWindowPoints = useAppStore((state) => state.chartWindowPoints);
  const lastUpdatedTs = useAppStore((state) => state.lastUpdatedTs);
  const version = useAppStore((state) => state.version);
  const error = useAppStore((state) => state.error);
  const setGPUs = useAppStore((state) => state.setGPUs);
  const setSelectedGpuId = useAppStore((state) => state.setSelectedGpuId);
  const setConnection = useAppStore((state) => state.setConnection);
  const setFeatures = useAppStore((state) => state.setFeatures);
  const setSampleInterval = useAppStore((state) => state.setSampleInterval);
  const setChartsMaxPoints = useAppStore((state) => state.setChartsMaxPoints);
  const setChartWindowPoints = useAppStore((state) => state.setChartWindowPoints);
  const updateStats = useAppStore((state) => state.updateStats);
  const updateProcs = useAppStore((state) => state.updateProcs);
  const clearGpuData = useAppStore((state) => state.clearGpuData);
  const setVersion = useAppStore((state) => state.setVersion);
  const setError = useAppStore((state) => state.setError);
  const uiScale = useAppStore((state) => state.uiScale);
  const setUiScale = useAppStore((state) => state.setUiScale);

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimerRef = useRef<number | null>(null);
  const heartbeatTimerRef = useRef<number | null>(null);
  const selectedGpuIdRef = useRef<string | null>(null);
  const versionRef = useRef<VersionInfo | null>(null);
  const hasConnectedRef = useRef(false);

  const fetchVersionInfo = useCallback(async (): Promise<VersionInfo | null> => {
    try {
      const res = await fetch('/api/version', { cache: 'no-store' });
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`);
      }
      const payload = createVersionInfo((await res.json()) as VersionInfoPayload);
      return payload;
    } catch (err) {
      console.warn('Unable to load version info', err);
      return null;
    }
  }, []);

  useEffect(() => {
    selectedGpuIdRef.current = selectedGpuId;
  }, [selectedGpuId]);

  useEffect(() => {
    versionRef.current = version;
  }, [version]);

  // Fetch GPU list via REST so UI can render before WS hello arrives.
  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const res = await fetch('/api/gpus');
        if (!res.ok) {
          throw new Error(`HTTP ${res.status}`);
        }
        const payload = await res.json();
        if (!cancelled) {
          setGPUs(payload);
          setError(null);
        }
      } catch (err) {
        if (!cancelled) {
          console.error('Failed to load GPU list', err);
          setError('Failed to load GPU list');
        }
      }
    };

    load();

    return () => {
      cancelled = true;
    };
  }, [setGPUs, setError]);

  // Fetch version info once.
  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      const payload = await fetchVersionInfo();
      if (!cancelled && payload) {
        setVersion(payload);
      }
    };
    load();
    return () => {
      cancelled = true;
    };
  }, [fetchVersionInfo, setVersion]);

  // WebSocket lifecycle.
  useEffect(() => {
    let stop = false;

    const clearHeartbeat = () => {
      if (heartbeatTimerRef.current != null) {
        window.clearInterval(heartbeatTimerRef.current);
        heartbeatTimerRef.current = null;
      }
    };

    const scheduleReconnect = () => {
      if (stop) {
        return;
      }
      if (reconnectTimerRef.current != null) {
        return;
      }
      reconnectTimerRef.current = window.setTimeout(() => {
        reconnectTimerRef.current = null;
        connect();
      }, WS_RECONNECT_DELAY_MS);
    };

    const connect = () => {
      if (stop) {
        return;
      }
      if (reconnectTimerRef.current != null) {
        window.clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
      clearHeartbeat();

      const existing = wsRef.current;
      if (existing && existing.readyState !== WebSocket.CLOSED && existing.readyState !== WebSocket.CLOSING) {
        // Already have an active connection; no need to open another.
        return;
      }

      try {
        setConnection('connecting');
        setError(null);
        const url = new URL(window.location.href);
        url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
        url.pathname = '/ws';
        url.search = '';

        const socket = new WebSocket(url.toString());
        wsRef.current = socket;

        socket.onopen = () => {
          const wasConnected = hasConnectedRef.current;
          hasConnectedRef.current = true;
          setConnection('open');
          const gpuId = selectedGpuIdRef.current;
          if (gpuId) {
            socket.send(
              JSON.stringify({
                type: 'subscribe',
                gpu_id: gpuId
              })
            );
          }
          socket.send(JSON.stringify({ type: 'ping' }));
          heartbeatTimerRef.current = window.setInterval(() => {
            if (socket.readyState === WebSocket.OPEN) {
              socket.send(JSON.stringify({ type: 'ping' }));
            }
          }, WS_HEARTBEAT_INTERVAL_MS);

          void (async () => {
            const payload = await fetchVersionInfo();
            if (!payload || stop) {
              return;
            }
            if (wasConnected) {
              const current = versionRef.current;
              if (current && !current.isEqual(payload)) {
                window.location.reload();
                return;
              }
            }
            setVersion(payload);
          })();
        };

        socket.onmessage = (event: MessageEvent<string>) => {
          try {
            const message: ServerMessage = JSON.parse(event.data);
            switch (message.type) {
              case 'hello':
                setFeatures(message.features ?? {});
                setSampleInterval(message.interval_ms);
                if (typeof message.charts_max_points === 'number') {
                  setChartsMaxPoints(message.charts_max_points);
                }
                setGPUs(message.gpus ?? []);
                break;
              case 'stats':
                updateStats(message as StatsSample);
                break;
              case 'procs':
                updateProcs(message as ProcSnapshot);
                break;
              case 'error':
                setError(message.message);
                break;
              case 'pong':
                break;
              default:
                console.warn('Unhandled message', message);
            }
          } catch (err) {
            console.error('Failed to decode websocket message', err);
          }
        };

        socket.onerror = () => {
          setConnection('error');
        };

        socket.onclose = () => {
          clearHeartbeat();
          if (wsRef.current === socket) {
            wsRef.current = null;
          }
          setConnection('closed');
          scheduleReconnect();
        };
      } catch (err) {
        console.error('Failed to open websocket', err);
        setConnection('error');
        scheduleReconnect();
      }
    };

    connect();

    return () => {
      stop = true;
      clearHeartbeat();
      if (reconnectTimerRef.current != null) {
        window.clearTimeout(reconnectTimerRef.current);
      }
      reconnectTimerRef.current = null;
      const socket = wsRef.current;
      wsRef.current = null;
      socket?.close(1000, 'shutdown');
    };
  }, [fetchVersionInfo, setConnection, setError, setFeatures, setGPUs, setSampleInterval, setVersion, updateProcs, updateStats]);

  // Resubscribe whenever selection changes.
  useEffect(() => {
    if (!selectedGpuId) {
      return;
    }
    const ws = wsRef.current;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'subscribe', gpu_id: selectedGpuId }));
    }

    // Kick off explicit fetch for latest data.
    let aborted = false;
    const fetchLatest = async () => {
      try {
        const res = await fetch(`/api/gpus/${selectedGpuId}/metrics`);
        if (res.ok) {
          const payload = await res.json();
          if (!aborted) {
            updateStats(payload);
          }
        }
      } catch (err) {
        console.debug('metrics fetch failed', err);
      }

      if (features.procs) {
        try {
          const res = await fetch(`/api/gpus/${selectedGpuId}/procs`);
          if (res.ok) {
            const payload = await res.json();
            if (!aborted) {
              updateProcs(payload);
            }
          }
        } catch (err) {
          console.debug('procs fetch failed', err);
        }
      }
    };

    fetchLatest();

    return () => {
      aborted = true;
    };
  }, [features.procs, selectedGpuId, updateProcs, updateStats]);

  // Clear data when GPU disappears (e.g., unplugged).
  useEffect(() => {
    const knownIds = new Set(gpus.map((gpu) => gpu.id));
    Object.keys(statsByGpu).forEach((id) => {
      if (!knownIds.has(id)) {
        clearGpuData(id);
      }
    });
  }, [clearGpuData, gpus, statsByGpu]);

  const statsSample = useMemo<StatsSample | undefined>(() => {
    if (!selectedGpuId) {
      return undefined;
    }
    return statsByGpu[selectedGpuId];
  }, [selectedGpuId, statsByGpu]);

  const procSnapshot = useMemo<ProcSnapshot | undefined>(() => {
    if (!selectedGpuId) {
      return undefined;
    }
    return procsByGpu[selectedGpuId];
  }, [procsByGpu, selectedGpuId]);

  const chartWindowOptions = useMemo(() => {
    if (!features.charts || !sampleIntervalMs || chartsMaxPoints <= 0) {
      return [];
    }
    const candidatePoints = [30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 14400];
    const options = candidatePoints.filter((points) => points <= chartsMaxPoints);
    if (!options.includes(chartsMaxPoints)) {
      options.push(chartsMaxPoints);
    }
    return options.sort((a, b) => a - b).map((points) => ({
      points,
      label: formatChartWindowLabel(points, sampleIntervalMs)
    }));
  }, [chartsMaxPoints, features.charts, sampleIntervalMs]);

  return (
    <main data-scale={uiScale}>
      <header>
        <h1 style="margin: 0;">AMD GPU stats</h1>
        <div class="gpu-picker">
          <GpuSelector
            gpus={gpus}
            selectedGpuId={selectedGpuId}
            onChange={(id) => setSelectedGpuId(id)}
            id="gpu-select"
          />
        </div>
      </header>

      {connection !== 'open' && (
        <div
          class={`status-banner ${connection === 'error' ? 'error' : 'info'}`}
          role="status"
        >
          {connection === 'connecting' && 'Connecting to sampler…'}
          {connection === 'error' && 'Connection lost — attempting to reconnect'}
          {connection === 'closed' && 'Disconnected — retrying shortly'}
        </div>
      )}

      {error && (
        <div class="status-banner warn" role="alert">
          {error}
        </div>
      )}

      <StatsTiles sample={statsSample} />
      <MemoryBars sample={statsSample} />
      {features.procs ? <ProcTable snapshot={procSnapshot} /> : null}

      <footer>
        {sampleIntervalMs ? (
          <span class="badge">Update interval {sampleIntervalMs} ms</span>
        ) : null}
        {lastUpdatedTs ? (
          <span>Last frame {formatTimeAgo(new Date(lastUpdatedTs).toISOString())}</span>
        ) : (
          <span>No telemetry received yet</span>
        )}
        {version ? (
          <span>
            Version {version.version}{' '}
            {version.commit ? `(${version.commit.substring(0, 7)})` : ''}
          </span>
        ) : null}
        <div class="scale-picker">
          <label for="ui-scale">UI Scale</label>
          <select
            id="ui-scale"
            value={uiScale}
            onChange={(event) =>
              setUiScale((event.currentTarget as HTMLSelectElement).value as UIScale)
            }
          >
            {UI_SCALE_OPTIONS.map((option) => (
              <option key={option} value={option}>
                {option.charAt(0).toUpperCase() + option.slice(1)}
              </option>
            ))}
          </select>
        </div>
        {features.charts && chartWindowOptions.length > 0 ? (
          <div class="scale-picker">
            <label for="chart-window">Chart window</label>
            <select
              id="chart-window"
              value={chartWindowPoints}
              onChange={(event) =>
                setChartWindowPoints(Number((event.currentTarget as HTMLSelectElement).value))
              }
            >
              {chartWindowOptions.map((option) => (
                <option key={option.points} value={option.points}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>
        ) : null}
        <a href="https://github.com/skobkin/amdgputop-web" target="_blank" rel="noreferrer">
          GitHub →
        </a>
      </footer>
    </main>
  );
};

export default App;

function formatChartWindowLabel(points: number, intervalMs: number): string {
  const totalMs = points * intervalMs;
  const seconds = Math.max(1, Math.round(totalMs / 1000));
  if (seconds < 60) {
    return `${seconds}s (${points} pts)`;
  }
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) {
    return `${minutes}m (${points} pts)`;
  }
  const hours = Math.round(minutes / 60);
  return `${hours}h (${points} pts)`;
}
