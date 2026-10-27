<?php

namespace plugins\mail\smtp;

use app\common\BaseMail;

class SmtpPlugin extends BaseMail
{
    public function send(string $to, string $subject, string $body): bool
    {
        if (empty($this->config['host']) || empty($this->config['port']) || empty($this->config['username']) || empty($this->config['password'])) {
            throw new \Exception('SMTP 发信配置不完整');
        }
        $fromName = config_get('sitename');

        $mail = new \PHPMailer\PHPMailer\PHPMailer(true);
        try {
            $mail->SMTPDebug = 0;
            $mail->CharSet = 'UTF-8';
            $mail->Timeout = 5;
            $mail->isSMTP();
            $mail->Host = $this->config['host'];
            $mail->SMTPAuth = true;
            $mail->Username = $this->config['username'];
            $mail->Password = $this->config['password'];
            if ($this->config['port'] == 587) {
                $mail->SMTPSecure = 'tls';
            } elseif ($this->config['port'] >= 465) {
                $mail->SMTPSecure = 'ssl';
            } else {
                $mail->SMTPAutoTLS = false;
            }
            $mail->Port = (int) $this->config['port'];
            $mail->setFrom($this->config['username'], $fromName);
            $mail->addAddress($to);
            $mail->addReplyTo($this->config['username'], $fromName);
            $mail->isHTML(true);
            $mail->Subject = $subject;
            $mail->Body = $body;
            $mail->send();
            return true;
        } catch (\Throwable $e) {
            throw new \Exception($e->getMessage() ?: ($mail->ErrorInfo ?? '发信失败'));
        }
    }
}
