// Tiny markdown → React renderer used by RulesViewer.
//
// Why not pull in marked / react-markdown:
//   - All 5 rules files use the same dialect (headings, paragraphs, lists,
//     tables, fenced code blocks, blockquotes, inline **bold** / `code`).
//   - Adding a 200KB+ dependency for ~80 lines of parsing is net negative
//     and conflicts with the project rule §4 "no extra build tooling
//     without written decision".
//   - The 5 rule files are static & curated, so we don't need CommonMark
//     edge cases (HTML blocks, reference images, etc.).
//
// If the dialect grows (e.g. inline HTML in the rules), replace this with
// react-markdown — the public surface (renderMarkdown(src) → ReactNode) is
// the only thing RulesViewer depends on.

import { Fragment, type ReactNode } from 'react';

interface Block {
  kind:
    | 'h1' | 'h2' | 'h3' | 'h4'
    | 'p'
    | 'ul' | 'ol'
    | 'table'
    | 'code'
    | 'quote'
    | 'hr';
  /** heading text (slugified to id) — for headings only */
  text?: string;
  /** for list: items; for table: rows (first is header) */
  items?: string[][];
  /** for code block */
  lang?: string;
  /** for paragraph / heading / quote */
  inline?: ReactNode;
  /** table column alignment hints (not used by us, but kept for future) */
}

function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/[\s　]+/g, '-')
    // strip markdown / Chinese punctuation so anchors work
    .replace(/[^\w一-鿿-]/g, '')
    .replace(/^-+|-+$/g, '');
}

/** Render inline `code`, **bold**, with everything else as plain text. */
function renderInline(input: string): ReactNode {
  if (!input) return null;
  const parts: ReactNode[] = [];
  // Match **bold** and `code` in one pass.
  const re = /(\*\*[^*]+\*\*|`[^`]+`)/g;
  let lastIdx = 0;
  let m: RegExpExecArray | null;
  let key = 0;
  while ((m = re.exec(input))) {
    if (m.index > lastIdx) {
      parts.push(input.slice(lastIdx, m.index));
    }
    const tok = m[0];
    if (tok.startsWith('**')) {
      parts.push(
        <strong key={`b${key++}`}>{tok.slice(2, -2)}</strong>,
      );
    } else {
      parts.push(
        <code key={`c${key++}`} className="md-inline-code">
          {tok.slice(1, -1)}
        </code>,
      );
    }
    lastIdx = m.index + tok.length;
  }
  if (lastIdx < input.length) {
    parts.push(input.slice(lastIdx));
  }
  return parts.map((p, i) => <Fragment key={i}>{p}</Fragment>);
}

/** Parse a markdown string into a flat list of blocks. */
function parse(src: string): Block[] {
  const lines = src.replace(/\r\n/g, '\n').split('\n');
  const blocks: Block[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];

    // blank line
    if (/^\s*$/.test(line)) {
      i++;
      continue;
    }

    // horizontal rule
    if (/^---+\s*$/.test(line)) {
      blocks.push({ kind: 'hr' });
      i++;
      continue;
    }

    // fenced code block
    if (/^```/.test(line)) {
      const lang = line.slice(3).trim();
      const body: string[] = [];
      i++;
      while (i < lines.length && !/^```/.test(lines[i])) {
        body.push(lines[i]);
        i++;
      }
      i++; // skip closing fence
      blocks.push({ kind: 'code', lang, text: body.join('\n') });
      continue;
    }

    // heading
    const h = /^(#{1,4})\s+(.*)$/.exec(line);
    if (h) {
      const level = h[1].length as 1 | 2 | 3 | 4;
      const text = h[2].trim();
      blocks.push({
        kind: (`h${level}` as Block['kind']),
        text,
        inline: renderInline(text),
      });
      i++;
      continue;
    }

    // blockquote (single-line, simple)
    if (/^>\s+/.test(line)) {
      const body = [line.replace(/^>\s+/, '')];
      i++;
      while (i < lines.length && /^>\s+/.test(lines[i])) {
        body.push(lines[i].replace(/^>\s+/, ''));
        i++;
      }
      blocks.push({
        kind: 'quote',
        inline: renderInline(body.join(' ')),
      });
      continue;
    }

    // table — header line + separator (| --- | --- |)
    if (/^\|.*\|\s*$/.test(line) && i + 1 < lines.length &&
        /^\|[\s|:-]+\|\s*$/.test(lines[i + 1])) {
      const split = (l: string) =>
        l.trim().replace(/^\||\|$/g, '').split('|').map((c) => c.trim());
      const header = split(line);
      i += 2; // skip header + separator
      const rows: string[][] = [];
      while (i < lines.length && /^\|.*\|\s*$/.test(lines[i])) {
        rows.push(split(lines[i]));
        i++;
      }
      blocks.push({ kind: 'table', items: [header, ...rows] });
      continue;
    }

    // unordered list
    if (/^[-*]\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^[-*]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^[-*]\s+/, ''));
        i++;
      }
      blocks.push({
        kind: 'ul',
        items: items.map((it) => [it]),
      });
      continue;
    }

    // ordered list
    if (/^\d+\.\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\d+\.\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\d+\.\s+/, ''));
        i++;
      }
      blocks.push({
        kind: 'ol',
        items: items.map((it) => [it]),
      });
      continue;
    }

    // paragraph (collect until blank line / structural)
    const para: string[] = [line];
    i++;
    while (
      i < lines.length &&
      !/^\s*$/.test(lines[i]) &&
      !/^(#{1,4}\s|>\s|[-*]\s|\d+\.\s|\|.*\|\s*$|```|---+\s*$)/.test(lines[i])
    ) {
      para.push(lines[i]);
      i++;
    }
    blocks.push({
      kind: 'p',
      inline: renderInline(para.join(' ')),
    });
  }
  return blocks;
}

function renderBlock(b: Block, idx: number): ReactNode {
  switch (b.kind) {
    case 'h1':
      return <h1 key={idx} id={slugify(b.text!)}>{b.inline}</h1>;
    case 'h2':
      return <h2 key={idx} id={slugify(b.text!)}>{b.inline}</h2>;
    case 'h3':
      return <h3 key={idx} id={slugify(b.text!)}>{b.inline}</h3>;
    case 'h4':
      return <h4 key={idx} id={slugify(b.text!)}>{b.inline}</h4>;
    case 'p':
      return <p key={idx}>{b.inline}</p>;
    case 'quote':
      return (
        <blockquote key={idx} className="md-quote">
          {b.inline}
        </blockquote>
      );
    case 'hr':
      return <hr key={idx} className="md-hr" />;
    case 'ul':
      return (
        <ul key={idx}>
          {b.items!.map((it, j) => (
            <li key={j}>{renderInline(it[0])}</li>
          ))}
        </ul>
      );
    case 'ol':
      return (
        <ol key={idx}>
          {b.items!.map((it, j) => (
            <li key={j}>{renderInline(it[0])}</li>
          ))}
        </ol>
      );
    case 'code':
      return (
        <pre key={idx} className="md-pre">
          <code>{b.text}</code>
        </pre>
      );
    case 'table': {
      const [header, ...rows] = b.items!;
      return (
        <div key={idx} className="md-table-wrap">
          <table className="md-table">
            <thead>
              <tr>
                {header.map((c, j) => (
                  <th key={j}>{renderInline(c)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, j) => (
                <tr key={j}>
                  {r.map((c, k) => (
                    <td key={k}>{renderInline(c)}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    }
    default:
      return null;
  }
}

/** Render a markdown string to React nodes. */
export function renderMarkdown(src: string): ReactNode {
  const blocks = parse(src);
  return (
    <div className="md-root">
      {blocks.map((b, i) => renderBlock(b, i))}
    </div>
  );
}
