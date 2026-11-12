import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import './style.css'
import { restoreTheme } from './lib/theme'

restoreTheme()

createApp(App).use(createPinia()).mount('#app')
