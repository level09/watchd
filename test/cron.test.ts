import { expect, it } from "vitest";
import { cron } from "../src/cron";

const at = (iso: string) => new Date(`${iso}Z`);

it("matches minutes, hours, steps, ranges and lists", () => {
  expect(cron("0 7 * * *")(at("2026-10-19T07:00"))).toBe(true);
  expect(cron("0 7 * * *")(at("2026-10-19T07:01"))).toBe(false);
  expect(cron("*/15 * * * *")(at("2026-10-19T03:45"))).toBe(true);
  expect(cron("*/15 * * * *")(at("2026-10-19T03:46"))).toBe(false);
  expect(cron("0 9-17/4 * * *")(at("2026-10-19T13:00"))).toBe(true);
  expect(cron("0 9-17/4 * * *")(at("2026-10-19T15:00"))).toBe(false);
  expect(cron("0 0 1,15 * *")(at("2026-10-15T00:00"))).toBe(true);
});

it("treats 0 and 7 as Sunday and ORs restricted day fields", () => {
  expect(cron("0 7 * * 1")(at("2026-10-19T07:00"))).toBe(true); // Monday
  expect(cron("0 7 * * 7")(at("2026-10-18T07:00"))).toBe(true); // Sunday
  expect(cron("0 0 1 * 1")(at("2026-10-19T00:00"))).toBe(true); // Monday, not the 1st
  expect(cron("0 0 1 * *")(at("2026-10-19T00:00"))).toBe(false);
});

it("rejects malformed expressions", () => {
  for (const bad of ["* * * *", "60 * * * *", "5-1 * * * *", "*/0 * * * *", "a * * * *"])
    expect(() => cron(bad)).toThrow();
});
