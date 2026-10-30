import type { PresetInfo } from "../types";

export interface PresetPickerProps {
  presets: PresetInfo[];
  value: string;
  onChange: (name: string) => void;
}

// PresetPicker chooses the permission profile for a new session. Existing
// sessions keep their stored preset; switching means creating a new session.
export function PresetPicker({ presets, value, onChange }: PresetPickerProps) {
  const current = presets.find((p) => p.name === value);
  return (
    <label className="preset-picker" title={current?.description ?? ""}>
      <span>预设</span>
      <select value={value} onChange={(e) => onChange(e.target.value)}>
        {presets.map((p) => (
          <option key={p.name} value={p.name}>
            {p.name}
          </option>
        ))}
      </select>
    </label>
  );
}
