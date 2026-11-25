// @ts-check
import withNuxt from './.nuxt/eslint.config.mjs'

export default withNuxt({
  ignores: ['app/types/grus-api.d.ts'],
  rules: {
    // Pavo's DataTable names its cell slots item.<column>; the dot is part of the slot name, not a modifier.
    'vue/valid-v-slot': ['error', { allowModifiers: true }],
  },
})
