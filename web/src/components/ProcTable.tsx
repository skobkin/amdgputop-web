import { useMemo, useState } from 'preact/hooks';
import type { FunctionalComponent } from 'preact';
import type { ProcSnapshot } from '@/types';
import { formatBytes, formatGpuTime, formatTimeAgo } from '@/lib/format';

type SortKey = 'total' | 'vram' | 'gtt' | 'pid';
type SortDirection = 'asc' | 'desc';

interface SortState {
  key: SortKey;
  direction: SortDirection;
}

const DEFAULT_SORT: SortState = { key: 'total', direction: 'desc' };

interface Props {
  snapshot?: ProcSnapshot;
}

const MAX_CMD_DISPLAY_LENGTH = 72;

const ProcTable: FunctionalComponent<Props> = ({ snapshot }) => {
  const [sort, setSort] = useState<SortState>(DEFAULT_SORT);

  const processes = useMemo(() => {
    if (!snapshot) {
      return [];
    }
    const rows = snapshot.processes.map((proc) => {
      const vram = proc.vram_bytes ?? 0;
      const gtt = proc.gtt_bytes ?? 0;
      const cmdRaw = (proc.cmd ?? '').trim();
      const cmdCollapsed =
        cmdRaw.length > MAX_CMD_DISPLAY_LENGTH
          ? `${cmdRaw.slice(0, MAX_CMD_DISPLAY_LENGTH - 1)}…`
          : cmdRaw;
      return {
        ...proc,
        totalBytes: vram + gtt,
        cmdCollapsed: cmdCollapsed || null,
        cmdTooltip: cmdRaw || null
      };
    });

    const sorted = [...rows];
    sorted.sort((a, b) => {
      let delta = 0;
      switch (sort.key) {
        case 'total':
          delta = a.totalBytes - b.totalBytes;
          break;
        case 'vram':
          delta = (a.vram_bytes ?? 0) - (b.vram_bytes ?? 0);
          break;
        case 'gtt':
          delta = (a.gtt_bytes ?? 0) - (b.gtt_bytes ?? 0);
          break;
        case 'pid':
          delta = a.pid - b.pid;
          break;
        default:
          delta = 0;
      }
      return sort.direction === 'asc' ? delta : -delta;
    });

    return sorted;
  }, [snapshot, sort]);

  if (!snapshot) {
    return null;
  }

  const toggleSort = (key: SortKey) => {
    setSort((current) => {
      if (current.key === key) {
        const nextDirection = current.direction === 'asc' ? 'desc' : 'asc';
        return { key, direction: nextDirection };
      }
      return { key, direction: key === 'pid' ? 'asc' : 'desc' };
    });
  };

  return (
    <section style="margin-top: 1.5rem;">
      <header style="margin-bottom: 0.5rem;">
        <h2 style="margin: 0;">Processes</h2>
        <small class="muted">
          Last update {formatTimeAgo(snapshot.ts)} · {processes.length} entries
        </small>
      </header>
      {processes.length === 0 ? (
        <div class="empty-state">
          <p>No processes currently using this GPU.</p>
        </div>
      ) : (
        <div class="table-responsive">
          <table class="proc-table" role="grid">
            <thead>
              <tr>
                <th scope="col" onClick={() => toggleSort('pid')} style="cursor: pointer;">PID</th>
                <th scope="col">User</th>
                <th scope="col">Name</th>
                <th scope="col" onClick={() => toggleSort('vram')} style="cursor: pointer;">VRAM</th>
                <th scope="col" onClick={() => toggleSort('gtt')} style="cursor: pointer;">GTT</th>
                <th scope="col" onClick={() => toggleSort('total')} style="cursor: pointer;">Total</th>
                <th scope="col">GPU Time</th>
              </tr>
            </thead>
            <tbody>
              {processes.map((proc) => (
                <tr key={proc.pid}>
                  <td>{proc.pid}</td>
                  <td>{proc.user || '—'}</td>
                  <td class="name-cell">
                    <div class="proc-name">
                      <strong title={proc.name || undefined}>{proc.name || '—'}</strong>
                      {proc.cmdCollapsed ? (
                        <span class="proc-cmd" title={proc.cmdTooltip || undefined}>
                          {proc.cmdCollapsed}
                        </span>
                      ) : null}
                    </div>
                  </td>
                  <td>{formatBytes(proc.vram_bytes)}</td>
                  <td>{formatBytes(proc.gtt_bytes)}</td>
                  <td>{formatBytes(proc.totalBytes)}</td>
                  <td>{formatGpuTime(proc.gpu_time_ms_per_s)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <small class="muted">
        Capabilities: VRAM/GTT {snapshot.capabilities.vram_gtt_from_fdinfo ? '✓' : '—'},
        GPU time {snapshot.capabilities.engine_time_from_fdinfo ? '✓' : '—'}
      </small>
    </section>
  );
};

export default ProcTable;
