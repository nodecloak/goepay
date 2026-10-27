<?php

declare(strict_types=1);

namespace app\common;

/**
 * 支付插件接口
 */
interface PaymentInterface
{
    public function submit(PaymentContext $ctx): array;
}
