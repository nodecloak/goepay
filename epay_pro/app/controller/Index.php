<?php

namespace app\controller;

use app\BaseController;
use app\logic\AuthLogic;
use think\facade\Db;
use think\facade\Config;
use think\facade\Cache;
use think\facade\Session;
use think\facade\Log;
use think\facade\View;

class Index extends BaseController
{
    public function index(string $page = 'index')
    {
        if (!preg_match('/^[A-Za-z0-9_]+$/', $page)) {
            return View::fetch('index/404');
        }
        
        $allow_search = true;
        include_once app()->getBasePath() . 'txprotect.php';

        if ($page == 'index' && $this->request->get('invite')) {
            $invite_code = $this->request->get('invite', '', 'trim');
            $uid = get_invite_uid($invite_code);
            if ($uid && is_numeric($uid)) {
                Session::set('invite_uid', intval($uid));
            }
        }

        if (config_get('homepage') == 3) {
            return '';
        } elseif (config_get('homepage') == 2) {
            return '<html><frameset framespacing="0" border="0" rows="0" frameborder="0"><frame name="main" src="' . config_get('homepage_url') . '" scrolling="auto" noresize></frameset></html>';
        } elseif (config_get('homepage') == 1) {
            return $this->redirect('/user/');
        }

        $template_file = \app\lib\Template::getHomeFilePath($page);
        $state = AuthLogic::getUserState();
        View::assign('islogin', $state['islogin']);
        View::assign('siteurl', $this->request->siteurl);
        return View::fetch($template_file);
    }

    public function test()
    {
        return 'success';
    }

    //开发文档
    public function doc(string $page = 'index')
    {
        if (!preg_match('/^[A-Za-z0-9_]+$/', $page)) {
            return View::fetch('index/404');
        }
        $viewFile = app()->getBasePath() . 'view' . DIRECTORY_SEPARATOR . 'doc' . DIRECTORY_SEPARATOR . $page . '.html';
        if (!is_file($viewFile)) {
            return View::fetch('index/404');
        }
        $allow_search = true;
        include_once app()->getBasePath() . 'txprotect.php';
        if ($page == 'paytype') {
            View::assign('paytype', Db::name('type')->where('status', 1)->order('id', 'asc')->select()->toArray());
        }
        return View::fetch('doc/' . $page);
    }
}
