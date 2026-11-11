<script setup lang="ts">
import { computed, ref } from 'vue'
import type { FieldError, Rule, RuleOptions } from '../api/types'
import {
  addableKinds,
  cloneRule,
  defaultCondition,
  errorsFor,
  generalErrors,
  kindOf,
  KIND_LABELS,
  normaliseRule,
  sameRule,
  valuesOf,
  type ConditionKind,
} from '../lib/rule'

const props = defineProps<{
  rule: Rule
  options: RuleOptions
  busy: boolean
  /** Validation errors of the last apply, from the backend. */
  errors: FieldError[]
}>()

const emit = defineEmits<{
  apply: [rule: Rule]
  cancel: []
}>()

// The draft is a copy: nothing changes for real until the backend accepts it.
const draft = ref<Rule>(cloneRule(props.rule))
const limits = computed(() => props.options.limits)
const dirty = computed(() => !sameRule(draft.value, props.rule))
const canAdd = computed(() => addableKinds(draft.value, props.options))
const newKind = ref<ConditionKind | ''>('')

function add() {
  if (newKind.value === '') return
  draft.value.conditions.push(defaultCondition(newKind.value, props.options))
  newKind.value = ''
}

function remove(index: number) {
  draft.value.conditions.splice(index, 1)
}

function toggle(values: string[], value: string) {
  const at = values.indexOf(value)
  if (at >= 0) values.splice(at, 1)
  else values.push(value)
}

function apply() {
  emit('apply', normaliseRule(draft.value))
}
</script>

<template>
  <!-- novalidate: min / max on the inputs guide the spinner buttons, but the
       browser's own blocking tooltips are skipped so every problem is reported
       the same way, by the backend, under the row it belongs to. -->
  <form class="rule-editor" novalidate @submit.prevent="apply">
    <div class="field">
      <label for="rule-match">Kullanıcı şunları sağlamalı</label>
      <select id="rule-match" v-model="draft.match">
        <option value="all">Koşulların hepsini</option>
        <option value="any">Koşullardan en az birini</option>
      </select>
    </div>

    <p v-if="draft.conditions.length === 0" class="hint">Henüz koşul yok. En az bir koşul gerekli.</p>

    <fieldset v-for="(c, i) in draft.conditions" :key="i" class="condition">
      <legend>{{ KIND_LABELS[kindOf(c)] }}</legend>

      <!-- City, language, gender: pick from the closed set. -->
      <template v-if="c.type === 'attribute' && c.field !== 'age'">
        <div class="inline">
          <select v-model="c.op" :aria-label="KIND_LABELS[c.field] + ' koşulu türü'">
            <option value="in">Şunlardan biri</option>
            <option value="not_in">Şunların dışında</option>
          </select>
        </div>
        <div class="choices">
          <label v-for="o in valuesOf(c.field, options)" :key="o.value" class="choice">
            <input type="checkbox" :checked="c.values.includes(o.value)" @change="toggle(c.values, o.value)" />
            {{ o.label }}
          </label>
        </div>
      </template>

      <template v-else-if="c.type === 'attribute'">
        <div class="inline">
          <label>En az <input v-model.number="c.min" type="number" :min="limits.min_age" :max="limits.max_age" /></label>
          <label>En çok <input v-model.number="c.max" type="number" :min="limits.min_age" :max="limits.max_age" /> yaş</label>
        </div>
      </template>

      <template v-else-if="c.type === 'event'">
        <div class="inline">
          <label>Son <input v-model.number="c.within_days" type="number" :min="limits.min_within_days" :max="limits.max_within_days" /> günde</label>
          <select v-model="c.event" aria-label="Davranış">
            <option v-for="o in options.events" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
          <select v-model="c.exists" aria-label="Yaptı ya da yapmadı">
            <option :value="true">yaptı</option>
            <option :value="false">hiç yapmadı</option>
          </select>
        </div>
        <div class="inline">
          <label>
            Kategori
            <select v-model="c.category">
              <option :value="null">Tümü</option>
              <option v-for="o in options.categories" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </label>
          <label v-if="c.exists">
            En az
            <input v-model.number="c.min_count" type="number" min="1" :max="limits.max_min_count" placeholder="1" />
            kez
          </label>
        </div>
      </template>

      <template v-else-if="c.type === 'signup'">
        <div class="inline">
          <select v-model="c.op" aria-label="Üyelik tarihi koşulu türü">
            <option value="within">Son … günde üye oldu</option>
            <option value="older_than">… günden daha önce üye oldu</option>
          </select>
          <label><input v-model.number="c.days" type="number" min="1" :max="limits.max_signup_days" /> gün</label>
        </div>
      </template>

      <template v-else>
        <div class="inline">
          <select v-model="c.metric" aria-label="Metrik">
            <option v-for="o in options.metrics" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
          <select v-model="c.op" aria-label="Karşılaştırma">
            <option value="gte">en az</option>
            <option value="lte">en fazla</option>
          </select>
          <input v-model.number="c.value" type="number" min="0" aria-label="Değer" />
        </div>
        <div class="inline">
          <label>
            Dönem
            <select
              :value="c.within_days === null ? 'all' : 'window'"
              @change="c.within_days = ($event.target as HTMLSelectElement).value === 'all' ? null : 30"
            >
              <option value="all">Tüm zamanlar</option>
              <option value="window">Son … gün</option>
            </select>
          </label>
          <label v-if="c.within_days !== null">
            <input v-model.number="c.within_days" type="number" :min="limits.min_within_days" :max="limits.max_within_days" /> gün
          </label>
        </div>
      </template>

      <p v-for="message in errorsFor(errors, i)" :key="message" class="field-error">{{ message }}</p>

      <button class="ghost danger" type="button" @click="remove(i)">Koşulu kaldır</button>
    </fieldset>

    <div v-if="canAdd.length" class="inline add">
      <label for="rule-add">Koşul ekle</label>
      <select id="rule-add" v-model="newKind" @change="add">
        <option value="" disabled>Seç…</option>
        <option v-for="kind in canAdd" :key="kind" :value="kind">{{ KIND_LABELS[kind] }}</option>
      </select>
    </div>

    <div v-if="generalErrors(errors).length" class="alert error" role="alert">
      <ul>
        <li v-for="(e, i) in generalErrors(errors)" :key="i">{{ e.message }}</li>
      </ul>
    </div>

    <div class="actions">
      <button type="button" :disabled="busy" @click="emit('cancel')">Vazgeç</button>
      <button class="primary" type="submit" :disabled="busy || !dirty">
        {{ busy ? 'Doğrulanıyor…' : 'Uygula ve yeniden say' }}
      </button>
    </div>
  </form>
</template>
