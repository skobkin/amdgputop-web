import { create } from 'zustand';
import type {
  ConnectionStatus,
  GPUInfo,
  ProcSnapshot,
  StatsSample,
  VersionInfo
} from './types';
import { appendChartSample, type ChartHistory } from './lib/chartHistory';

type FeatureMap = Record<string, boolean>;

export type UIScale = 'smallest' | 'small' | 'compact' | 'medium' | 'comfortable' | 'large';

const UI_SCALE_STORAGE_KEY = 'amdgputop-web:ui-scale';
const CHART_WINDOW_STORAGE_KEY = 'amdgputop-web:chart-window-points';
const CHARTS_COLLAPSED_STORAGE_KEY = 'amdgputop-web:charts-collapsed';
const SELECTED_GPU_STORAGE_KEY = 'amdgputop-web:selected-gpu-id';

const DEFAULT_CHART_WINDOW_POINTS = 300;

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

function readInitialChartWindow(): number {
  if (typeof window === 'undefined') {
    return DEFAULT_CHART_WINDOW_POINTS;
  }
  try {
    const stored = window.localStorage.getItem(CHART_WINDOW_STORAGE_KEY);
    if (stored) {
      const parsed = Number.parseInt(stored, 10);
      if (!Number.isNaN(parsed) && parsed > 0) {
        return parsed;
      }
    }
  } catch {
    // Ignore storage errors and use fallback.
  }
  return DEFAULT_CHART_WINDOW_POINTS;
}

function readInitialChartsCollapsed(): boolean {
  if (typeof window === 'undefined') {
    return true;
  }
  try {
    const stored = window.localStorage.getItem(CHARTS_COLLAPSED_STORAGE_KEY);
    if (stored != null) {
      return stored === 'true';
    }
  } catch {
    // Ignore storage errors and use fallback.
  }
  return true;
}

function readInitialSelectedGpuId(): string | null {
  if (typeof window === 'undefined') {
    return null;
  }
  try {
    const stored = window.localStorage.getItem(SELECTED_GPU_STORAGE_KEY);
    if (stored) {
      return stored;
    }
  } catch {
    // Ignore storage errors and use fallback.
  }
  return null;
}

const initialUiScale = readInitialUiScale();
const initialChartWindowPoints = readInitialChartWindow();
const initialChartsCollapsed = readInitialChartsCollapsed();
const initialSelectedGpuId = readInitialSelectedGpuId();

interface AppState {
  gpus: GPUInfo[];
  selectedGpuId: string | null;
  connection: ConnectionStatus;
  features: FeatureMap;
  sampleIntervalMs: number | null;
  chartsMaxPoints: number;
  chartWindowPoints: number;
  chartsCollapsed: boolean;
  statsByGpu: Record<string, StatsSample>;
  procsByGpu: Record<string, ProcSnapshot>;
  chartHistoryByGpu: Record<string, ChartHistory>;
  lastUpdatedTs: number | null;
  version: VersionInfo | null;
  error: string | null;
  uiScale: UIScale;
  setGPUs: (gpus: GPUInfo[]) => void;
  setSelectedGpuId: (id: string | null) => void;
  setConnection: (status: ConnectionStatus) => void;
  setFeatures: (features: FeatureMap) => void;
  setSampleInterval: (ms: number) => void;
  setChartsMaxPoints: (maxPoints: number) => void;
  setChartWindowPoints: (points: number) => void;
  setChartsCollapsed: (collapsed: boolean) => void;
  updateStats: (sample: StatsSample) => void;
  updateProcs: (snapshot: ProcSnapshot) => void;
  clearGpuData: (gpuId: string) => void;
  setVersion: (info: VersionInfo) => void;
  setError: (message: string | null) => void;
  setUiScale: (scale: UIScale) => void;
}

export const useAppStore = create<AppState>((set) => ({
  gpus: [],
  selectedGpuId: initialSelectedGpuId,
  connection: 'idle',
  features: {},
  sampleIntervalMs: null,
  chartsMaxPoints: 7200,
  chartWindowPoints: initialChartWindowPoints,
  chartsCollapsed: initialChartsCollapsed,
  statsByGpu: {},
  procsByGpu: {},
  chartHistoryByGpu: {},
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
      if (typeof window !== 'undefined') {
        try {
          if (selected) {
            window.localStorage.setItem(SELECTED_GPU_STORAGE_KEY, selected);
          } else {
            window.localStorage.removeItem(SELECTED_GPU_STORAGE_KEY);
          }
        } catch {
          // Ignore storage errors.
        }
      }
      return { gpus, selectedGpuId: selected };
    }),
  setSelectedGpuId: (id) =>
    set(() => {
      if (typeof window !== 'undefined') {
        try {
          if (id) {
            window.localStorage.setItem(SELECTED_GPU_STORAGE_KEY, id);
          } else {
            window.localStorage.removeItem(SELECTED_GPU_STORAGE_KEY);
          }
        } catch {
          // Ignore storage errors.
        }
      }
      return { selectedGpuId: id };
    }),
  setConnection: (status) => set({ connection: status }),
  setFeatures: (features) => set({ features }),
  setSampleInterval: (ms) => set({ sampleIntervalMs: ms }),
  setChartsMaxPoints: (maxPoints) =>
    set((state) => {
      if (!Number.isFinite(maxPoints) || maxPoints <= 0) {
        return {};
      }
      const windowPoints = Math.min(state.chartWindowPoints, maxPoints);
      return {
        chartsMaxPoints: maxPoints,
        chartWindowPoints: windowPoints > 0 ? windowPoints : 1
      };
    }),
  setChartWindowPoints: (points) =>
    set((state) => {
      if (!Number.isFinite(points) || points <= 0) {
        return {};
      }
      const clamped = Math.min(points, state.chartsMaxPoints);
      if (typeof window !== 'undefined') {
        try {
          window.localStorage.setItem(CHART_WINDOW_STORAGE_KEY, String(clamped));
        } catch {
          // Ignore storage errors.
        }
      }
      return { chartWindowPoints: clamped };
    }),
  setChartsCollapsed: (collapsed) =>
    set(() => {
      if (typeof window !== 'undefined') {
        try {
          window.localStorage.setItem(CHARTS_COLLAPSED_STORAGE_KEY, String(collapsed));
        } catch {
          // Ignore storage errors.
        }
      }
      return { chartsCollapsed: collapsed };
    }),
  updateStats: (sample) =>
    set((state) => ({
      statsByGpu: { ...state.statsByGpu, [sample.gpu_id]: sample },
      chartHistoryByGpu: state.features.charts
        ? {
            ...state.chartHistoryByGpu,
            [sample.gpu_id]: appendChartSample(
              state.chartHistoryByGpu[sample.gpu_id],
              sample,
              state.chartsMaxPoints
            )
          }
        : state.chartHistoryByGpu,
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
      const nextHistory = { ...state.chartHistoryByGpu };
      delete nextStats[gpuId];
      delete nextProcs[gpuId];
      delete nextHistory[gpuId];
      return { statsByGpu: nextStats, procsByGpu: nextProcs, chartHistoryByGpu: nextHistory };
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
