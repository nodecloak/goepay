import { defineConfig } from 'vitepress'
import { sidebar } from './navigation'

export default defineConfig({
  lang: 'zh-CN',
  title: 'GOEPAY 使用手册',
  description: 'GOEPAY 管理员与商户后台操作指南：商户、通道、订单、退款、结算与系统维护。',
  base: '/manual/',
  cleanUrls: false,
  srcExclude: ['README.md', 'VERIFICATION.md'],
  lastUpdated: false,
  appearance: 'light',
  head: [
    ['link', { rel: 'icon', href: '/manual/brand/favicon.svg', type: 'image/svg+xml' }],
    ['meta', { name: 'theme-color', content: '#ffffff' }],
  ],
  themeConfig: {
    logo: { light: '/brand/wordmark-light.svg', dark: '/brand/wordmark-dark.svg' },
    siteTitle: false,
    nav: [
      { text: '开始使用', link: '/' },
      { text: '管理员', link: '/admin/quickstart' },
      { text: '商户', link: '/merchant/quickstart' },
      { text: '功能索引', link: '/reference/admin-menu' },
      { text: '开发文档', link: 'https://goepay.cc/doc/' },
    ],
    sidebar,
    outline: { level: [2, 3], label: '本页内容' },
    search: { provider: 'local', options: {
      miniSearch: {
        options: { tokenize: (text: string) => text.match(/[\p{Script=Han}]|[\p{L}\p{N}_-]+/gu) || [] },
        searchOptions: { prefix: true, fuzzy: 0.15 },
      },
      translations: {
        button: { buttonText: '搜索手册', buttonAriaLabel: '搜索使用手册' },
        modal: {
          noResultsText: '没有找到相关内容', resetButtonTitle: '清空搜索',
          displayDetails: '显示摘要', backButtonTitle: '返回',
          footer: { selectText: '选择', selectKeyAriaLabel: '回车', navigateText: '切换', navigateUpKeyAriaLabel: '向上', navigateDownKeyAriaLabel: '向下', closeText: '关闭', closeKeyAriaLabel: 'Esc' },
        },
      },
    } },
    docFooter: { prev: '上一篇', next: '下一篇' },
    darkModeSwitchLabel: '切换明暗主题',
    darkModeSwitchTitle: '深色模式',
    lightModeSwitchTitle: '浅色模式',
    sidebarMenuLabel: '目录', returnToTopLabel: '回到顶部',
    externalLinkIcon: true,
  },
})
