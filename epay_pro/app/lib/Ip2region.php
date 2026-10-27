<?php

namespace app\lib;

use Exception;

/**
 * ip2region xdb 查询
 */
class Ip2region
{
    private const HEADER_INFO_LENGTH = 256;
    private const VECTOR_INDEX_COLS = 256;
    private const VECTOR_INDEX_SIZE = 8;

    public const IPV4 = 0;
    public const IPV6 = 1;

    private $handle = null;

    private int $family;
    private int $bytes;
    private int $segmentIndexSize;

    public static function newWithFileOnly(int $family, string $dbFile): self
    {
        return new self($family, $dbFile);
    }

    private function __construct(int $family, string $dbFile)
    {
        if ($family === self::IPV4) {
            $this->bytes = 4;
            $this->segmentIndexSize = 14;
        } else {
            $this->bytes = 16;
            $this->segmentIndexSize = 38;
        }
        $this->family = $family;
        $this->handle = fopen($dbFile, 'rb');
        if ($this->handle === false) {
            throw new Exception("failed to open xdb file '{$dbFile}'");
        }
    }

    public function __destruct()
    {
        if ($this->handle !== null) {
            fclose($this->handle);
            $this->handle = null;
        }
    }

    /**
     * @throws Exception
     */
    public function search(string $ip): string
    {
        $ipBytes = self::parseIP($ip);
        if ($ipBytes === null) {
            throw new Exception("invalid ip address `{$ip}`");
        }
        return $this->searchByBytes($ipBytes);
    }

    /**
     * @throws Exception
     */
    private function searchByBytes(string $ipBytes): string
    {
        if (strlen($ipBytes) !== $this->bytes) {
            throw new Exception('invalid ip address length');
        }

        $il0 = ord($ipBytes[0]) & 0xFF;
        $il1 = ord($ipBytes[1]) & 0xFF;
        $idx = $il0 * self::VECTOR_INDEX_COLS * self::VECTOR_INDEX_SIZE + $il1 * self::VECTOR_INDEX_SIZE;

        $buff = $this->read(self::HEADER_INFO_LENGTH + $idx, 8);
        $sPtr = self::leUint32($buff, 0);
        $ePtr = self::leUint32($buff, 4);

        if ($sPtr == 0 || $ePtr == 0) {
            return '';
        }

        $bytes = strlen($ipBytes);
        $dBytes = $bytes << 1;
        $idxSize = $this->segmentIndexSize;

        $dataLen = 0;
        $dataPtr = 0;
        $l = 0;
        $h = (int) (($ePtr - $sPtr) / $idxSize);

        while ($l <= $h) {
            $m = ($l + $h) >> 1;
            $p = $sPtr + $m * $idxSize;
            $buff = $this->read($p, $idxSize);

            if ($this->ipSubCompare($ipBytes, $buff, 0) < 0) {
                $h = $m - 1;
            } elseif ($this->ipSubCompare($ipBytes, $buff, $bytes) > 0) {
                $l = $m + 1;
            } else {
                $dataLen = self::leUint16($buff, $dBytes);
                $dataPtr = self::leUint32($buff, $dBytes + 2);
                break;
            }
        }

        if ($dataLen == 0) {
            return '';
        }

        return $this->read($dataPtr, $dataLen);
    }

    private function ipSubCompare(string $ip1, string $buff, int $offset): int
    {
        if ($this->family === self::IPV4) {
            $len = strlen($ip1);
            $eIdx = $offset + $len;
            for ($i = 0, $j = $eIdx - 1; $i < $len; $i++, $j--) {
                $i1 = ord($ip1[$i]) & 0xFF;
                $i2 = ord($buff[$j]) & 0xFF;
                if ($i1 > $i2) {
                    return 1;
                }
                if ($i1 < $i2) {
                    return -1;
                }
            }
            return 0;
        }
        $r = strcmp($ip1, substr($buff, $offset, strlen($ip1)));
        if ($r < 0) {
            return -1;
        }
        if ($r > 0) {
            return 1;
        }
        return 0;
    }

    /**
     * @throws Exception
     */
    private function read(int $offset, int $len): string
    {
        $r = fseek($this->handle, $offset);
        if ($r === -1) {
            throw new Exception("failed to fseek to {$offset}");
        }
        $buff = fread($this->handle, $len);
        if ($buff === false) {
            throw new Exception("failed to fread len {$len}");
        }
        if (strlen($buff) !== $len) {
            throw new Exception("incomplete read: expected {$len} bytes");
        }
        return $buff;
    }

    private static function parseIP(string $ipString): ?string
    {
        $flag = FILTER_FLAG_IPV4 | FILTER_FLAG_IPV6;
        if (!filter_var($ipString, FILTER_VALIDATE_IP, $flag)) {
            return null;
        }
        return inet_pton($ipString);
    }

    /**
     * @return int|string
     */
    private static function leUint32(string $b, int $idx)
    {
        $val = (ord($b[$idx]))
            | (ord($b[$idx + 1]) << 8)
            | (ord($b[$idx + 2]) << 16)
            | (ord($b[$idx + 3]) << 24);
        if ($val < 0 && PHP_INT_SIZE === 4) {
            return sprintf('%u', $val);
        }
        return $val;
    }

    private static function leUint16(string $b, int $idx): int
    {
        return (ord($b[$idx])) | (ord($b[$idx + 1]) << 8);
    }
}
