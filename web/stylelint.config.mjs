export default {
  extends: ['stylelint-config-standard', 'stylelint-config-standard-vue'],
  ignoreFiles: ['.nuxt/**', '.output/**', 'dist/**', 'node_modules/**'],
  rules: {
    'custom-property-pattern': '^rv-[a-z0-9-]+$',
    'selector-class-pattern': '^[a-z][a-z0-9]*(?:(?:--|__|-)[a-z0-9]+)*$',
    'value-keyword-case': null,
  },
}
