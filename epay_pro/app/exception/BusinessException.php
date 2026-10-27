<?php

declare(strict_types=1);

namespace app\exception;

use think\Exception;

class BusinessException extends Exception
{

    private string $sub_code;

    public function __construct(string $message = '', string $sub_code = '')
    {
        parent::__construct($message);
        $this->sub_code = $sub_code;
    }

    public function getSubCode(): string
    {
        return $this->sub_code;
    }
}