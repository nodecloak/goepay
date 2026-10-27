<?php

namespace app\lib\ocr;

use Exception;

class OcrFactory
{
    /**
     * @param string $type
     * @return OcrServiceInterface
     * @throws \InvalidArgumentException
     */
    public static function create($type)
    {
        switch ($type) {
            case 'aliyun':
                if (empty(config_get('ocr_aliyunid')) || empty(config_get('ocr_aliyunkey'))) throw new Exception('请先配置阿里云OCR文字识别密钥');
                return new AliyunOcrService(config_get('ocr_aliyunid'), config_get('ocr_aliyunkey'));
            case 'baidu':
                if (empty(config_get('ocr_baiduid')) || empty(config_get('ocr_baidukey'))) throw new Exception('请先配置百度云OCR文字识别密钥');
                return new BaiduOcrService(config_get('ocr_baiduid'), config_get('ocr_baidukey'));
            default:
                throw new \InvalidArgumentException('Unsupported OCR type: ' . $type);
        }
    }
}
