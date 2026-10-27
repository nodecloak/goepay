<?php

declare(strict_types=1);

namespace app\common;

/**
 * 投诉处理插件接口
 */
interface ComplainInterface
{
    //刷新最新投诉记录列表
    public function refreshNewList(int $num): array;

    //回调刷新单条投诉记录
    public function refreshNewInfo(string $thirdid, mixed $type = null): mixed;

    //获取单条投诉记录
    public function getNewInfo(int $id): array;

    //获取投诉协商记录
    public function getNegotiationHistory(string $thirdid): mixed;

    //上传图片
    public function uploadImage(string $thirdid, string $filepath, string $filename): mixed;

    //处理投诉（仅支付宝）
    public function feedbackSubmit(string $thirdid, string $code, string $content, array $images = []): mixed;

    //回复用户
    public function replySubmit(string $thirdid, string $content, array $images = []): mixed;

    //更新退款审批结果（仅微信）
    public function refundProgressSubmit(string $thirdid, string $code, string $content, ?string $remark = null, array $images = []): mixed;

    //处理完成（仅微信）
    public function complete(string $thirdid): mixed;

    //商家补充凭证（仅支付宝）
    public function supplementSubmit(string $thirdid, string $content, array $images = []): mixed;

    //下载图片（仅微信）
    public function getImage(string $media_id): mixed;

    //设置投诉通知回调地址
    public function setNotifyUrl(): mixed;

    //删除投诉通知回调地址
    public function delNotifyUrl(): mixed;
}
