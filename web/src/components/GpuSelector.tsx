import type { FunctionalComponent } from 'preact';
import type { GPUInfo } from '@/types';

interface Props {
  gpus: GPUInfo[];
  selectedGpuId: string | null;
  onChange: (id: string) => void;
}

const GpuSelector: FunctionalComponent<Props> = ({ gpus, selectedGpuId, onChange }) => {
  if (gpus.length === 0) {
    return (
      <div class="empty-state">
        <strong>No GPUs detected</strong>
        <p>Ensure the amdgpu driver is loaded and this host exposes /dev/dri.</p>
      </div>
    );
  }

  return (
    <label>
      <span>GPU</span>
      <select
        value={selectedGpuId ?? ''}
        onChange={(event) => onChange((event.currentTarget as HTMLSelectElement).value)}
      >
        {gpus.map((gpu) => (
          <option key={gpu.id} value={gpu.id}>
            {gpu.name || gpu.id} ({gpu.id})
          </option>
        ))}
      </select>
    </label>
  );
};

export default GpuSelector;
