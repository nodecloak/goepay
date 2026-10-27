<?php

declare(strict_types=1);

namespace app\command;

use think\console\Input;
use think\console\input\Argument;
use think\console\Output;
use think\facade\Db;

class Reset extends BaseCommand
{
    protected function configure()
    {
        // 指令配置
        $this->setName('reset')
            ->addArgument('type', Argument::REQUIRED, '操作类型,pwd:重置密码,paypwd:重置密码,totp:关闭TOTP')
            ->addArgument('password', Argument::OPTIONAL, '密码')
            ->setDescription('重置密码');
    }

    protected function execute(Input $input, Output $output)
    {
        $type = trim($input->getArgument('type'));
        $adminRow = Db::name('adminuser')->where('uid', 1)->find();
        if (!$adminRow) {
            $output->writeln('管理员（UID:1）不存在');
            return;
        }

        if ($type == 'pwd') {
            $password = $input->getArgument('password');
            if (empty($password)) $password = '123456';
            Db::name('adminuser')->where('uid', 1)->update([
                'password' => password_hash($password, PASSWORD_DEFAULT),
            ]);
            $output->writeln('管理员 ' . $adminRow['username'] . ' 密码重置成功');
        } elseif ($type == 'paypwd') {
            $password = $input->getArgument('password');
            if (empty($password)) $password = '123456';
            Db::name('adminuser')->where('uid', 1)->update([
                'paypwd' => password_hash($password, PASSWORD_DEFAULT),
            ]);
            $output->writeln('管理员 ' . $adminRow['username'] . ' 支付密码重置成功');
        } elseif ($type == 'totp') {
            Db::name('adminuser')->where('uid', 1)->update([
                'totp' => 0,
                'totp_secret' => null,
            ]);
            $output->writeln('管理员 ' . $adminRow['username'] . ' TOTP关闭成功');
        }
    }
}
