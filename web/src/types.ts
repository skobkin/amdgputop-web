export interface GPUInfo {
  id: string;
  pci: string;
  pci_id: string;
  name: string;
  render_node: string;
}

export interface Metrics {
  gpu_busy_pct: number | null;
  mem_busy_pct: number | null;
  sclk_mhz: number | null;
  mclk_mhz: number | null;
  temp_c: number | null;
  fan_rpm: number | null;
  power_w: number | null;
  vram_used_bytes: number | null;
  vram_total_bytes: number | null;
  gtt_used_bytes: number | null;
  gtt_total_bytes: number | null;
}

export interface StatsSample {
  type: 'stats';
  gpu_id: string;
  ts: string;
  metrics: Metrics;
}

export interface ProcScannerCapabilities {
  vram_gtt_from_fdinfo: boolean;
  engine_time_from_fdinfo: boolean;
}

export interface ProcInfo {
  pid: number;
  uid: number;
  user: string;
  name: string;
  cmd: string;
  render_node: string;
  vram_bytes: number | null;
  gtt_bytes: number | null;
  gpu_time_ms_per_s: number | null;
}

export interface ProcSnapshot {
  type: 'procs';
  gpu_id: string;
  ts: string;
  capabilities: ProcScannerCapabilities;
  processes: ProcInfo[];
}

export interface HelloMessage {
  type: 'hello';
  interval_ms: number;
  gpus: GPUInfo[];
  features: Record<string, boolean>;
  charts_max_points?: number;
}

export interface ErrorMessage {
  type: 'error';
  message: string;
}

export interface PongMessage {
  type: 'pong';
}

export type ServerMessage = HelloMessage | StatsSample | ProcSnapshot | ErrorMessage | PongMessage;

export type ConnectionStatus = 'idle' | 'connecting' | 'open' | 'closed' | 'error';

export interface VersionInfoPayload {
  version: string;
  commit: string;
  build_time: string;
}

export interface VersionInfo extends VersionInfoPayload {
  isEqual(other: VersionInfo | null | undefined): boolean;
}

export function createVersionInfo(payload: VersionInfoPayload): VersionInfo {
  const normalized: VersionInfo = {
    ...payload,
    isEqual(other) {
      if (!other) {
        return false;
      }
      return (
        normalized.version === other.version &&
        normalized.commit === other.commit &&
        normalized.build_time === other.build_time
      );
    }
  };
  return normalized;
}
