import { useEffect, useRef } from "react";
import { LexicalComposer } from "@lexical/react/LexicalComposer";
import { PlainTextPlugin } from "@lexical/react/LexicalPlainTextPlugin";
import { ContentEditable } from "@lexical/react/LexicalContentEditable";
import { HistoryPlugin } from "@lexical/react/LexicalHistoryPlugin";
import { OnChangePlugin } from "@lexical/react/LexicalOnChangePlugin";
import { LexicalErrorBoundary } from "@lexical/react/LexicalErrorBoundary";
import { useLexicalComposerContext } from "@lexical/react/LexicalComposerContext";
import {
  $createParagraphNode,
  $createTextNode,
  $getRoot,
  type EditorState,
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
        <PlainTextOnChange value={value} onChange={onChange} />
      </div>
    </LexicalComposer>
  );
}

function PlainTextOnChange({
  onChange,
  value,
}: {
  onChange: (value: string) => void;
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

  function handleChange(editorState: EditorState) {
    editorState.read(() => {
      const text = $getRoot().getTextContent();
      lastFromEditor.current = text;
      onChange(text);
    });
  }

  return <OnChangePlugin ignoreSelectionChange onChange={handleChange} />;
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
