import { FormEvent, useRef, useState } from "react";

interface RemoteInputBarProps {
  disabled: boolean;
  onSubmitLine: (text: string) => void;
  autoEnter?: boolean;
}

export function RemoteInputBar({
  disabled,
  onSubmitLine,
  autoEnter = true
}: RemoteInputBarProps): JSX.Element {
  const [text, setText] = useState("");
  const inputRef = useRef<HTMLInputElement | null>(null);

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!text || disabled) {
      return;
    }
    onSubmitLine(text);
    setText("");
    window.requestAnimationFrame(() => {
      inputRef.current?.focus();
    });
  };

  return (
    <form className="mf-input-bar" onSubmit={handleSubmit}>
      <input
        ref={inputRef}
        type="text"
        value={text}
        onChange={(event) => setText(event.target.value)}
        placeholder={autoEnter ? "Type command (Send includes Enter)" : "Type command"}
        autoCapitalize="off"
        autoComplete="off"
        autoCorrect="off"
        spellCheck={false}
        disabled={disabled}
      />
      <button type="submit" disabled={disabled || text.length === 0}>
        {autoEnter ? "Send ↵" : "Send"}
      </button>
    </form>
  );
}
