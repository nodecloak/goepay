<?php

namespace plugins\sms\thinkapi;

use app\common\BaseSms;

class ThinkapiPlugin extends BaseSms
{

    public function send(string $phone, string $tpl_code, array $tpl_param): bool
    {
        if (empty($this->config['appCode']) || empty($this->config['signId'])) {
            throw new \Exception('短信配置不能为空');
            return false;
        }
        $tpl_code = $this->getTplCode($tpl_code);

        $url = 'https://api.topthink.com/sms/send';
        $param = ['appCode' => $this->config['appCode'], 'signId' => $this->config['signId'], 'templateId' => $tpl_code, 'phone' => $phone, 'params' => json_encode($tpl_param)];
        $response = get_curl($url, http_build_query($param));
        if (!$response) {
            throw new \Exception('接口请求失败');
            return false;
        }
        $arr = json_decode($response, true);
        if (isset($arr['code']) && $arr['code'] == 0) {
            return true;
        } elseif (isset($arr['message'])) {
            throw new \Exception($arr['message']);
            return false;
        } else {
            throw new \Exception('接口返回异常');
            return false;
        }
    }
}
