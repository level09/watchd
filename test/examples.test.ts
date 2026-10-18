import { expect, it } from "vitest";
import { AgentInput } from "../src/agents";

const examples = import.meta.glob("../examples/*.json", { eager: true, import: "default" });

it("every example agent is a valid save_agent input", () => {
  expect(Object.keys(examples).length).toBeGreaterThan(0);
  for (const [path, a] of Object.entries(examples)) expect(AgentInput.safeParse(a).success, path).toBe(true);
});
