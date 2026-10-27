<?php
if (!is_file(__DIR__ . '/../vendor/autoload.php')) {
  exit('网站文件不完整，确保上传的是完整安装包！');
}
preg_match("#^\d.\d#", PHP_VERSION, $p_v);
$php_v = str_replace('.', '', $p_v[0]);

$is_win = strtolower(substr(PHP_OS, 0, 3)) === 'win';
$loader_ext = $is_win ? 'dll' : 'so';
$arch_suffix = '';
if (!$is_win) {
  $machine = strtolower((string)php_uname('m'));
  if ($machine === 'aarch64' || $machine === 'arm64' || str_starts_with($machine, 'armv8')) {
    $arch_suffix = '_arm64';
  }
}
$loader_path = dirname(dirname(__FILE__)).'/app/loader/swoole_loader_'.$php_v.'_nts' . $arch_suffix . '.' . $loader_ext;
$loader_path = str_replace('/', DIRECTORY_SEPARATOR, $loader_path);

function render_page(string $title, string $body_html): void {
  $title_esc = htmlspecialchars($title, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
  echo '<!doctype html><html lang="zh-CN"><head>';
  echo '<meta charset="utf-8">';
  echo '<meta name="viewport" content="width=device-width, initial-scale=1">';
  echo '<meta name="color-scheme" content="light dark">';
  echo "<title>{$title_esc}</title>";
  echo '<link href="//lib.baomitu.com/twitter-bootstrap/5.3.3/css/bootstrap.min.css" rel="stylesheet">';
  echo '<style>
    body{background: radial-gradient(1200px circle at 10% 0%, rgba(13,110,253,.10), transparent 45%), radial-gradient(900px circle at 90% 20%, rgba(25,135,84,.10), transparent 40%), var(--bs-body-bg);height:100vh}
    .brand-badge{background: rgba(13,110,253,.10); color: var(--bs-primary); border: 1px solid rgba(13,110,253,.20);}
    code{word-break: break-all;}
  </style>';
  echo '</head><body>';
  echo '<div class="container py-4 py-md-5">';
  echo $body_html;
  echo '<footer class="text-center text-body-secondary small mt-4 mt-md-5">';
  echo '<div></div>';
  echo '</footer>';
  echo '</div>';
  echo '<script src="//lib.baomitu.com/twitter-bootstrap/5.3.3/js/bootstrap.bundle.min.js"></script>';
  echo '<script>
    (function(){
      var btn = document.getElementById("copyExtLine");
      if(!btn) return;
      btn.addEventListener("click", async function(){
        var text = btn.getAttribute("data-copy") || "";
        function setCopied(){
          btn.classList.remove("btn-outline-primary");
          btn.classList.add("btn-success");
          btn.textContent = "已复制";
          setTimeout(function(){
            btn.classList.remove("btn-success");
            btn.classList.add("btn-outline-primary");
            btn.textContent = "复制到剪贴板";
          }, 1500);
        }
        async function copyModern(){
          if(!navigator.clipboard || (window.isSecureContext === false)) return false;
          await navigator.clipboard.writeText(text);
          return true;
        }
        function copyLegacy(){
          try{
            var ta = document.createElement("textarea");
            ta.value = text;
            ta.setAttribute("readonly","");
            ta.style.position = "fixed";
            ta.style.top = "-1000px";
            ta.style.left = "-1000px";
            document.body.appendChild(ta);
            ta.select();
            ta.setSelectionRange(0, ta.value.length);
            var ok = document.execCommand("copy");
            document.body.removeChild(ta);
            return ok;
          }catch(e){
            return false;
          }
        }
        try{
          var ok = false;
          try{ ok = await copyModern(); }catch(e){ ok = false; }
          if(!ok) ok = copyLegacy();
          if(ok) setCopied();
          else alert("复制失败，请手动复制。");
        }catch(e){
          alert("复制失败，请手动复制。");
        }
      });
    })();
  </script>';
  echo '</body></html>';
}

function render_alert_and_exit(string $title, string $message, string $variant = 'warning'): void {
  $message_esc = htmlspecialchars($message, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
  $title_esc = htmlspecialchars($title, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
  render_page($title, '
    <div class="row justify-content-center">
      <div class="col-12 col-lg-9 col-xl-8">
        <div class="d-flex align-items-center gap-2 mb-3">
          <span class="badge rounded-pill brand-badge">Swoole Loader</span>
          <span class="text-body-secondary small">安装助手</span>
        </div>
        <div class="alert alert-' . $variant . ' shadow-sm" role="alert">
          <div class="fw-semibold mb-1">' . $title_esc . '</div>
          <div>' . $message_esc . '</div>
        </div>
      </div>
    </div>
  ');
  exit;
}

if ($p_v[0] < 8.2 || $p_v[0] >= 8.3) {
  render_alert_and_exit('环境不支持', '只支持 PHP 8.2 版本！', 'danger');
}
if (!file_exists($loader_path)) {
  render_alert_and_exit('缺少 Loader 文件', 'Swoole Loader 文件不存在，请联系管理员手动安装！', 'danger');
}

$has_swoole_loader = extension_loaded('swoole_loader');
if ($has_swoole_loader) {
  exit('<script>window.location.href="/";</script>');
}

$ini_file = php_ini_loaded_file();
$ini_file_show = $ini_file ? $ini_file : '（未检测到：可能通过其它方式加载配置）';
$ini_file_show_esc = htmlspecialchars($ini_file_show, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
$loader_ext_esc = htmlspecialchars($loader_ext, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
$loader_path_esc = htmlspecialchars($loader_path, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
$extension_line = "extension={$loader_path}";
$extension_line_esc = htmlspecialchars($extension_line, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
$is_kangle = strpos($_SERVER['SERVER_SOFTWARE'], 'Kangle') !== false;

render_page('Swoole Loader 安装助手', '
  <div class="row justify-content-center">
    <div class="col-12 col-lg-9 col-xl-8">
      <div class="d-flex flex-column flex-md-row align-items-start align-items-md-center justify-content-between gap-2 mb-3">
        <div>
          <h1 class="h4 mb-0">Swoole Loader 安装助手</h1>
        </div>
        <div class="text-body-secondary small">
          <div>PHP：' . htmlspecialchars(PHP_VERSION, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8') . '</div>
          <div>平台：' . ($is_win ? 'Windows' : 'Linux/Unix') . '</div>
        </div>
      </div>

      <div class="alert alert-' . ($has_swoole_loader ? 'success' : 'warning') . ' d-flex flex-column flex-sm-row align-items-start align-items-sm-center gap-2 shadow-sm" role="alert">
        <div class="fw-semibold">扩展状态：' . ($has_swoole_loader ? '已安装' : '未安装') . '</div>
        <div class="text-body-secondary small ms-sm-auto">
          ' . ($has_swoole_loader ? '检测到 swoole_loader 已加载。' : '未检测到 swoole_loader，请按下面步骤安装后刷新。') . '
        </div>
        ' . ($has_swoole_loader ? '<div class="ms-sm-3"><a class="btn btn-success btn-sm" href="./" role="button">返回首页</a></div>' : '') . '
      </div>

      <div class="card shadow-sm border-0">
        <div class="card-body p-3 p-md-4">

          <ol class="mb-0 ps-3">
            <li class="mb-2">
              打开您的 PHP 配置文件（<code>' . $ini_file_show_esc . '</code>）。如果是宝塔面板：软件商店 → PHP → 设置 → 配置文件。' . ($is_kangle ? '如果是Kangle面板：进入PHP设置，开启Swoole Loader 3.2扩展即可。' : '') . '
            </li>
            <li class="mb-2">
              在配置文件底部检查是否曾添加过类似 <code>extension=swoole_loader_***.' . $loader_ext_esc . '</code> 的旧配置；如有请先删除。
            </li>
            <li class="mb-3">
              将下面这一行追加到 <strong>php.ini 最后一行</strong> 并保存：
              <div class="mt-2 p-3 rounded-3 border bg-body-tertiary">
                <div class="d-flex flex-column flex-sm-row align-items-stretch align-items-sm-center gap-2">
                  <code class="flex-grow-1">' . $extension_line_esc . '</code>
                  <button id="copyExtLine" type="button" class="btn btn-outline-primary btn-sm" data-copy="' . htmlspecialchars($extension_line, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8') . '">复制到剪贴板</button>
                </div>
              </div>
              <br/><strong>注意结尾要有换行。</strong>
            </li>
            <li class="mb-2">重启 PHP 进程。</li>
            <li><a href="javascript:location.reload()">刷新</a>本页面，确认 Loader 已加载。</li>
          </ol>
        </div>
      </div>
    </div>
  </div>
');