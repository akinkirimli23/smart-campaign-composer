import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import '@fontsource-variable/inter-tight'
import '@fontsource/instrument-serif/400.css'
import '@fontsource/instrument-serif/400-italic.css'
import './style.css'
import { restoreTheme } from './lib/theme'

restoreTheme()

createApp(App).use(createPinia()).mount('#app')
