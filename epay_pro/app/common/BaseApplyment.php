<?php

declare(strict_types=1);

namespace app\common;

use app\logic\ApplymentLogic;
use think\facade\Db;

/**
 * 商户进件插件基类
 */
abstract class BaseApplyment implements ApplymentInterface
{
    public string $type;
    public string $plugin;

    protected static $allow_image_ext = ['png', 'jpg', 'jpeg', 'bmp', 'gif', 'webp'];
    protected static $allow_file_ext = ['png', 'jpg', 'jpeg', 'bmp', 'gif', 'webp', 'pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'zip', '7z', 'rar', 'txt', 'mp4', 'mp3', 'wav'];
    protected array $applychannel;
    protected array $channel;
    protected string $basedir;
    protected string $datadir;
    protected string $banklist_file;
    protected string $imgdir = UPLOAD_ROOT . 'applyments/';
    protected string $imgurl = '/upload/applyments/';

    public function __construct(array $applychannel, array $channel)
    {
        $this->applychannel = $applychannel;
        $this->channel = $channel;
        $this->type = $applychannel['type'];
        $this->plugin = $channel['plugin'];
        $this->basedir = PLUGIN_ROOT . 'applyments/' . $this->type . '/';
        $this->datadir = app()->getBasePath() . 'data/';
        $this->banklist_file = $this->datadir . 'bank.json';
    }

    abstract public function create(array $data): array;

    abstract public function query(array $row): array;

    abstract public function setPayStatus(array $row, int $status): array;

    abstract public static function getOperation(array $row): array;

    /**
     * 上传图片
     * @param string $filepath 上传的临时文件路径
     * @param string $filename 上传的文件名
     * @return array
     */
    public function uploadImage(string $filepath, string $filename): array
    {
        $file_ext = pathinfo($filename, PATHINFO_EXTENSION);
        if (!in_array($file_ext, self::$allow_image_ext)) {
            return ['code' => -1, 'msg' => '图片格式必须为：png、bmp、gif、jpg、jpeg'];
        }
        if (filesize($filepath) > 5 * 1024 * 1024) {
            return ['code' => -1, 'msg' => '图片大小不能超过5M'];
        }
        if ($file_name = $this->saveFile($filepath, $file_ext)) {
            return ['code' => 0, 'image_id' => $file_name];
        } else {
            return ['code' => -1, 'msg' => '图片上传失败'];
        }
    }

    /**
     * 上传文件
     * @param string $filepath 上传的临时文件路径
     * @param string $filename 上传的文件名
     * @return array
     */
    public function uploadFile(string $filepath, string $filename): array
    {
        $file_ext = pathinfo($filename, PATHINFO_EXTENSION);
        if (!in_array($file_ext, self::$allow_file_ext)) {
            return ['code' => -1, 'msg' => '不支持的文件格式'];
        }
        if (filesize($filepath) > 10 * 1024 * 1024) {
            return ['code' => -1, 'msg' => '文件大小不能超过10M'];
        }
        if ($file_name = $this->saveFile($filepath, $file_ext)) {
            return ['code' => 0, 'file_id' => $file_name];
        } else {
            return ['code' => -1, 'msg' => '文件上传失败'];
        }
    }

    /**
     * 获取银行列表
     * @param int $page 页码
     * @param int $limit 每页条数
     * @param string $keyword 关键词
     * @param string $keyid 银行ID
     * @return array
     */
    public function getBankList(int $page, int $limit, string $keyword = '', string $keyid = ''): array
    {
        $file = file_get_contents($this->banklist_file);
        $data = json_decode($file, true);

        if (!empty($keyid)) {
            $data = array_filter($data, function ($item) use ($keyid) {
                return $item['value'] == $keyid;
            });
        } elseif (!empty($keyword)) {
            $data = array_filter($data, function ($item) use ($keyword) {
                return strpos($item['label'], $keyword) !== false;
            });
        }
        $total = count($data);
        $data = array_slice($data, ($page - 1) * $limit, $limit);

        $results = [];
        foreach ($data as $item) {
            $results[] = ['id' => $item['value'], 'text' => $item['label']];
        }
        return ['code' => 0, 'data' => ['results' => $results, 'pagination' => ['more' => ($page * $limit < $total)]]];
    }

    /**
     * 查询支行列表
     * @param string $bank_code 银行代码
     * @param string $city_code 城市代码
     * @return array
     */
    public function getBankBranchList(string $bank_code, string $city_code): array
    {
        try {
            $branches = ApplymentLogic::getBankBranchList($bank_code, $city_code);
        } catch (\Exception $e) {
            return ['code' => -1, 'msg' => $e->getMessage()];
        }
        $results = [];
        foreach ($branches as $item) {
            $results[] = ['id' => $item['id'], 'text' => $item['name']];
        }
        return ['code' => 0, 'data' => $results];
    }

    /**
     * 返回图片完整路径
     * @param string $image_id 图片ID
     * @return string|null
     */
    protected function checkImage(string $image_id): string|null
    {
        if (empty($image_id)) return null;
        $path_info = pathinfo($image_id);
        if (!in_array($path_info['extension'], self::$allow_image_ext)) {
            throw new \Exception($image_id . '图片格式不正确');
        }
        if (!preg_match('/^[0-9a-z]{32}$/i', $path_info['filename'])) {
            throw new \Exception($image_id . '图片路径不正确');
        }
        $file = $this->imgdir . $image_id;
        if (!file_exists($file)) {
            throw new \Exception($image_id . '图片不存在');
        }
        return $file;
    }

    /**
     * 返回文件完整路径
     * @param string $file_id 文件ID
     * @return string|null
     */
    protected function checkFile(string $file_id): string|null
    {
        if (empty($file_id)) return null;
        $path_info = pathinfo($file_id);
        if (!in_array($path_info['extension'], self::$allow_file_ext)) {
            throw new \Exception($file_id . '文件格式不正确');
        }
        if (!preg_match('/^[0-9a-z]{32}$/i', $path_info['filename'])) {
            throw new \Exception($file_id . '文件路径不正确');
        }
        $file = $this->imgdir . $file_id;
        if (!file_exists($file)) {
            throw new \Exception($file_id . '文件不存在');
        }
        return $file;
    }


    /**
     * 渲染消息提示页面
     * @param string $content 消息内容
     * @param int $type 消息类型 1=success 2=info 3=warning 4=danger
     * @param string|false $back 返回链接
     * @return \think\Response
     */
    protected function showMsg(string $content, int $type = 4, string|false $back = false): \think\Response
    {
        $panelMap = [1 => 'success', 2 => 'info', 3 => 'warning', 4 => 'danger'];
        $panel = $panelMap[$type] ?? 'danger';
        return view('common/applyments_msg', [
            'content' => $content,
            'panel' => $panel,
            'back' => $back,
        ]);
    }

    /**
     * 保存上传的图片到本地
     * @param string $filepath 上传的临时文件路径
     * @param string $filename 上传的文件名
     * @param string $image_id 接口返回的图片ID（用作文件名）
     * @throws \InvalidArgumentException
     */
    protected function saveUploadedImage(string $filepath, string $filename, string $image_id): void
    {
        $image_id = ApplymentLogic::parseImageId($image_id);
        if (empty($image_id)) return;
        if (strlen($image_id) > 255) return;

        $file_ext = pathinfo($filename, PATHINFO_EXTENSION);
        if (!in_array($file_ext, self::$allow_image_ext)) {
            $file_ext = 'png';
        }
        $file_id = $this->type . '_' . $image_id;

        $this->saveFile($filepath, $file_ext, $file_id);
    }

    /**
     * 保存上传的文件到本地
     * @param string $filepath 上传的临时文件路径
     * @param string $filename 上传的文件名
     * @param string $file_id 接口返回的文件ID（用作文件名）
     * @throws \InvalidArgumentException
     */
    protected function saveUploadedFile(string $filepath, string $filename, string $file_id): void
    {
        $file_id = ApplymentLogic::parseImageId($file_id);
        if (empty($file_id)) return;
        if (strlen($file_id) > 255) return;
        
        $file_ext = pathinfo($filename, PATHINFO_EXTENSION);
        if (!in_array($file_ext, self::$allow_file_ext)) return;
        $file_id = $this->type . '_' . $file_id;

        $this->saveFile($filepath, $file_ext, $file_id);
    }

    private function saveFile(string $file_path, string $file_ext, ?string $file_id = null): string|bool
    {
        $md5 = md5_file($file_path);
        $file_name = $md5 . '.' . $file_ext;
        $file_size = filesize($file_path);
        if (!is_dir($this->imgdir)) mkdir($this->imgdir, 0755, true);
        $dest = $this->imgdir . $file_name;
        if (!move_uploaded_file($file_path, $dest)) return false;

        if (!$file_id) $file_id = $this->type . '_' . $file_name;

        $row = Db::name('attachment')->where('name', $file_id)->where('type', 'applyments')->find();
        if (!$row) {
            Db::name('attachment')->insert([
                'type' => 'applyments',
                'name' => $file_id,
                'path' => $file_name,
                'addtime' => date('Y-m-d H:i:s'),
                'status' => 1,
                'ext' => $file_ext,
                'size' => $file_size,
                'md5' => $md5,
            ]);
        } else {
            Db::name('attachment')->where('id', $row['id'])->update([
                'path' => $file_name,
                'addtime' => date('Y-m-d H:i:s'),
                'status' => 1,
                'ext' => $file_ext,
                'size' => $file_size,
                'md5' => $md5,
            ]);
        }
        return $file_name;
    }

    /**
     * 处理进件付费
     */
    protected static function payForMerchant(int $id, int $uid, float $money): bool
    {
        $userrow = Db::name('user')->where('uid', $uid)->field('upid,money')->find();
        if ($userrow['money'] >= $money) {
            changeUserMoney($uid, $money, false, '新增商户进件');
            Db::name('applymerchant')->where('id', $id)->update(['paid' => 1]);

            if ($userrow['upid'] > 0) {
                $upgid = Db::name('user')->where('uid', $userrow['upid'])->value('gid');
                mergeGroupConfig($upgid);
                if (config_get('invite_open') == 1 && config_get('invite_apply_rate') > 0) {
                    $invite_money = round($money * config_get('invite_apply_rate') / 100, 2);
                    if ($invite_money > 0) {
                        changeUserMoney($userrow['upid'], $invite_money, true, '邀请商户进件');
                    }
                }
            }
            return true;
        }
        return false;
    }
}
