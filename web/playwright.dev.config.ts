import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './tests/dev',
  outputDir: '../tmp/test-artifacts/dev-mode',
  workers: 1,
  timeout: 180_000,
  expect: { timeout: 30_000 },
  forbidOnly: Boolean(process.env.CI),
  use: {
    ...devices['Desktop Chrome'],
    locale: 'en-US',
    trace: 'retain-on-failure',
  },
})
