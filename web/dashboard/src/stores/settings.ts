import { defineStore } from 'pinia'
import { ref } from 'vue'
export const useSettingsStore = defineStore('settings', () => {
  const theme = ref(localStorage.getItem('runq-theme') || 'light')
  const locale = ref(localStorage.getItem('runq-locale') || 'en')
  const animeMode = ref(localStorage.getItem('runq-anime') === 'true')
  // RQ-75: "don't ask again" for the cross-generation rerun dialog.
  const skipGenRerunPrompt = ref(localStorage.getItem('runq-skip-gen-rerun') === 'true')

  function setTheme(t: string) {
    theme.value = t
    localStorage.setItem('runq-theme', t)
  }

  function setLocale(l: string) {
    locale.value = l
    localStorage.setItem('runq-locale', l)
  }

  function setAnimeMode(v: boolean) {
    animeMode.value = v
    localStorage.setItem('runq-anime', String(v))
  }

  function setSkipGenRerunPrompt(v: boolean) {
    skipGenRerunPrompt.value = v
    localStorage.setItem('runq-skip-gen-rerun', String(v))
  }

  return { theme, locale, animeMode, skipGenRerunPrompt, setTheme, setLocale, setAnimeMode, setSkipGenRerunPrompt }
})
