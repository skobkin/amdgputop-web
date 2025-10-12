import type { FunctionalComponent } from 'preact';
import type { GPUInfo } from '@/types';

interface Props {
  gpus: GPUInfo[];
  selectedGpuId: string | null;
  onChange: (id: string) => void;
  id?: string;
}

const GpuSelector: FunctionalComponent<Props> = ({ gpus, selectedGpuId, onChange, id }) => {
  if (gpus.length === 0) {
    return (
      <select id={id} disabled>
        <option>No GPUs detected</option>
      </select>
    );
  }

  return (
    <select
      id={id}
      value={selectedGpuId ?? ''}
      onChange={(event) => onChange((event.currentTarget as HTMLSelectElement).value)}
    >
      {gpus.map((gpu) => (
        <option key={gpu.id} value={gpu.id}>
          {gpu.name || gpu.id} ({gpu.id})
        </option>
      ))}
    </select>
  );
};

export default GpuSelector;
