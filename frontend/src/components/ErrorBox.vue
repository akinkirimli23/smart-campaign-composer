<script setup lang="ts">
import type { FieldError } from '../api/types'

defineProps<{ message: string; details?: FieldError[] }>()
</script>

<template>
  <div class="alert error" role="alert">
    <strong>{{ message }}</strong>
    <ul v-if="details && details.length">
      <li v-for="(d, i) in details" :key="i">
        <code v-if="d.path">{{ d.path }}</code> {{ d.message }}
      </li>
    </ul>
  </div>
</template>
