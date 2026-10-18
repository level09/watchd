// Five-field cron (minute hour day-of-month month day-of-week), evaluated in UTC.
const RANGES = [[0, 59], [0, 23], [1, 31], [1, 12], [0, 7]] as const;

function field(spec: string, [lo, hi]: readonly [number, number]): Set<number> {
  const values = new Set<number>();
  for (const part of spec.split(",")) {
    const m = /^(?:(\*)|(\d+)(?:-(\d+))?)(?:\/(\d+))?$/.exec(part);
    if (!m) throw new Error(`invalid cron field "${spec}"`);
    const start = m[1] ? lo : Number(m[2]);
    const end = m[1] ? hi : m[3] ? Number(m[3]) : m[4] ? hi : start;
    const step = m[4] ? Number(m[4]) : 1;
    if (start < lo || end > hi || start > end || step < 1)
      throw new Error(`invalid cron field "${spec}"`);
    for (let v = start; v <= end; v += step) values.add(v);
  }
  return values;
}

export function cron(expr: string): (at: Date) => boolean {
  const specs = expr.trim().split(/\s+/);
  if (specs.length !== 5) throw new Error(`cron needs 5 fields: "${expr}"`);
  const [minute, hour, dom, month, dow] = specs.map((s, i) => field(s, RANGES[i]));
  if (dow.has(7)) dow.add(0);
  // Standard cron: when both day fields are restricted, either may match.
  const both = !specs[2].startsWith("*") && !specs[4].startsWith("*");
  return (at) => {
    const d = dom.has(at.getUTCDate());
    const w = dow.has(at.getUTCDay());
    return (
      minute.has(at.getUTCMinutes()) &&
      hour.has(at.getUTCHours()) &&
      month.has(at.getUTCMonth() + 1) &&
      (both ? d || w : d && w)
    );
  };
}
