<script setup lang="ts">
// The form of one plain condition. Used for a rule's own conditions and for
// the members of a group, so both are edited the same way.
import { computed } from 'vue'
import type { PlainCondition, RuleOptions } from '../api/types'
import { kindOf, KIND_LABELS, valuesOf } from '../lib/rule'
import OptionPicker from './OptionPicker.vue'

const props = defineProps<{
  /** Part of the editor's draft: edited in place. */
  condition: PlainCondition
  options: RuleOptions
  /** What the backend said about this condition on the last apply. */
  errors: string[]
}>()

defineEmits<{ remove: [] }>()

// The draft object itself; the parent owns it and reads the edits back.
const c = computed(() => props.condition)
const limits = computed(() => props.options.limits)

/** Sets longer than this are picked by searching instead of checkboxes. */
const PICKER_FROM = 12

function toggle(values: string[], value: string) {
  const at = values.indexOf(value)
  if (at >= 0) values.splice(at, 1)
  else values.push(value)
}
</script>

<template>
  <fieldset class="condition">
    <legend>{{ KIND_LABELS[kindOf(c)] }}</legend>

    <!-- City, language, gender: pick from the closed set. -->
    <template v-if="c.type === 'attribute' && c.field !== 'age'">
      <div class="inline">
        <select v-model="c.op" :aria-label="KIND_LABELS[c.field] + ' koşulu türü'">
          <option value="in">Şunlardan biri</option>
          <option value="not_in">Şunların dışında</option>
        </select>
      </div>
      <!-- A long set (the provinces) is searched, a short one is shown whole. -->
      <OptionPicker
        v-if="valuesOf(c.field, options).length > PICKER_FROM"
        :values="c.values"
        :options="valuesOf(c.field, options)"
        :noun="c.field === 'city' ? 'şehir' : 'değer'"
      />
      <div v-else class="choices">
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
        <label v-if="c.exists">
          En fazla
          <input v-model.number="c.max_count" type="number" min="1" :max="limits.max_min_count" placeholder="∞" />
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
        <label>
          Kategori
          <select v-model="c.category">
            <option :value="null">Tümü</option>
            <option v-for="o in options.categories" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
        </label>
      </div>
      <p v-if="c.metric === 'avg_order_value'" class="field-hint">Hiç alışveriş yapmamış kullanıcıların ortalaması yoktur; bu koşula girmezler.</p>
    </template>

    <p v-for="message in errors" :key="message" class="field-error">{{ message }}</p>

    <button class="ghost danger" type="button" @click="$emit('remove')">Koşulu kaldır</button>
  </fieldset>
</template>
