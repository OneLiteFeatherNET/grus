import tailwindcss from '@tailwindcss/vite'

// Nuxt on the node-server preset: Nitro runs the BFF in front of the Grus API. Everything that differs between
// deployments is read at runtime from the environment (NUXT_GRUS_API_URL, NUXT_GRUS_TOKEN), never at build time,
// so one image runs in every environment.
export default defineNuxtConfig({
  compatibilityDate: '2026-10-01',
  devtools: { enabled: false },
  telemetry: false,

  // Area components keep their own names (PoolTable, not PoolsPoolTable); m3/ keeps its M3 prefix.
  components: [
    { path: '~/components/pools', pathPrefix: false },
    { path: '~/components/templates', pathPrefix: false },
    '~/components',
  ],

  modules: [
    '@nuxt/eslint',
    '@nuxtjs/i18n',
  ],

  runtimeConfig: {
    // Base URL of the Grus controller, without the /v1 prefix. Set with NUXT_GRUS_API_URL.
    grusApiUrl: 'http://localhost:8080',
    // Admin bearer token, only read on the server and only ever sent upstream. Set with NUXT_GRUS_TOKEN.
    grusToken: '',
  },

  app: {
    head: {
      title: 'Grus',
      htmlAttrs: { lang: 'de' },
      meta: [
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
        { name: 'robots', content: 'noindex, nofollow' },
      ],
      link: [{ rel: 'icon', type: 'image/png', href: '/logo.png' }],
    },
  },

  css: [
    '@fontsource/roboto/400.css',
    '@fontsource/roboto/500.css',
    '@fontsource/roboto/700.css',
    '~/assets/css/main.css',
  ],

  vite: {
    plugins: [tailwindcss()],
  },

  i18n: {
    strategy: 'no_prefix',
    defaultLocale: 'de',
    locales: [
      {
        code: 'de',
        language: 'de-DE',
        name: 'Deutsch',
        files: [
          'de/common.json',
          'de/overview.json',
          'de/pools.json',
          'de/instances.json',
          'de/templates.json',
          'de/events.json',
        ],
      },
      {
        code: 'en',
        language: 'en-US',
        name: 'English',
        files: [
          'en/common.json',
          'en/overview.json',
          'en/pools.json',
          'en/instances.json',
          'en/templates.json',
          'en/events.json',
        ],
      },
    ],
    detectBrowserLanguage: {
      useCookie: true,
      cookieKey: 'grus_locale',
      fallbackLocale: 'de',
      redirectOn: 'root',
    },
  },

  eslint: { config: { stylistic: false } },

  typescript: { strict: true, typeCheck: false },

  nitro: { preset: 'node-server' },
})
