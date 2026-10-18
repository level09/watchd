// Small markdown subset for model reports. Input is escaped first, so the model can never emit raw HTML.
export const esc = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);

const inline = (s: string) =>
  esc(s)
    .replace(/`([^`]+)`/g, "<code>$1</code>")
    .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
    .replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" rel="noreferrer">$1</a>')
    .replace(/(^|[\s(])(https?:\/\/[^\s<>"')\]]+)/g, '$1<a href="$2" rel="noreferrer">$2</a>');

export function markdown(src: string): string {
  const out: string[] = [];
  let para: string[] = [];
  let list: "ul" | "ol" | null = null;
  const endPara = () => {
    if (para.length) out.push(`<p>${para.join("<br>")}</p>`);
    para = [];
  };
  const endList = () => {
    if (list) out.push(`</${list}>`);
    list = null;
  };
  for (const line of src.split("\n")) {
    const text = line.trim();
    const heading = /^(#{1,3})\s+(.*)$/.exec(text);
    const item = /^(?:([-*])|\d+[.)])\s+(.*)$/.exec(text);
    if (!text || /^-{3,}$/.test(text)) {
      endPara();
      endList();
    } else if (heading) {
      endPara();
      endList();
      const level = heading[1].length + 2;
      out.push(`<h${level}>${inline(heading[2])}</h${level}>`);
    } else if (item) {
      endPara();
      const kind = item[1] ? "ul" : "ol";
      if (list !== kind) {
        endList();
        out.push(`<${kind}>`);
        list = kind;
      }
      out.push(`<li>${inline(item[2])}</li>`);
    } else {
      endList();
      para.push(inline(text));
    }
  }
  endPara();
  endList();
  return out.join("");
}
