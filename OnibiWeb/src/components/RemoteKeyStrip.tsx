import type { RemoteInputKey } from "../types";

interface RemoteKeyStripProps {
  disabled: boolean;
  onSendKey: (key: RemoteInputKey) => void;
}

const KEY_BUTTONS: Array<{ label: string; key: RemoteInputKey }> = [
  { label: "Enter", key: "enter" },
  { label: "Ctrl-C", key: "ctrl_c" },
  { label: "Up", key: "arrow_up" },
  { label: "Down", key: "arrow_down" },
  { label: "Left", key: "arrow_left" },
  { label: "Right", key: "arrow_right" },
  { label: "Space", key: "space" }
];

export function RemoteKeyStrip({ disabled, onSendKey }: RemoteKeyStripProps): JSX.Element {
  return (
    <div className="remote-key-strip">
      {KEY_BUTTONS.map(({ label, key }) => (
        <button
          type="button"
          key={key}
          disabled={disabled}
          onClick={() => onSendKey(key)}
          className="key-button"
        >
          {label}
        </button>
      ))}
    </div>
  );
}
