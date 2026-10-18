import { expect, it } from "vitest";
import { htmlToText } from "../src/tools";

it("turns HTML into readable text with link targets", () => {
  expect(
    htmlToText(
      `<html><head><title>T</title><style>p{}</style></head><body><script>x()</script><h1>Jobs &amp; more</h1><p>Go dev at <a href="https://e.test/1">Acme</a></p><!-- c --><li>&#39;Remote&#x27;</li></body></html>`,
    ),
  ).toBe("Jobs & more\nGo dev at Acme (https://e.test/1)\n'Remote'");
});

it("reads Atom feeds, keeping entry links and unescaping their HTML content", () => {
  const feed = `<?xml version="1.0"?><feed><entry><title>v2.0</title><link rel="alternate" href="https://e.test/v2"/><content type="html">&lt;p&gt;Adds &lt;b&gt;tools&lt;/b&gt;&lt;/p&gt;</content></entry></feed>`;
  expect(htmlToText(htmlToText(feed))).toBe("v2.0\nhttps://e.test/v2\nAdds tools");
});
