export interface EmailSender {
  send(message: {
    to: string;
    from: { email: string; name: string };
    subject: string;
    html: string;
    text: string;
  }): Promise<{ messageId: string }>;
}

export interface Env {
  DB: D1Database;
  EMAIL: EmailSender;
  OPENROUTER_API_KEY: string;
  RUN_TOKEN: string;
  DEFAULT_MODEL: string;
  DAILY_CAP_USD: string;
  EMAIL_FROM?: string;
  NOTIFY_EMAIL?: string;
}

export interface Agent {
  name: string;
  /** Five-field cron in UTC, e.g. "0 7 * * 1" for Mondays 07:00. */
  schedule: string;
  /** USD reserved per run against the daily cap. */
  budget: number;
  prompt: string;
  /** OpenRouter model slug; append ":online" for web search. */
  model?: string;
  maxTokens?: number;
  /** URLs fetched each run and passed to the model as untrusted data. */
  sources?: string[];
  /** Carry a notes file between runs. */
  memory?: boolean;
  /** Publish successful reports at /p/<name>. Costs and errors stay private. */
  public?: boolean;
}
