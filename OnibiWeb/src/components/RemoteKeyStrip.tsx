import type { RemoteInputKey } from "../types";

interface RemoteKeyStripProps {
  disabled: boolean;
  onSendKey: (key: RemoteInputKey) => void;
}

const CORE_KEYS: Array<{ label: string; key: RemoteInputKey }> = [
  { label: "Enter", key: "enter" },
  { label: "Tab", key: "tab" },
  { label: "Ctrl-C", key: "ctrl_c" },
  { label: "Ctrl-D", key: "ctrl_d" },
  { label: "Ctrl-S", key: "ctrl_s" },
  { label: "Ctrl-Q", key: "ctrl_q" },
  { label: "Bksp", key: "backspace" },
  { label: "Esc", key: "escape" },
  { label: "Delete", key: "delete" },
  { label: "Home", key: "home" },
  { label: "End", key: "end" },
  { label: "PgUp", key: "page_up" },
  { label: "PgDn", key: "page_down" },
  { label: "Space", key: "space" },
  { label: "↑", key: "arrow_up" },
  { label: "↓", key: "arrow_down" },
  { label: "←", key: "arrow_left" },
  { label: "→", key: "arrow_right" }
];

export function RemoteKeyStrip({ disabled, onSendKey }: RemoteKeyStripProps): JSX.Element {
  return (
    <section className="mf-key-strip" aria-label="Remote key controls">
      <div className="mf-key-strip-row">
        {CORE_KEYS.map(({ label, key }) => (
          <button type="button" key={key} disabled={disabled} onClick={() => onSendKey(key)}>
            {label}
          </button>
        ))}
      </div>
    </section>
  );
}
