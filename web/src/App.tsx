import { useEffect, useMemo, useRef } from 'preact/hooks';
import GpuSelector from '@/components/GpuSelector';
import StatsTiles from '@/components/StatsTiles';
import MemoryBars from '@/components/MemoryBars';
import ProcTable from '@/components/ProcTable';
import { useAppStore } from './store';
import type { ServerMessage, StatsSample, ProcSnapshot } from './types';
import { formatTimeAgo } from './lib/format';

const WS_RECONNECT_DELAY_MS = 2000;
const WS_HEARTBEAT_INTERVAL_MS = 10000;

const App = () => {
  const gpus = useAppStore((state) => state.gpus);
  const selectedGpuId = useAppStore((state) => state.selectedGpuId);
  const statsByGpu = useAppStore((state) => state.statsByGpu);
  const procsByGpu = useAppStore((state) => state.procsByGpu);
  const connection = useAppStore((state) => state.connection);
  const features = useAppStore((state) => state.features);
  const sampleIntervalMs = useAppStore((state) => state.sampleIntervalMs);
  const lastUpdatedTs = useAppStore((state) => state.lastUpdatedTs);
  const version = useAppStore((state) => state.version);
  const error = useAppStore((state) => state.error);
  const setGPUs = useAppStore((state) => state.setGPUs);
  const setSelectedGpuId = useAppStore((state) => state.setSelectedGpuId);
  const setConnection = useAppStore((state) => state.setConnection);
  const setFeatures = useAppStore((state) => state.setFeatures);
  const setSampleInterval = useAppStore((state) => state.setSampleInterval);
  const updateStats = useAppStore((state) => state.updateStats);
  const updateProcs = useAppStore((state) => state.updateProcs);
  const clearGpuData = useAppStore((state) => state.clearGpuData);
  const setVersion = useAppStore((state) => state.setVersion);
  const setError = useAppStore((state) => state.setError);

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimerRef = useRef<number | null>(null);
  const heartbeatTimerRef = useRef<number | null>(null);
  const selectedGpuIdRef = useRef<string | null>(null);

  useEffect(() => {
    selectedGpuIdRef.current = selectedGpuId;
  }, [selectedGpuId]);

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
      try {
        const res = await fetch('/api/version');
        if (!res.ok) {
          throw new Error(`HTTP ${res.status}`);
        }
        const payload = await res.json();
        if (!cancelled) {
          setVersion(payload);
        }
      } catch (err) {
        console.warn('Unable to load version info', err);
      }
    };
    load();
    return () => {
      cancelled = true;
    };
  }, [setVersion]);

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
        window.clearTimeout(reconnectTimerRef.current);
      }
      reconnectTimerRef.current = window.setTimeout(connect, WS_RECONNECT_DELAY_MS);
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
          heartbeatTimerRef.current = window.setInterval(() => {
            if (socket.readyState === WebSocket.OPEN) {
              socket.send(JSON.stringify({ type: 'ping' }));
            }
          }, WS_HEARTBEAT_INTERVAL_MS);
        };

        socket.onmessage = (event: MessageEvent<string>) => {
          try {
            const message: ServerMessage = JSON.parse(event.data);
            switch (message.type) {
              case 'hello':
                setFeatures(message.features ?? {});
                setSampleInterval(message.interval_ms);
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
      socket?.close();
    };
  }, [setConnection, setError, setFeatures, setGPUs, setSampleInterval, updateProcs, updateStats]);

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

  return (
    <main>
      <header>
        <div>
          <h1 style="margin: 0;">amdgpu_top-web</h1>
          <p style="margin: 0; color: rgba(255,255,255,0.7);">Live AMD GPU telemetry for the web</p>
        </div>
        <GpuSelector
          gpus={gpus}
          selectedGpuId={selectedGpuId}
          onChange={(id) => setSelectedGpuId(id)}
        />
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
        <a href="https://github.com/skobkin/amdgputop-web" target="_blank" rel="noreferrer">
          GitHub →
        </a>
      </footer>
    </main>
  );
};

export default App;
