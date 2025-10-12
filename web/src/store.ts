import { create } from 'zustand';
import type {
  ConnectionStatus,
  GPUInfo,
  ProcSnapshot,
  StatsSample,
  VersionInfo
} from './types';

type FeatureMap = Record<string, boolean>;

export type UIScale = 'smallest' | 'small' | 'compact' | 'medium' | 'comfortable' | 'large';

const UI_SCALE_STORAGE_KEY = 'amdgputop-web:ui-scale';

function isValidScale(value: string | null): value is UIScale {
  return (
    value === 'smallest' ||
    value === 'small' ||
    value === 'compact' ||
    value === 'medium' ||
    value === 'comfortable' ||
    value === 'large'
  );
}

function readInitialUiScale(): UIScale {
  if (typeof window === 'undefined') {
    return 'large';
  }
  try {
    const stored = window.localStorage.getItem(UI_SCALE_STORAGE_KEY);
    if (isValidScale(stored)) {
      return stored;
    }
  } catch {
    // Ignore storage errors and use fallback.
  }
  return 'large';
}

const initialUiScale = readInitialUiScale();

interface AppState {
  gpus: GPUInfo[];
  selectedGpuId: string | null;
  connection: ConnectionStatus;
  features: FeatureMap;
  sampleIntervalMs: number | null;
  statsByGpu: Record<string, StatsSample>;
  procsByGpu: Record<string, ProcSnapshot>;
  lastUpdatedTs: number | null;
  version: VersionInfo | null;
  error: string | null;
  uiScale: UIScale;
  setGPUs: (gpus: GPUInfo[]) => void;
  setSelectedGpuId: (id: string | null) => void;
  setConnection: (status: ConnectionStatus) => void;
  setFeatures: (features: FeatureMap) => void;
  setSampleInterval: (ms: number) => void;
  updateStats: (sample: StatsSample) => void;
  updateProcs: (snapshot: ProcSnapshot) => void;
  clearGpuData: (gpuId: string) => void;
  setVersion: (info: VersionInfo) => void;
  setError: (message: string | null) => void;
  setUiScale: (scale: UIScale) => void;
}

export const useAppStore = create<AppState>((set) => ({
  gpus: [],
  selectedGpuId: null,
  connection: 'idle',
  features: {},
  sampleIntervalMs: null,
  statsByGpu: {},
  procsByGpu: {},
  lastUpdatedTs: null,
  version: null,
  error: null,
  uiScale: initialUiScale,
  setGPUs: (gpus) =>
    set((state) => {
      let selected = state.selectedGpuId;
      if (!selected || !gpus.some((gpu) => gpu.id === selected)) {
        selected = gpus.length > 0 ? gpus[0].id : null;
      }
      return { gpus, selectedGpuId: selected };
    }),
  setSelectedGpuId: (id) => set({ selectedGpuId: id }),
  setConnection: (status) => set({ connection: status }),
  setFeatures: (features) => set({ features }),
  setSampleInterval: (ms) => set({ sampleIntervalMs: ms }),
  updateStats: (sample) =>
    set((state) => ({
      statsByGpu: { ...state.statsByGpu, [sample.gpu_id]: sample },
      lastUpdatedTs: Date.now()
    })),
  updateProcs: (snapshot) =>
    set((state) => ({
      procsByGpu: { ...state.procsByGpu, [snapshot.gpu_id]: snapshot },
      lastUpdatedTs: Date.now()
    })),
  clearGpuData: (gpuId) =>
    set((state) => {
      const nextStats = { ...state.statsByGpu };
      const nextProcs = { ...state.procsByGpu };
      delete nextStats[gpuId];
      delete nextProcs[gpuId];
      return { statsByGpu: nextStats, procsByGpu: nextProcs };
    }),
  setVersion: (info) => set({ version: info }),
  setError: (message) => set({ error: message }),
  setUiScale: (scale) =>
    set((state) => {
      if (state.uiScale === scale) {
        return {};
      }
      if (typeof window !== 'undefined') {
        try {
          window.localStorage.setItem(UI_SCALE_STORAGE_KEY, scale);
        } catch {
          // Ignore storage errors.
        }
      }
      return { uiScale: scale };
    })
}));
