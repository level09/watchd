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
  BROWSER: BrowserRun;
  OPENROUTER_API_KEY: string;
  RUN_TOKEN: string;
  DEFAULT_MODEL: string;
  DAILY_CAP_USD: string;
  EMAIL_FROM?: string;
  NOTIFY_EMAIL?: string;
}
