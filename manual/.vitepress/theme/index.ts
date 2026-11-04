import DefaultTheme from 'vitepress/theme'
import Diagram from './Diagram.vue'
import Layout from './Layout.vue'
import RoleQuickstart from './RoleQuickstart.vue'
import './custom.css'

export default {
  extends: DefaultTheme,
  Layout,
  enhanceApp({ app }) {
    app.component('Diagram', Diagram)
    app.component('RoleQuickstart', RoleQuickstart)
  },
}
