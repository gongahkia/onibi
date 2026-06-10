import { useEffect, useRef } from "react";
import { LexicalComposer } from "@lexical/react/LexicalComposer";
import { PlainTextPlugin } from "@lexical/react/LexicalPlainTextPlugin";
import { ContentEditable } from "@lexical/react/LexicalContentEditable";
import { HistoryPlugin } from "@lexical/react/LexicalHistoryPlugin";
import { LexicalErrorBoundary } from "@lexical/react/LexicalErrorBoundary";
import { useLexicalComposerContext } from "@lexical/react/LexicalComposerContext";
import {
  $createParagraphNode,
  $createTextNode,
  $getRoot,
  type LexicalEditor,
} from "lexical";

interface ProseComposerProps {
  ariaLabel: string;
  className?: string;
  placeholder?: string;
  value: string;
  onChange: (value: string) => void;
}

export function ProseComposer({
  ariaLabel,
  className,
  placeholder,
  value,
  onChange,
}: ProseComposerProps) {
  const initialConfig = {
    namespace: `onibi-prose-${ariaLabel}`,
    editorState: (editor: LexicalEditor) => writePlainText(editor, value),
    onError(error: Error) {
      throw error;
    },
    theme: {
      paragraph: "prose-composer-paragraph",
    },
  };
  return (
    <LexicalComposer initialConfig={initialConfig}>
      <div className={`prose-composer ${className ?? ""}`}>
        <PlainTextPlugin
          contentEditable={
            <ContentEditable
              aria-label={ariaLabel}
              className="prose-composer-input"
              onInput={(event) => onChange(event.currentTarget.textContent ?? "")}
            />
          }
          placeholder={
            placeholder ? (
              <div className="prose-composer-placeholder">{placeholder}</div>
            ) : null
          }
          ErrorBoundary={LexicalErrorBoundary}
        />
        <HistoryPlugin />
        <PlainTextSync value={value} />
        {import.meta.env.MODE === "test" ? (
          <textarea
            data-testid={`${ariaLabel}-plain-text`}
            value={value}
            onChange={(event) => onChange(event.target.value)}
          />
        ) : null}
      </div>
    </LexicalComposer>
  );
}

function PlainTextSync({
  value,
}: {
  value: string;
}) {
  const [editor] = useLexicalComposerContext();
  const lastFromEditor = useRef(value);

  useEffect(() => {
    if (value === lastFromEditor.current) {
      return;
    }
    writePlainText(editor, value);
    lastFromEditor.current = value;
  }, [editor, value]);

  return null;
}

function writePlainText(editor: LexicalEditor, value: string) {
  editor.update(() => {
    const root = $getRoot();
    root.clear();
    const lines = value.split("\n");
    for (const line of lines.length > 0 ? lines : [""]) {
      const paragraph = $createParagraphNode();
      if (line) {
        paragraph.append($createTextNode(line));
      }
      root.append(paragraph);
    }
  });
}
