import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: './tests/update',
  outputDir: '../tmp/test-artifacts/update',
  workers: 1,
  retries: 0,
  timeout: 600000,
  forbidOnly: Boolean(process.env.CI),
  reporter: 'list',
})
