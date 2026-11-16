<script setup lang="ts">
import { computed, ref } from 'vue'
import type { FieldError, GroupCondition, Rule, RuleOptions } from '../api/types'
import {
  addableKinds,
  canAddGroup,
  cloneRule,
  defaultCondition,
  defaultGroup,
  errorsFor,
  generalErrors,
  groupErrors,
  GROUP_LABELS,
  isGroup,
  KIND_LABELS,
  memberErrors,
  normaliseRule,
  opposite,
  sameRule,
  type ConditionKind,
} from '../lib/rule'
import ConditionFields from './ConditionFields.vue'

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
const dirty = computed(() => !sameRule(draft.value, props.rule))
const canAdd = computed(() => addableKinds(draft.value.conditions, draft.value, props.options))
const newKind = ref<ConditionKind | ''>('')

// A group joins its members with the connector the rule does not use, so its
// heading follows the rule's own choice above.
const groupLabel = computed(() => GROUP_LABELS[opposite(draft.value.match)])

function add() {
  if (newKind.value === '') return
  draft.value.conditions.push(defaultCondition(newKind.value, props.options))
  newKind.value = ''
}

function addGroup() {
  draft.value.conditions.push(defaultGroup(draft.value, props.options))
}

function remove(index: number) {
  draft.value.conditions.splice(index, 1)
}

function addMember(group: GroupCondition, event: Event) {
  const select = event.target as HTMLSelectElement
  if (select.value) group.conditions.push(defaultCondition(select.value as ConditionKind, props.options))
  select.value = ''
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
    <label class="everyone">
      <input v-model="draft.everyone" type="checkbox" />
      Tüm kullanıcılara gönder (koşul yok)
    </label>
    <p v-if="draft.everyone" class="alert warn">
      Kampanya, izni ve adresi olan herkese gider. Koşullar uygulanmaz; kutuyu kaldırırsan geri gelirler.
    </p>

    <template v-if="!draft.everyone">
    <div class="field">
      <label for="rule-match">Kullanıcı şunları sağlamalı</label>
      <select id="rule-match" v-model="draft.match">
        <option value="all">Koşulların hepsini</option>
        <option value="any">Koşullardan en az birini</option>
      </select>
    </div>

    <p v-if="draft.conditions.length === 0" class="hint">Henüz koşul yok. En az bir koşul ekle ya da yukarıdan tüm kullanıcıları seç.</p>

    <template v-for="(c, i) in draft.conditions" :key="i">
      <!-- A group: its own box, with the same condition forms inside. -->
      <fieldset v-if="isGroup(c)" class="group">
        <legend>{{ groupLabel }}</legend>
        <p class="hint tight">
          Bu kutudaki koşullar kendi aralarında “{{ draft.match === 'all' ? 'veya' : 've' }}” ile bağlanır; kutu, kuralın tek bir koşulu sayılır.
        </p>

        <ConditionFields
          v-for="(member, j) in c.conditions"
          :key="j"
          :condition="member"
          :options="options"
          :errors="memberErrors(errors, i, j)"
          @remove="c.conditions.splice(j, 1)"
        />

        <p v-for="message in groupErrors(errors, i)" :key="message" class="field-error">{{ message }}</p>

        <div class="inline add">
          <template v-if="addableKinds(c.conditions, draft, options).length">
            <label :for="'group-add-' + i">Bu gruba koşul ekle</label>
            <select :id="'group-add-' + i" @change="addMember(c, $event)">
              <option value="">Seç…</option>
              <option v-for="kind in addableKinds(c.conditions, draft, options)" :key="kind" :value="kind">{{ KIND_LABELS[kind] }}</option>
            </select>
          </template>
          <button class="ghost danger" type="button" @click="remove(i)">Grubu kaldır</button>
        </div>
      </fieldset>

      <ConditionFields v-else :condition="c" :options="options" :errors="errorsFor(errors, i)" @remove="remove(i)" />
    </template>

    <div class="inline add">
      <template v-if="canAdd.length">
        <label for="rule-add">Koşul ekle</label>
        <select id="rule-add" v-model="newKind" @change="add">
          <option value="" disabled>Seç…</option>
          <option v-for="kind in canAdd" :key="kind" :value="kind">{{ KIND_LABELS[kind] }}</option>
        </select>
      </template>
      <button
        v-if="canAddGroup(draft, options)"
        class="small"
        type="button"
        :title="'İçindeki koşullardan ' + (draft.match === 'all' ? 'en az birinin' : 'hepsinin') + ' sağlanması gereken bir kutu'"
        @click="addGroup"
      >
        + Grup ekle <span class="muted">({{ groupLabel.toLocaleLowerCase('tr') }})</span>
      </button>
    </div>
    </template>

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
