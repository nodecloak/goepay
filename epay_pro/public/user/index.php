<!doctype html>
<html>
  <head>
    <title></title>
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
    <script type="module" crossorigin src="../static/assets/index-_K93c9Ev.js"></script>
    <link rel="modulepreload" crossorigin href="../static/assets/vendor-DRxD4IH7.js">
    <link rel="modulepreload" crossorigin href="../static/assets/vue-vendor-DR2b24US.js">
    <link rel="modulepreload" crossorigin href="../static/assets/element-plus-Bdeq4_Of.js">
    <link rel="stylesheet" crossorigin href="../static/assets/vendor-BgDCIyLK.css">
    <link rel="stylesheet" crossorigin href="../static/assets/element-plus-CKYNvudA.css">
    <link rel="stylesheet" crossorigin href="../static/assets/index-nN4maGP0.css">
  </head>

  <body>
    <div id="app"></div>
  </body>
</html>
