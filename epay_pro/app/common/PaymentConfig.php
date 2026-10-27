<?php

declare(strict_types=1);

namespace app\common;

class PaymentConfig
{
    // 获取支付宝配置
    public static function getAlipayConfig(array $channel): array
    {
        $config = [
            //应用ID
            'app_id' => $channel['appid'],

            //支付宝公钥
            'alipay_public_key' => $channel['appkey'],

            //应用私钥
            'app_private_key' => $channel['appsecret'],

            //签名方式,默认为RSA2
            'sign_type' => 'RSA2',

            //编码格式
            'charset' => 'UTF-8',

            //是否使用证书模式
            'cert_mode' => ($channel['sign_type'] ?? '0') == '1' ? 1 : 0,
        ];
        if ($channel['plugin'] == 'alipaysl') {
            $config['app_auth_token'] = $channel['appmchid']; //商户授权token
        } elseif ($channel['plugin'] == 'alipaycode') {
            $config['app_auth_token'] = $channel['apptoken']; //商户授权token
        } elseif ($channel['plugin'] == 'alipayd' || $channel['plugin'] == 'alipayrp') {
            $config['smid'] = $channel['appmchid']; //子商户SMID
        }

        if ($config['cert_mode'] == 1) {
            $config['app_cert_path'] = getCertFilePath($channel['app_cert_path']);
            $config['alipay_cert_path'] = getCertFilePath($channel['alipay_cert_path']);
            $config['root_cert_path'] = getCertFilePath($channel['root_cert_path']);
        }
        return $config;
    }

    // 获取微信支付配置
    public static function getWxpayConfig(array $channel): array
    {
        $config = [
            //绑定支付的APPID
            'appid' => $channel['appid'],

            //商户号
            'mchid' => $channel['appmchid'],

            //商户APIv2密钥
            'apikey' => $channel['appkey'],

            //公众帐号secert（仅JSAPI支付需要配置）
            'appsecret' => '',

            //商户证书路径（仅退款、撤销订单时需要）
            'sslcert_path' => getCertFilePath($channel['sslcert_path'] ?? ''),

            //商户证书私钥路径
            'sslkey_path' => getCertFilePath($channel['sslkey_path'] ?? ''),

            //付款到银行卡的RSA公钥
            'publickey_path' => getCertFilePath('wxpaypubkey_'.$channel['appmchid'].'.pem', true),
        ];
        if ($channel['plugin'] == 'wxpaysl') {
            $config['sub_mchid'] = $channel['appurl']; //子商户号
            $config['sub_appid'] = $channel['sub_appid'] ?? ''; //子商户APPID
        }
        return $config;
    }

    // 获取微信支付v3配置
    public static function getWxpayV3Config(array $channel): array
    {
        //微信支付v3配置文件
        $config = [
            //应用ID
            'appid' => $channel['appid'],

            //商户号
            'mchid' => $channel['appmchid'],

            //APIv3密钥
            'apikey' => $channel['appsecret'],

            //「商户API私钥」文件路径
            'merchantPrivateKeyFilePath' => getCertFilePath($channel['merchant_key_path'] ?? ''),

            //「商户API证书」的「证书序列号」
            'merchantCertificateSerial' => $channel['appkey'],

            //「微信支付公钥」文件路径
            'platformPublicKeyFilePath' => getCertFilePath($channel['platform_pubkey_path'] ?? ''),

            //「微信支付平台证书」文件路径
            'platformCertificateFilePath' => getCertFilePath('wxpaycert_'.$channel['appmchid'].'.pem', true),

            //微信支付平台公钥ID
            'platformCertificateSerial' => $channel['publickeyid'] ?? '',

            //是否电商收付通
            'ecommerce' => false,
        ];
        if ($channel['plugin'] == 'wxpaynp') {
            $config['sub_mchid'] = $channel['appurl']; //子商户号
            $config['sub_appid'] = $channel['sub_appid'] ?? ''; //子商户APPID
            $config['ecommerce'] = ($channel['appswitch'] ?? '0') == '1';
        } elseif ($channel['plugin'] == 'wxpayng') {
            $config['isGlobal'] = true; //是否国际版商户
        }
        return $config;
    }

    // 获取QQ钱包支付配置
    public static function getQqpayConfig(array $channel): array
    {
        $config = [
            //商户号
            'mchid' => $channel['appid'],

            //商户API密钥
            'apikey' => $channel['appkey'],

            //公众号APPID（可空）
            'appid' => '',

            //操作员账号（仅退款、撤销订单、企业付款时需要）
            //创建操作员说明：https://kf.qq.com/faq/170112AZ7Fzm170112VNz6zE.html
            'op_userid' => $channel['appurl'],

            //操作员密码
            'op_userpwd' => $channel['appmchid'],

            //商户证书路径（仅退款、撤销订单、企业付款时需要）
            'sslcert_path' => getCertFilePath($channel['sslcert_path'] ?? ''),

            //商户证书私钥路径
            'sslkey_path' => getCertFilePath($channel['sslkey_path'] ?? ''),
        ];
        return $config;
    }

    // 获取抖音支付配置
    public static function getDouyinpayConfig(array $channel): array
    {
        $config = [
            //应用ID
            'appid' => $channel['appid'],

            //商户号
            'mchid' => $channel['appmchid'],

            //接口加密密钥
            'apikey' => $channel['apikey'],

            //「商户API私钥」文件路径
            'merchantPrivateKeyFilePath' => getCertFilePath($channel['merchant_key_path'] ?? ''),

            //「商户API证书」的「证书序列号」
            'merchantCertificateSerial' => $channel['certserial'],

            //「微信支付平台证书」文件路径
            'platformCertificateFilePath' => getCertFilePath('douyinpaycert_' . $channel['appmchid'] . '.pem', true),
        ];
        return $config;
    }
}
