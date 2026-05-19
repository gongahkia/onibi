import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  timeout: 60_000,
  expect: {
    timeout: 10_000
  },
  use: {
    baseURL: "http://127.0.0.1:5173",
    trace: "on-first-retry"
  },
  webServer: [
    {
      command:
        'pnpm --filter @kelpclaw/workflow-spec build && pnpm --filter @kelpclaw/nanoclaw build && pnpm --filter @kelpclaw/api build && KELPCLAW_ADMIN_TOKEN=openclaw-e2e KELPCLAW_SECRET_STORE=memory KELPCLAW_SECRET_GOOGLE_OAUTH_DEFAULT=\'{"accessToken":"test-google"}\' KELPCLAW_SECRET_EMAIL_SMTP_DEFAULT=\'{"host":"smtp.test"}\' KELPCLAW_SECRET_WHATSAPP_CLOUD_DEFAULT=\'{"accessToken":"test-whatsapp","phoneNumberId":"phone-1"}\' KELPCLAW_SECRET_TELEGRAM_BOT_DEFAULT=\'{"botToken":"test-telegram","chatId":"ops"}\' NANOCLAW_RUNNER=mock PORT=8787 pnpm --filter @kelpclaw/api start',
      url: "http://127.0.0.1:8787/health",
      reuseExistingServer: !process.env.CI,
      timeout: 120_000
    },
    {
      command:
        "VITE_OPENCLAW_ADMIN_TOKEN=openclaw-e2e pnpm --filter @kelpclaw/openclaw dev -- --host 127.0.0.1 --port 5173",
      url: "http://127.0.0.1:5173",
      reuseExistingServer: !process.env.CI,
      timeout: 120_000
    }
  ],
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 920 }
      }
    }
  ]
});
