<?php

declare(strict_types=1);

namespace app\common;

/**
 * 商户进件插件接口
 */
interface ApplymentInterface
{
    /**
     * 获取表单数据
     * @param string $action 操作类型
     * @param string|null $info 商户信息JSON
     * @return array
     */
    public function getFormData(string $action, ?string $info = null): array;

    /**
     * 商户创建/进件
     * @param array $data 进件数据
     * @return array
     */
    public function create(array $data): array;

    /**
     * 申请单进度查询
     * @param array $row 商户记录
     * @return array
     */
    public function query(array $row): array;

    /**
     * 上传图片
     * @param string $filepath 文件路径
     * @param string $filename 文件名
     * @return array
     */
    public function uploadImage(string $filepath, string $filename): array;

    /**
     * 上传文件
     * @param string $filepath 文件路径
     * @param string $filename 文件名
     * @return array
     */
    public function uploadFile(string $filepath, string $filename): array;

    /**
     * 获取银行列表
     * @param int $page 页码
     * @param int $limit 每页条数
     * @param string $keyword 关键词
     * @param string $keyid 银行ID
     * @return array
     */
    public function getBankList(int $page, int $limit, string $keyword = '', string $keyid = ''): array;

    /**
     * 查询支行列表
     * @param string $bank_code 银行代码
     * @param string $city_code 城市代码
     * @return array
     */
    public function getBankBranchList(string $bank_code, string $city_code): array;

    /**
     * 修改支付状态
     * @param array $row 商户记录
     * @param int $status 状态
     * @return array
     */
    public function setPayStatus(array $row, int $status): array;

    /**
     * 获取操作按钮列表
     * @param array $row 商户记录
     * @return array
     */
    public static function getOperation(array $row): array;
}
