import type { D1Migration } from "cloudflare:test";
import type { Env as WorkerEnv } from "../src/types";
declare module "cloudflare:workers" {
  namespace Cloudflare {
    interface Env extends WorkerEnv {
      TEST_MIGRATIONS: D1Migration[];
    }
  }
}
