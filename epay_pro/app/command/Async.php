<?php

declare(strict_types=1);

namespace app\command;

use app\lib\PoolDbManager;
use app\logic\AsyncTaskRunner;
use think\Container;
use think\console\Input;
use think\console\Output;
use think\facade\Config;
use think\facade\Db;
use think\facade\Log;
use Throwable;

class Async extends BaseCommand
{
    protected function configure()
    {
        $this->setName('async')
            ->setDescription('启动 Swoole 异步任务守护进程');
    }

    protected function initialize(Input $input, Output $output)
    {
        Container::getInstance()->bind('think\DbManager', PoolDbManager::class);
        parent::initialize($input, $output);
    }

    protected function execute(Input $input, Output $output): int
    {
        if (!extension_loaded('swoole')) {
            $output->writeln('<error>未安装 Swoole 扩展</error>');
            return 1;
        }
        if (!AsyncTaskRunner::isEnabled()) {
            $output->writeln('<error>后台未开启异步处理模式</error>');
            return 1;
        }

        if (!defined('ASYNC_TASK')) {
            define('ASYNC_TASK', true);
        }

        $listenType = AsyncTaskRunner::listenType();
        $listenPort = AsyncTaskRunner::listenPort();
        $startVersion = $this->readAppVersion();

        $output->writeln('Swoole 异步任务守护进程已启动（' . $this->listenDesc($listenType, $listenPort) . '）');

        $hookFlags = SWOOLE_HOOK_ALL;
        if (!function_exists('proc_open') && defined('SWOOLE_HOOK_PROC')) {
            $hookFlags &= ~SWOOLE_HOOK_PROC;
        }
        \Swoole\Coroutine::set(['hook_flags' => $hookFlags]);
        \Swoole\Coroutine\run(function () use ($output, $listenType, $listenPort, $startVersion): void {
            try {
                $server = $this->createServer($listenType, $listenPort);
            } catch (Throwable $e) {
                $output->writeln('<error>启动异步任务服务失败：' . $e->getMessage() . '</error>');
                return;
            }

            $server->set([
                'open_eof_check' => true,
                'package_eof' => "\n",
                'package_max_length' => 2 * 1024 * 1024,
            ]);
            $server->handle(function (\Swoole\Coroutine\Server\Connection $conn) use ($output): void {
                $this->serveConnection($conn, $output);
            });

            $this->watchConfig($server, $output, $listenType, $listenPort, $startVersion);
            $timerId = \Swoole\Timer::tick(60000, function () use ($server, $output, $listenType, $listenPort, $startVersion): void {
                $this->watchConfig($server, $output, $listenType, $listenPort, $startVersion);
            });
            $server->start();
            \Swoole\Timer::clear($timerId);
        });

        return 0;
    }

    private function createServer(string $listenType, int $listenPort): \Swoole\Coroutine\Server
    {
        if ($listenType === 'tcp') {
            return new \Swoole\Coroutine\Server('127.0.0.1', $listenPort);
        }

        $path = AsyncTaskRunner::socketPath();
        $dir = dirname($path);
        if (!is_dir($dir)) {
            mkdir($dir, 0755, true);
        }
        if (file_exists($path)) {
            @unlink($path);
        }
        $server = new \Swoole\Coroutine\Server('unix:' . $path);
        @chmod($path, 0666);
        return $server;
    }

    private function watchConfig(
        \Swoole\Coroutine\Server $server,
        Output $output,
        string $listenType,
        int $listenPort,
        string $startVersion
    ): void {
        try {
            $res = Db::name('config')->where('cache', 1)->cache('configs', 0)->column('v', 'k');
            Config::set($res, 'sys');

            if (!AsyncTaskRunner::isEnabled()) {
                $output->writeln('异步处理模式已关闭，守护进程停止运行');
                $server->shutdown();
                return;
            }

            $currentType = AsyncTaskRunner::listenType();
            $currentPort = AsyncTaskRunner::listenPort();
            if ($currentType !== $listenType || ($currentType === 'tcp' && $currentPort !== $listenPort)) {
                $output->writeln('异步服务监听配置已变化，守护进程停止运行');
                $server->shutdown();
                return;
            }

            $currentVersion = $this->readAppVersion();
            if ($currentVersion !== '' && $currentVersion !== $startVersion) {
                $output->writeln(
                    '系统版本号已变化（' . $startVersion . ' -> ' . $currentVersion . '），守护进程停止运行'
                );
                $server->shutdown();
                return;
            }
        } catch (Throwable $e) {
            exception_log($e, '异步任务配置刷新');
            $output->writeln('<error>刷新异步任务配置失败：' . $e->getMessage() . '</error>');
        }
        config_set('async_task_time', date('Y-m-d H:i:s'));
    }

    private function serveConnection(\Swoole\Coroutine\Server\Connection $conn, Output $output): void
    {
        $data = $conn->recv();
        if ($data === '' || $data === false) {
            $conn->close();
            return;
        }
        $this->acceptTask($conn, rtrim((string)$data, "\r\n"), $output);
    }

    private function acceptTask(\Swoole\Coroutine\Server\Connection $conn, string $line, Output $output): void
    {
        $request = json_decode(trim($line), true);
        if (!is_array($request) || empty($request['task'])) {
            $this->reply($conn, false, '请求格式无效');
            $conn->close();
            return;
        }

        $task = (string)$request['task'];
        $payload = $request['payload'] ?? [];
        if (!is_array($payload)) {
            $this->reply($conn, false, '任务参数无效');
            $conn->close();
            return;
        }

        try {
            AsyncTaskRunner::assertTask($task);
        } catch (Throwable $e) {
            $this->reply($conn, false, $e->getMessage());
            $conn->close();
            return;
        }

        $this->reply($conn, true);
        $conn->close();

        try {
            AsyncTaskRunner::execute($task, $payload);
            $output->writeln('[' . date('Y-m-d H:i:s') . '] 完成：' . $task);
        } catch (Throwable $e) {
            exception_log($e, '异步任务:' . $task);
            Log::error('[异步任务][' . $task . '] ' . $e->getMessage());
            $output->writeln('[' . date('Y-m-d H:i:s') . '] 失败：' . $task . '，' . $e->getMessage());
        }
    }

    private function reply(\Swoole\Coroutine\Server\Connection $conn, bool $ok, string $error = ''): void
    {
        $data = ['ok' => $ok];
        if (!$ok) {
            $data['error'] = $error;
        }
        $conn->send(json_encode($data, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES) . "\n");
    }

    private function listenDesc(string $listenType, int $listenPort): string
    {
        if ($listenType === 'tcp') {
            return 'TCP 127.0.0.1:' . $listenPort;
        }
        return 'Unix Socket ' . AsyncTaskRunner::socketPath();
    }

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
}
