<!doctype html>
<html>
  <head>
    <title>支付管理中心</title>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />

    <style>
      /* 防止页面刷新时白屏的初始样式 */
      html {
        background-color: #fafbfc;
      }

      html.dark {
        background-color: #070707;
      }
    </style>

    <script>
      // 初始化 html class 主题属性
      ;(function () {
        try {
          if (typeof Storage === 'undefined' || !window.localStorage) {
            return
          }

          const themeType = localStorage.getItem('sys-theme')
          if (themeType === 'dark') {
            document.documentElement.classList.add('dark')
          }
        } catch (e) {
          console.warn('Failed to apply initial theme:', e)
        }
      })()
    </script>
    <script type="module" crossorigin src="../static/assets/index-IIq1IliX.js"></script>
    <link rel="modulepreload" crossorigin href="../static/assets/vendor-Du8xmWnP.js">
    <link rel="modulepreload" crossorigin href="../static/assets/vue-vendor-Boehz83i.js">
    <link rel="modulepreload" crossorigin href="../static/assets/element-plus-CjZ6NaEe.js">
    <link rel="stylesheet" crossorigin href="../static/assets/vendor-BgDCIyLK.css">
    <link rel="stylesheet" crossorigin href="../static/assets/element-plus-I4XHZGy8.css">
    <link rel="stylesheet" crossorigin href="../static/assets/index-2g6phfp_.css">
  </head>

  <body>
    <div id="app"></div>
  </body>
</html>
