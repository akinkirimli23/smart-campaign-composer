<script setup lang="ts">
// Picks values from a closed set too long to show as a row of checkboxes
// (the 81 provinces): what is chosen stays visible as removable chips, the
// rest is found by typing.
import { computed, ref } from 'vue'
import type { Option } from '../api/types'
import { searchOptions } from '../lib/rule'

const props = defineProps<{
  /** Part of the editor's draft: edited in place. */
  values: string[]
  options: Option[]
  /** What is being picked, for the labels: "şehir". */
  noun: string
}>()

/** How many options are offered before anything is typed. */
const SUGGESTED = 15

const query = ref('')
const chosen = computed(() => props.options.filter((o) => props.values.includes(o.value)))
const matches = computed(() => searchOptions(props.options, query.value, props.values))
const shown = computed(() => (query.value.trim() ? matches.value : matches.value.slice(0, SUGGESTED)))
const hidden = computed(() => matches.value.length - shown.value.length)

function add(value: string) {
  if (!props.values.includes(value)) props.values.push(value)
  query.value = ''
}

function remove(value: string) {
  const at = props.values.indexOf(value)
  if (at >= 0) props.values.splice(at, 1)
}

// Enter picks the first match, so a city is two keystrokes and a return.
function pickFirst() {
  const first = shown.value[0]
  if (query.value.trim() && first) add(first.value)
}
</script>

<template>
  <div class="picker">
    <div v-if="chosen.length" class="picked">
      <button v-for="o in chosen" :key="o.value" class="picked-chip" type="button" :aria-label="o.label + ' seçimini kaldır'" @click="remove(o.value)">
        {{ o.label }} <span aria-hidden="true">×</span>
      </button>
    </div>
    <p v-else class="picker-empty">Henüz {{ noun }} seçilmedi.</p>

    <input
      v-model="query"
      class="picker-search"
      type="search"
      :placeholder="`${options.length} ${noun} içinde ara`"
      :aria-label="noun + ' ara'"
      @keydown.enter.prevent="pickFirst"
    />

    <div class="picker-list">
      <button v-for="o in shown" :key="o.value" class="picker-option" type="button" @click="add(o.value)">+ {{ o.label }}</button>
      <span v-if="!shown.length" class="picker-none">{{ query.trim() ? 'Eşleşen ' + noun + ' yok.' : 'Hepsi seçildi.' }}</span>
      <span v-else-if="hidden > 0" class="picker-none">ve {{ hidden }} {{ noun }} daha: bulmak için yaz.</span>
    </div>
  </div>
</template>
