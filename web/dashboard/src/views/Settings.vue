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
          :variant="currentTheme === th.value ? 'tonal' : 'flat'"
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
          :variant="currentLocale === lang.value ? 'tonal' : 'flat'"
          @click="currentLocale = lang.value"
        >
          <div class="text-body-2">{{ lang.flag }}</div>
          <div class="text-caption">{{ lang.label }}</div>
        </v-card>
      </div>

      <!-- Appearance levers (RQ2-2): density / surface / accent. State +
           persistence (localStorage) live in
           useAppearance; these are plain selectors. -->
      <div class="text-caption text-on-surface-variant mb-2">{{ t('settings.density') }}</div>
      <div class="d-flex ga-2 mb-5">
        <v-card
          v-for="d in densities"
          :key="d.value"
          class="pa-3 text-center flex-grow-1 cursor-pointer"
          :color="appearance.density.value === d.value ? 'primary' : 'surface-variant'"
          :variant="appearance.density.value === d.value ? 'tonal' : 'flat'"
          @click="appearance.density.value = d.value"
        >
          <v-icon size="20" class="mb-1">{{ d.icon }}</v-icon>
          <div class="text-caption">{{ d.label }}</div>
        </v-card>
      </div>

      <div class="text-caption text-on-surface-variant mb-2">{{ t('settings.surface') }}</div>
      <div class="d-flex ga-2 mb-5">
        <v-card
          v-for="s in surfaces"
          :key="s.value"
          class="pa-3 text-center flex-grow-1 cursor-pointer"
          :color="appearance.surface.value === s.value ? 'primary' : 'surface-variant'"
          :variant="appearance.surface.value === s.value ? 'tonal' : 'flat'"
          @click="appearance.surface.value = s.value"
        >
          <v-icon size="20" class="mb-1">{{ s.icon }}</v-icon>
          <div class="text-caption">{{ s.label }}</div>
        </v-card>
      </div>

      <div class="text-caption text-on-surface-variant mb-2">{{ t('settings.accent') }}</div>
      <div class="d-flex ga-2 mb-5">
        <v-card
          v-for="(a, hex) in ACCENTS"
          :key="hex"
          class="pa-3 text-center flex-grow-1 cursor-pointer"
          :color="appearance.accent.value === hex ? 'primary' : 'surface-variant'"
          :variant="appearance.accent.value === hex ? 'tonal' : 'flat'"
          @click="appearance.accent.value = hex"
        >
          <span
            class="accent-swatch mb-1"
            :style="{ background: currentTheme === 'dark' ? a.dark : hex }"
          />
          <div class="text-caption">{{ a.name }}</div>
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
                  :variant="!settings.animeMode ? 'tonal' : 'flat'"
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
                  :variant="settings.animeMode ? 'tonal' : 'flat'"
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
import { useAppearance, ACCENTS, type Density, type Surface } from '@/composables/useAppearance'
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

const surfaces = computed((): Array<{ value: Surface; label: string; icon: string }> => [
  { value: 'hairline', label: t('settings.surface_hairline'), icon: 'mdi-square-outline' },
  { value: 'elevated', label: t('settings.surface_elevated'), icon: 'mdi-checkbox-multiple-blank-outline' },
  { value: 'grid', label: t('settings.surface_grid'), icon: 'mdi-grid' },
])

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
.accent-swatch {
  display: inline-block;
  width: 20px;
  height: 20px;
  border-radius: 50%;
}
</style>
