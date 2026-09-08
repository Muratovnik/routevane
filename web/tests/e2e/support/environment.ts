/** Keep workstation process settings while disabling personal plugin discovery. */
export const isolatedProductEnvironment = (
  parent: NodeJS.ProcessEnv = process.env,
): NodeJS.ProcessEnv => ({ ...parent, ROUTEVANE_PLUGINS_DIR: '' })
