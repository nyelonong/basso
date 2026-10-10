import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { bracketMatching, HighlightStyle, StreamLanguage, syntaxHighlighting } from "@codemirror/language";
import { clojure } from "@codemirror/legacy-modes/mode/clojure";
import { EditorState, Prec } from "@codemirror/state";
import { drawSelection, EditorView, highlightActiveLine, keymap, lineNumbers } from "@codemirror/view";
import { tags } from "@lezer/highlight";

const theme = EditorView.theme(
  {
    "&": { backgroundColor: "var(--panel)", color: "var(--panel-text)", fontSize: "0.85rem", height: "100%" },
    ".cm-content": { fontFamily: "var(--font-mono)", caretColor: "var(--accent)" },
    ".cm-gutters": { backgroundColor: "var(--panel)", color: "var(--panel-muted)", border: "none" },
    ".cm-activeLine": { backgroundColor: "var(--panel-raised)" },
    ".cm-activeLineGutter": { backgroundColor: "var(--panel-raised)" },
    "&.cm-focused .cm-cursor": { borderLeftColor: "var(--accent)" },
    "&.cm-focused .cm-selectionBackground, .cm-selectionBackground": { backgroundColor: "#3a4441" },
    ".cm-matchingBracket": { outline: "1px solid var(--accent)", backgroundColor: "transparent" },
    ".cm-scroller": { lineHeight: "1.55" },
  },
  { dark: true },
);

const highlight = HighlightStyle.define([
  { tag: tags.comment, color: "var(--panel-muted)", fontStyle: "italic" },
  { tag: tags.string, color: "#f7a35c" },
  { tag: [tags.number, tags.bool, tags.atom], color: "#8fd3c1" },
  { tag: tags.keyword, color: "var(--accent)" },
  { tag: tags.variableName, color: "var(--panel-text)" },
]);

export function createEditor(
  parent: HTMLElement,
  options: { onSubmit: () => void; onChange: () => void },
): { getSource(): string; setSource(source: string): void; focus(): void } {
  const view = new EditorView({
    parent,
    state: EditorState.create({
      extensions: [
        Prec.highest(
          keymap.of([
            {
              key: "Mod-Enter",
              run: () => {
                options.onSubmit();
                return true;
              },
            },
          ]),
        ),
        lineNumbers(),
        history(),
        drawSelection(),
        highlightActiveLine(),
        bracketMatching(),
        StreamLanguage.define(clojure),
        syntaxHighlighting(highlight),
        keymap.of([...defaultKeymap, ...historyKeymap]),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) options.onChange();
        }),
        EditorView.contentAttributes.of({ "aria-label": "Fennel pattern source" }),
        theme,
      ],
    }),
  });

  return {
    getSource: () => view.state.doc.toString(),
    setSource(source) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: source } });
    },
    focus: () => view.focus(),
  };
}
