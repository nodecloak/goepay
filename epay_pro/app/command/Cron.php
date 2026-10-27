<?php

declare(strict_types=1);

namespace app\command;

use app\lib\PoolDbManager;
use app\logic\CronTaskRunner;
use think\Container;
use think\console\Input;
use think\console\input\Argument;
use think\console\Output;
use think\facade\Config;
use think\facade\Db;

class Cron extends BaseCommand
{
    protected function configure()
    {
        $this->setName('cron')
            ->addArgument('task', Argument::OPTIONAL)
            ->setDescription('启动 Swoole 计划任务守护进程');
    }

    protected function initialize(Input $input, Output $output)
    {
        Container::getInstance()->bind('think\DbManager', PoolDbManager::class);
        parent::initialize($input, $output);
    }

    protected function execute(Input $input, Output $output): int
    {
        $task = trim((string)$input->getArgument('task'));
        if ($task !== '') {
            return $this->executeOnce($task, $output);
        }

        if (!extension_loaded('swoole')) {
            $output->writeln('<error>未安装 Swoole 扩展</error>');
            return 1;
        }
        if (config_get('cron_mode', 'url') !== 'swoole') {
            $output->writeln('<error>后台计划任务执行方式不是 Swoole 守护进程</error>');
            return 1;
        }

        try {
            Db::name('crontab')->whereNotNull('lock_token')->update([
                'lock_token' => null,
                'locked_until' => null,
            ]);
        } catch (\Throwable $e) {
            $output->writeln('<error>清理计划任务锁失败：' . $e->getMessage() . '</error>');
            return 1;
        }

        $output->writeln('Swoole 计划任务守护进程已启动');
        define('CRON_INIT', true);
        $startVersion = $this->readAppVersion();
        $hookFlags = SWOOLE_HOOK_ALL;
        if (!function_exists('proc_open') && defined('SWOOLE_HOOK_PROC')) {
            $hookFlags &= ~SWOOLE_HOOK_PROC;
        }
        \Swoole\Coroutine::set(['hook_flags' => $hookFlags]);
        \Swoole\Coroutine\run(function () use ($output, $startVersion): void {
            $runner = new CronTaskRunner();
            $tasks = [];
            $running = [];
            $lastRefresh = 0;

            while (true) {
                $now = time();
                if ($lastRefresh === 0 || $now - $lastRefresh >= 60) {
                    $lastRefresh = $now;
                    try {
                        if (config_get('cron_mode', 'url', true) !== 'swoole') {
                            $output->writeln('计划任务执行方式已切换，守护进程停止运行');
                            return;
                        }
                        $currentVersion = $this->readAppVersion();
                        if ($currentVersion !== '' && $currentVersion !== $startVersion) {
                            $output->writeln(
                                '系统版本号已变化（' . $startVersion . ' -> ' . $currentVersion . '），守护进程停止运行'
                            );
                            return;
                        }
                        $res = Db::name('config')->where('cache', 1)->cache('configs', 0)->column('v', 'k');
                        Config::set($res, 'sys');
                        $rows = Db::name('crontab')->where('enabled', 1)->select()->toArray();
                        $freshTasks = [];
                        foreach ($rows as $row) {
                            if (empty($row['next_run_time'])) {
                                $row['next_run_time'] = $runner->nextRunTime($row);
                                if ($row['next_run_time'] !== null) {
                                    Db::name('crontab')->where('id', $row['id'])->update([
                                        'next_run_time' => $row['next_run_time'],
                                    ]);
                                }
                            }
                            $freshTasks[(int)$row['id']] = $row;
                        }
                        $tasks = $freshTasks;
                    } catch (\Throwable $e) {
                        exception_log($e, '计划任务列表刷新');
                        $output->writeln('<error>刷新计划任务失败：' . $e->getMessage() . '</error>');
                    }
                    config_set('cron_task_time', date('Y-m-d H:i:s'));
                }

                foreach ($tasks as $id => &$task) {
                    $nextRun = empty($task['next_run_time']) ? 0 : strtotime((string)$task['next_run_time']);
                    if ($nextRun > $now || isset($running[$id])) {
                        continue;
                    }

                    $lockToken = bin2hex(random_bytes(16));
                    if (!$runner->claim((int)$id, $lockToken)) {
                        $task['next_run_time'] = date('Y-m-d H:i:s', $now + 60);
                        continue;
                    }
                    $running[$id] = true;
                    $task['next_run_time'] = $runner->nextRunTime($task);
                    \Swoole\Coroutine::create(function () use (
                        $id,
                        $lockToken,
                        &$tasks,
                        &$running,
                        $runner,
                        $output
                    ): void {
                        $task = $tasks[$id];
                        //$output->writeln('[' . date('Y-m-d H:i:s') . '] 开始：' . $task['name']);
                        try {
                            $update = $runner->runScheduled($task, $lockToken);
                            if (!empty($update['result'])) {
                                echo (string)$update['result'] . "\n";
                            }
                        } catch (\Throwable $e) {
                            exception_log($e, '计划任务状态更新:' . $task['task']);
                            unset($running[$id]);
                            $output->writeln(
                                '[' . date('Y-m-d H:i:s') . '] 失败：' . $task['name'] . '，' . $e->getMessage()
                            );
                            return;
                        }
                        if (isset($tasks[$id])) {
                            $tasks[$id] = array_merge($tasks[$id], $update);
                            if (($update['enabled'] ?? 1) == 0) {
                                unset($tasks[$id]);
                            }
                        }
                        unset($running[$id]);
                        $status = ($update['last_status'] ?? 2) === 1 ? '完成' : '失败';
                        $output->writeln('[' . date('Y-m-d H:i:s') . '] ' . $status . '：' . $task['name']);
                    });
                }
                unset($task);

                \Swoole\Coroutine::sleep(1);
            }
        });

        return 0;
    }

    /**
     * 从配置文件读取当前系统版本号（绕过进程内缓存，便于检测热更新）
     */
    private function readAppVersion(): string
    {
        $file = config_path() . 'app.php';
        if (!is_file($file)) {
            return (string)config('app.version');
        }
        if (function_exists('opcache_invalidate')) {
            opcache_invalidate($file, true);
        }
        $config = include $file;
        if (!is_array($config) || !isset($config['version'])) {
            return (string)config('app.version');
        }
        return (string)$config['version'];
    }

    private function executeOnce(string $identifier, Output $output): int
    {
        try {
            $row = Db::name('crontab')->where('task', $identifier)->find();
        } catch (\Throwable $e) {
            exception_log($e, '读取调试计划任务:' . $identifier);
            $output->writeln('<error>读取计划任务失败：' . $e->getMessage() . '</error>');
            return 1;
        }
        if (!$row) {
            $output->writeln('<error>计划任务不存在：' . $identifier . '</error>');
            return 1;
        }

        $runner = new CronTaskRunner();
        $lockToken = bin2hex(random_bytes(16));
        if (!$runner->claim((int)$row['id'], $lockToken, false)) {
            $output->writeln('<error>当前计划任务正在执行，请稍后再试</error>');
            return 1;
        }

        if (!defined('CRON_INIT')) {
            define('CRON_INIT', true);
        }
        $output->writeln('开始调试计划任务：' . $row['name'] . ' [' . $identifier . ']');

        try {
            $params = json_decode((string)($row['params'] ?? '{}'), true);
            $result = $runner->execute(
                $identifier,
                is_array($params) ? $params : [],
                (string)($row['plugin'] ?? '')
            );
            if ($result !== '') {
                echo $result . PHP_EOL;
            }
            $output->writeln('<info>计划任务执行完成</info>');
            return 0;
        } catch (\Throwable $e) {
            exception_log($e, '调试计划任务:' . $identifier);
            $output->writeln('<error>计划任务执行失败：' . $e->getMessage() . '</error>');
            return 1;
        } finally {
            try {
                $runner->release((int)$row['id'], $lockToken);
            } catch (\Throwable $e) {
                exception_log($e, '释放计划任务调试锁:' . $identifier);
            }
        }
    }
}
