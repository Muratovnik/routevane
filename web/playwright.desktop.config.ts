import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests/desktop',
  outputDir: '../tmp/test-artifacts/desktop',
  workers: 1,
  retries: 0,
  timeout: 60000,
  forbidOnly: Boolean(process.env.CI),
  reporter: 'list',
})
