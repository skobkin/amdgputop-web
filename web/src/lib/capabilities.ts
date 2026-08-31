import type { MetricCapabilities } from '@/types';

/**
 * Reports whether a metric is supported for the selected GPU. Missing or
 * null capabilities mean "unknown" and are treated as supported so the UI
 * keeps rendering every widget; only an explicit `false` hides one.
 */
export function metricSupported(
  capabilities: MetricCapabilities | null | undefined,
  metric: keyof MetricCapabilities
): boolean {
  return capabilities == null || capabilities[metric] !== false;
}
