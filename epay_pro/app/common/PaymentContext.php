<?php

declare(strict_types=1);

namespace app\common;

/**
 * 支付上下文：承载当前支付请求相关数据
 */
class PaymentContext
{
    
    public function __construct(
        public array $order,
        public string $ordername = '',
        public string $method = '',
        public bool $isMobile = false,
        public string $mdevice = ''
    ) {
    }
}
