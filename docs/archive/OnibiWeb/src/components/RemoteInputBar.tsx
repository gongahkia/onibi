import { ClipboardEvent, FormEvent, useRef, useState } from "react";

interface RemoteInputBarProps {
  disabled: boolean;
  onSubmitLine: (text: string) => void;
  onPasteText?: (text: string) => void;
  onUploadFile?: (file: File) => void;
  autoEnter?: boolean;
}

export function RemoteInputBar({
  disabled,
  onSubmitLine,
  onPasteText,
  onUploadFile,
  autoEnter = true
}: RemoteInputBarProps): JSX.Element {
  const [text, setText] = useState("");
  const inputRef = useRef<HTMLInputElement | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

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

  const submitPaste = (pastedText: string) => {
    if (!pastedText || disabled || !onPasteText) {
      return;
    }
    onPasteText(pastedText);
    setText("");
    window.requestAnimationFrame(() => {
      inputRef.current?.focus();
    });
  };

  const handlePaste = (event: ClipboardEvent<HTMLInputElement>) => {
    const pastedText = event.clipboardData.getData("text");
    if (pastedText.includes("\n") || pastedText.includes("\r")) {
      event.preventDefault();
      submitPaste(pastedText);
    }
  };

  const handlePasteButton = async () => {
    try {
      const pastedText = await navigator.clipboard.readText();
      submitPaste(pastedText);
    } catch {
      // Browser clipboard permission errors are intentionally non-fatal.
    }
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
        onPaste={handlePaste}
      />
      {onPasteText && (
        <button type="button" disabled={disabled} onClick={handlePasteButton}>
          Paste
        </button>
      )}
      {onUploadFile && (
        <>
          <input
            ref={fileInputRef}
            type="file"
            className="mf-hidden-file-input"
            onChange={(event) => {
              const file = event.target.files?.[0];
              if (file) {
                onUploadFile(file);
              }
              event.target.value = "";
            }}
          />
          <button type="button" disabled={disabled} onClick={() => fileInputRef.current?.click()}>
            File
          </button>
        </>
      )}
      <button type="submit" disabled={disabled || text.length === 0}>
        {autoEnter ? "Send ↵" : "Send"}
      </button>
    </form>
  );
}
