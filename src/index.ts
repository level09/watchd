import { agents } from "../agents";
import { cron } from "./cron";
import { runAgent } from "./run";
import type { Env } from "./types";
import { handle } from "./web";

// Validated at startup so a bad agent fails the deploy, not a 3am run.
const names = new Set<string>();
const schedules = agents.map((agent) => {
  if (!/^[\w-]+$/.test(agent.name) || names.has(agent.name))
    throw new Error(`invalid or duplicate agent name "${agent.name}"`);
  if (!(agent.budget > 0)) throw new Error(`agent "${agent.name}" needs a positive budget`);
  names.add(agent.name);
  return { agent, due: cron(agent.schedule) };
});

export default {
  async scheduled(controller, env) {
    const at = new Date(controller.scheduledTime);
    await Promise.all(
      schedules
        .filter((s) => s.due(at))
        .map((s) => runAgent(env, s.agent, controller.scheduledTime)),
    );
  },

  // Dashboard at /, public reports at /p/<agent>, POST /run/<agent> with a session or bearer token.
  fetch: (request, env) => handle(request, env, agents),
} satisfies ExportedHandler<Env>;
