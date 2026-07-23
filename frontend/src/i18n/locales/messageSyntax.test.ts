import assert from 'node:assert/strict'
import test from 'node:test'

import { createI18n } from 'vue-i18n'

import enUS from './en-US.ts'
import koKR from './ko-KR.ts'
import ruRU from './ru-RU.ts'
import zhCN from './zh-CN.ts'

const localeChecks = [
  { name: 'zh-CN', messages: zhCN },
  { name: 'en-US', messages: enUS },
  { name: 'ko-KR', messages: koKR },
  { name: 'ru-RU', messages: ruRU },
]

test('Git repository URL hints compile and render the SCP-style SSH example literally', () => {
  for (const check of localeChecks) {
    const i18n = createI18n({
      legacy: false,
      locale: check.name,
      messages: { [check.name]: check.messages },
    })

    const hint = i18n.global.t('datasource.field.gitRepoUrlHint')

    assert.match(hint, /user@host:path/, `${check.name} did not render the SSH URL literally`)
  }
})
