// @vitest-environment happy-dom
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as vComponents from 'vuetify/components'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/en.json'
import type { ViewResult } from '@/types/views'
import ViewTable from './ViewTable.vue'

const row = (series: string, score: number) => ({
  key: { series }, cells: [{ value: score, text: `${score}%` }], updated_at: 1, empty: false,
})
const result: ViewResult = {
  name: 'v', title: 'v', generated_at: 1, sources: [], extra: [], row_keys: ['series'],
  columns: [{ name: 'Score', better: 'higher' }],
  rows: [row('原模型 JA-plain | 准确率', 52), row('A1 JA-plain | 日文交付率', 90), row('A1 EN-plain | 准确率', 47)],
}

function render() {
  return mount(ViewTable, {
    props: { result },
    global: { plugins: [createVuetify({ components: vComponents }), createI18n({ legacy: false, locale: 'en', messages: { en } })] },
  })
}

describe('ViewTable', () => {
  it('puts the parts of a "a | b" key in their own sub-columns', () => {
    const w = render()
    expect(w.find('th.vt-key').attributes('colspan')).toBe('2')
    const first = w.findAll('tbody tr')[0].findAll('td.vt-part')
    expect(first.map((td) => td.text())).toEqual(['原模型 JA-plain', '|准确率'])
    expect(first[1].find('.vt-bar').exists()).toBe(true)
  })

  it('sorts by a value column: high to low, low to high, then the view order', async () => {
    const w = render()
    const scores = () => w.findAll('tbody td.vt-num').map((td) => td.text())
    const header = w.find('.vt-sort')
    await header.trigger('click')
    expect(scores()).toEqual(['90%', '52%', '47%'])
    await header.trigger('click')
    expect(scores()).toEqual(['47%', '52%', '90%'])
    await header.trigger('click')
    expect(scores()).toEqual(['52%', '90%', '47%'])
  })
})
