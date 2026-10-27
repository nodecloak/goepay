<?php
declare (strict_types = 1);

namespace app;

use think\App;
use think\exception\ValidateException;
use think\Validate;
use think\facade\Db;
use think\facade\Cache;
use think\facade\View;

/**
 * 控制器基础类
 */
abstract class BaseController
{
    /**
     * Request实例
     * @var \think\Request
     */
    protected $request;

    /**
     * 应用实例
     * @var \think\App
     */
    protected $app;

    /**
     * 是否批量验证
     * @var bool
     */
    protected $batchValidate = false;

    /**
     * 控制器中间件
     * @var array
     */
    protected $middleware = [];

    /**
     * 构造方法
     * @access public
     * @param  App  $app  应用对象
     */
    public function __construct(App $app)
    {
        $this->app     = $app;
        $this->request = $this->app->request;

        // 控制器初始化
        $this->initialize();
    }

    // 初始化
    protected function initialize()
    {
    }

    /**
     * 验证数据
     * @access protected
     * @param  array        $data     数据
     * @param  string|array $validate 验证器名或者验证规则数组
     * @param  array        $message  提示信息
     * @param  bool         $batch    是否批量验证
     * @return array|string|true
     * @throws ValidateException
     */
    protected function validate(array $data, string|array $validate, array $message = [], bool $batch = false)
    {
        if (is_array($validate)) {
            $v = new Validate();
            $v->rule($validate);
        } else {
            if (strpos($validate, '.')) {
                // 支持场景
                [$validate, $scene] = explode('.', $validate);
            }
            $class = false !== strpos($validate, '\\') ? $validate : $this->app->parseClass('validate', $validate);
            $v     = new $class();
            if (!empty($scene)) {
                $v->scene($scene);
            }
        }

        $v->message($message);

        // 是否批量验证
        if ($batch || $this->batchValidate) {
            $v->batch(true);
        }

        return $v->failException(true)->check($data);
    }

    /**
     * 展示错误/提示页面
     * @param string $msg 提示内容，支持 HTML
     * @param string $title 页面标题
     * @return \think\Response
     */
    protected function sysmsg(string $msg = '未知的异常', string $title = '站点提示信息')
    {
        return View::fetch('common/sysmsg', [
            'msg' => $msg,
            'title' => $title,
        ]);
    }

    /**
     * 展示成功/失败提示页面
     * @param string $title 页面标题
     * @param string $desc 提示内容，支持 HTML
     * @param bool $success 是否成功
     * @return \think\Response
     */
    protected function showAlert(string $title, string $desc = '', bool $success = false)
    {
        return View::fetch('common/alert', [
            'title' => $title,
            'desc' => $desc,
            'success' => $success,
            'cdnpublic' => get_cdn_public(),
        ]);
    }

    protected function redirect(string $url)
    {
        return response('<script>window.location.href="'.$url.'";</script>');
    }

    /**
     * 文件响应
     * @param string $data 文件内容
     * @param string $filename 文件名称
     * @return \think\Response
     */
    protected function fileResponse(string $data, string $filename): \think\Response
    {
        $headers = [
            'Content-Description' => 'File Transfer',
            'Content-Type' => 'application/force-download',
            'Content-Length' => (string) strlen($data),
            'Content-Disposition' => 'attachment; filename=' . $filename,
        ];
        return response($data, 200, $headers);
    }

    /**
     * 下载文件
     * @param string $filepath 文件路径
     * @param string $filename 文件名称
     * @return \think\Response
     */
    protected function downloadFile(string $filepath, string $filename): \think\Response
    {
        return \think\Response::create($filepath, 'file')->name(basename($filename))->force(true)->mimeType('application/octet-stream');
    }
}
