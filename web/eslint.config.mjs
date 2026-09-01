import withNuxt from './.nuxt/eslint.config.mjs'
import eslintConfigPrettier from 'eslint-config-prettier'
import security from 'eslint-plugin-security'
import sonarjs from 'eslint-plugin-sonarjs'
import vuejsAccessibility from 'eslint-plugin-vuejs-accessibility'

export default withNuxt(
  {
    name: 'routevane/application-rules',
    plugins: {
      security,
      sonarjs,
      'vuejs-accessibility': vuejsAccessibility,
    },
    rules: {
      'security/detect-object-injection': 'off',
      'sonarjs/no-duplicate-string': 'off',
      'vuejs-accessibility/anchor-has-content': 'error',
      'vuejs-accessibility/aria-props': 'error',
      'vuejs-accessibility/aria-role': 'error',
      'vuejs-accessibility/form-control-has-label': 'error',
      'vuejs-accessibility/heading-has-content': 'error',
    },
  },
  eslintConfigPrettier,
)
