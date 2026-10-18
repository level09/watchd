import { expect, it } from "vitest";
import { markdown } from "../src/markdown";

it("renders the report subset", () => {
  expect(markdown("**1. Title**\nApplies to: `x`\nhttps://e.test/a\n\n- one\n- two\n\n## Next")).toBe(
    '<p><strong>1. Title</strong><br>Applies to: <code>x</code><br><a href="https://e.test/a" rel="noreferrer">https://e.test/a</a></p>' +
      "<ul><li>one</li><li>two</li></ul><h4>Next</h4>",
  );
  expect(markdown("[docs](https://e.test/d)")).toBe('<p><a href="https://e.test/d" rel="noreferrer">docs</a></p>');
});

it("never passes model HTML through", () => {
  const html = markdown('<script>alert(1)</script> [x](javascript:alert(1)) <img src=x onerror="y">');
  expect(html).not.toMatch(/<script|<img|href="javascript/);
});
