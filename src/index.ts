import { tick } from "./run";
import type { Env } from "./types";
import { handle } from "./web";

export default {
  scheduled: (controller, env) => tick(env, new Date(controller.scheduledTime)),
  // Dashboard, public pages, POST /run/<agent> and /mcp; see web.ts.
  fetch: (request, env) => handle(request, env),
} satisfies ExportedHandler<Env>;
