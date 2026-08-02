import pluginVue from 'eslint-plugin-vue'
import {
  defineConfigWithVueTs,
  vueTsConfigs,
} from '@vue/eslint-config-typescript'

/** @type {import('eslint').Linter.Config[]} */
export default defineConfigWithVueTs(
  {
    ignores: ['dist/**', 'node_modules/**', 'wailsjs/**'],
  },
  ...pluginVue.configs['flat/recommended'],
  ...vueTsConfigs.recommended,
  {
    rules: {
      'vue/singleline-html-element-content-newline': 'off',
      'vue/max-attributes-per-line': 'off',
      'vue/html-closing-bracket-newline': 'off',
      'vue/html-indent': 'off',
      'vue/multiline-html-element-content-newline': 'off',
      'vue/html-self-closing': 'off',
      '@typescript-eslint/no-empty-object-type': 'off',
      '@typescript-eslint/no-explicit-any': 'off',
    },
  },
)
