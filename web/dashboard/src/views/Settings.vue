<template>
  <div style="max-width: 640px; margin: 0 auto">
    <div class="text-h5 font-weight-bold mb-6">{{ t('settings.title') }}</div>

    <!-- Appearance -->
    <v-card class="pa-5">
      <div class="text-subtitle-2 mb-4">{{ t('settings.appearance') }}</div>

      <div class="text-caption text-on-surface-variant mb-2">{{ t('settings.theme') }}</div>
      <div class="d-flex ga-2 mb-5">
        <v-card
          v-for="th in themes"
          :key="th.value"
          class="pa-3 text-center flex-grow-1 cursor-pointer"
          :elevation="currentTheme === th.value ? 3 : 0"
          :color="currentTheme === th.value ? 'primary' : 'surface-variant'"
          :variant="currentTheme === th.value ? 'outlined' : 'flat'"
          @click="currentTheme = th.value"
        >
          <v-icon size="20" class="mb-1">{{ th.icon }}</v-icon>
          <div class="text-caption">{{ th.label }}</div>
        </v-card>
      </div>

      <div class="text-caption text-on-surface-variant mb-2">{{ t('settings.language') }}</div>
      <div class="d-flex ga-2 mb-5">
        <v-card
          v-for="lang in locales"
          :key="lang.value"
          class="pa-3 text-center flex-grow-1 cursor-pointer"
          :elevation="currentLocale === lang.value ? 3 : 0"
          :color="currentLocale === lang.value ? 'primary' : 'surface-variant'"
          :variant="currentLocale === lang.value ? 'outlined' : 'flat'"
          @click="currentLocale = lang.value"
        >
          <div class="text-body-2">{{ lang.flag }}</div>
          <div class="text-caption">{{ lang.label }}</div>
        </v-card>
      </div>

      <!-- Appearance levers: palette / density. State + persistence
           (localStorage) live in useAppearance; these are plain selectors. -->
      <div class="text-caption text-on-surface-variant mb-2">{{ t('settings.palette') }}</div>
      <div class="d-flex ga-2 mb-5">
        <v-card
          v-for="p in palettes"
          :key="p.value"
          class="pa-3 text-center flex-grow-1 cursor-pointer"
          :color="appearance.palette.value === p.value ? 'primary' : 'surface-variant'"
          :variant="appearance.palette.value === p.value ? 'outlined' : 'flat'"
          :aria-pressed="appearance.palette.value === p.value"
          role="button"
          @click="appearance.palette.value = p.value"
        >
          <span class="palette-swatch mb-1" :style="{ background: p.paper }" aria-hidden="true">
            <span v-for="c in p.marks" :key="c" class="palette-mark" :style="{ background: c }" />
          </span>
          <div class="text-caption">{{ p.label }}</div>
        </v-card>
      </div>

      <div class="text-caption text-on-surface-variant mb-2">{{ t('settings.density') }}</div>
      <div class="d-flex ga-2 mb-5">
        <v-card
          v-for="d in densities"
          :key="d.value"
          class="pa-3 text-center flex-grow-1 cursor-pointer"
          :color="appearance.density.value === d.value ? 'primary' : 'surface-variant'"
          :variant="appearance.density.value === d.value ? 'outlined' : 'flat'"
          @click="appearance.density.value = d.value"
        >
          <v-icon size="20" class="mb-1">{{ d.icon }}</v-icon>
          <div class="text-caption">{{ d.label }}</div>
        </v-card>
      </div>

      <!-- Experimental (anime mode tucked away from the default view) -->
      <v-expansion-panels variant="accordion" class="mt-2">
        <v-expansion-panel>
          <v-expansion-panel-title class="text-caption text-on-surface-variant">
            <v-icon size="14" start>mdi-flask-outline</v-icon>
            {{ t('settings.experimental') }}
          </v-expansion-panel-title>
          <v-expansion-panel-text>
            <div class="d-flex align-center justify-space-between rounded-lg pa-3" style="background: rgb(var(--v-theme-surface-variant), 0.3)">
              <div>
                <div class="text-body-2 font-weight-medium">{{ t('settings.anime_mode') }}</div>
                <div class="text-caption text-on-surface-variant">{{ t('settings.anime_hint') }}</div>
              </div>
              <!-- Logo switch group: pick the face, not a bare toggle — the
                   choice IS which logo the app wears. -->
              <div class="d-flex ga-2">
                <v-card
                  class="pa-2 d-flex align-center justify-center cursor-pointer"
                  :color="!settings.animeMode ? 'primary' : 'surface-variant'"
                  :variant="!settings.animeMode ? 'outlined' : 'flat'"
                  role="button"
                  :aria-pressed="!settings.animeMode"
                  :aria-label="t('settings.anime_mode') + ': off'"
                  @click="settings.setAnimeMode(false)"
                >
                  <RunqLogo :size="32" variant="normal" />
                </v-card>
                <v-card
                  class="pa-2 d-flex align-center justify-center cursor-pointer"
                  :color="settings.animeMode ? 'primary' : 'surface-variant'"
                  :variant="settings.animeMode ? 'outlined' : 'flat'"
                  role="button"
                  :aria-pressed="settings.animeMode"
                  :aria-label="t('settings.anime_mode') + ': on'"
                  @click="settings.setAnimeMode(true)"
                >
                  <RunqLogo :size="32" variant="anime" />
                </v-card>
              </div>
            </div>
          </v-expansion-panel-text>
        </v-expansion-panel>
      </v-expansion-panels>
    </v-card>

    <!-- RQ-74: runq self-logs — deaths the UI can't push still land in
         daemon.log; read it here instead of grepping files. -->
    <DaemonLogPanel />
  </div>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTheme } from 'vuetify'
import { useSettingsStore } from '@/stores/settings'
import { useAppearance, type Density } from '@/composables/useAppearance'
import { PALETTES, PALETTE_DEFS, type Palette } from '@/plugins/palettes'
import DaemonLogPanel from '@/components/DaemonLogPanel.vue'
import RunqLogo from '@/components/RunqLogo.vue'

const { t, locale } = useI18n()
const theme = useTheme()
const settings = useSettingsStore()

const currentTheme = ref(settings.theme)
const currentLocale = ref(settings.locale)

const themes = computed(() => [
  { value: 'light', label: t('settings.theme_light'), icon: 'mdi-white-balance-sunny' },
  { value: 'dark', label: t('settings.theme_dark'), icon: 'mdi-moon-waning-crescent' },
])

const appearance = useAppearance()

const densities = computed((): Array<{ value: Density; label: string; icon: string }> => [
  { value: 'compact', label: t('settings.density_compact'), icon: 'mdi-view-headline' },
  { value: 'regular', label: t('settings.density_regular'), icon: 'mdi-view-day-outline' },
  { value: 'comfy', label: t('settings.density_comfy'), icon: 'mdi-view-agenda-outline' },
])

// Each palette previews itself in the current mode: its paper, ink and the
// three status colours that matter most (failed, running, success).
const palettes = computed(() =>
  PALETTES.map((value: Palette) => {
    const c = PALETTE_DEFS[value][currentTheme.value === 'dark' ? 'dark' : 'light']
    return {
      value,
      label: t(`settings.palette_${value}`),
      paper: c.background,
      marks: [c['on-surface'], c.error, c.info, c.success],
    }
  }),
)

const locales = [
  { value: 'en', label: 'English', flag: 'EN' },
  { value: 'ja', label: '日本語', flag: 'JA' },
  { value: 'zh-CN', label: '中文', flag: 'ZH' },
]

watch(currentTheme, (val) => {
  settings.setTheme(val)
  theme.global.name.value = val
})

watch(currentLocale, (val) => {
  settings.setLocale(val)
  locale.value = val
})

</script>

<style scoped>
.font-mono, .font-mono :deep(input), .font-mono :deep(textarea) { font-family: monospace; font-size: 13px; }
.tmpl-display {
  border: 1px solid rgb(var(--v-theme-outline-variant));
  background: rgb(var(--v-theme-surface-variant), 0.2);
  transition: border-color 0.15s ease, background 0.15s ease;
  min-height: 38px;
}
.tmpl-display:hover {
  border-color: rgb(var(--v-theme-primary));
  background: rgb(var(--v-theme-surface-variant), 0.35);
}
.palette-swatch {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 6px 8px;
  border-radius: var(--radius-sm);
  border: var(--border-hairline) solid rgb(var(--v-theme-outline-variant));
}
.palette-mark {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
</style>
