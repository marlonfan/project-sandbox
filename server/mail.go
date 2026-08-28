package main

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// buildVerificationMail 生成 multipart/alternative 邮件（纯文本回退 + 品牌化 HTML）。
func buildVerificationMail(cfg SMTPConfig, to, code string) string {
	encodedFrom := "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(cfg.FromName)) + "?="
	subject := "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte("项目沙盘注册验证码："+code)) + "?="
	boundary := "psb-" + newID()
	from := encodedFrom + " <" + cfg.User + ">"

	text := strings.Join([]string{
		"你正在注册「项目沙盘」，邮箱验证码：",
		"",
		"    " + code,
		"",
		"验证码 15 分钟内有效，请尽快填写。",
		"若非本人操作，请忽略此邮件，你的邮箱不会因此被注册。",
	}, "\r\n")

	// 邮件客户端兼容：表格布局 + 全内联样式，配色与沙盘一致（暖象牙底 / 赤陶色主视觉）
	// 验证码容器 white-space:nowrap 防止数字换行；媒体查询在窄屏收紧字号与留白
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<style>
  @media only screen and (max-width:480px) {
    .psb-code { font-size:26px !important; letter-spacing:7px !important; padding:14px 20px !important; }
    .psb-card { border-radius:0 !important; }
    .psb-pad { padding-left:22px !important; padding-right:22px !important; }
  }
</style>
</head>
<body style="margin:0;padding:0;background:#f3f1ea;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background:#f3f1ea;">
<tr><td align="center" style="padding:36px 10px;">
<table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0" class="psb-card" style="max-width:600px;width:100%;background:#ffffff;border:1px solid #ece9e0;border-radius:14px;font-family:-apple-system,BlinkMacSystemFont,'PingFang SC','Hiragino Sans GB','Microsoft YaHei',sans-serif;">
  <tr><td class="psb-pad" style="padding:38px 44px 6px;">
    <div style="font-size:13px;font-weight:700;letter-spacing:3px;color:#d97757;">项目沙盘</div>
    <h1 style="margin:16px 0 8px;font-size:22px;line-height:1.4;color:#1f1e1d;">注册验证码</h1>
    <p style="margin:0;font-size:14px;color:#6b6862;line-height:1.8;">你正在注册「项目沙盘」，请使用下面的验证码完成邮箱验证：</p>
  </td></tr>
  <tr><td align="center" style="padding:24px 20px 8px;">
    <div class="psb-code" style="white-space:nowrap;background:#faf9f5;border:1px solid #ece9e0;border-radius:12px;padding:18px 28px;font-size:34px;font-weight:700;letter-spacing:10px;text-indent:10px;color:#1f1e1d;">` + code + `</div>
  </td></tr>
  <tr><td class="psb-pad" style="padding:14px 44px 36px;">
    <p style="margin:0;font-size:13px;color:#8a867e;line-height:1.9;">验证码 15 分钟内有效，请尽快填写。<br>若非本人操作，请忽略此邮件，你的邮箱不会因此被注册。</p>
  </td></tr>
  <tr><td class="psb-pad" style="padding:16px 44px;background:#faf9f5;border-top:1px solid #f0eee6;border-radius:0 0 14px 14px;">
    <p style="margin:0;font-size:12px;color:#b5b1a7;line-height:1.7;">项目沙盘 · 以目标为分区俯瞰所有项目的推进状态<br>本邮件由系统自动发送，请勿回复。</p>
  </td></tr>
</table>
</td></tr>
</table>
</body>
</html>`

	headers := strings.Join([]string{
		"From: " + from,
		"To: " + to,
		"Subject: " + subject,
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"",
	}, "\r\n")

	msg := headers + "\r\n\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" +
		text + "\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" +
		html + "\r\n" +
		"--" + boundary + "--\r\n"
	return msg
}

// sendVerificationCode 通过 SMTP 发送注册验证码邮件。
// 465 端口走隐式 TLS，其他端口走 STARTTLS（由 smtp.SendMail 自动协商）。
func sendVerificationCode(cfg SMTPConfig, to, code string) error {
	msg := buildVerificationMail(cfg, to, code)

	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	auth := smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)

	if cfg.Port == 465 {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: cfg.Host})
		if err != nil {
			return fmt.Errorf("连接 SMTP 失败: %w", err)
		}
		defer conn.Close()
		c, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			return fmt.Errorf("建立 SMTP 会话失败: %w", err)
		}
		defer c.Close()
		if err = c.Auth(auth); err != nil {
			return fmt.Errorf("SMTP 认证失败: %w", err)
		}
		if err = c.Mail(cfg.User); err != nil {
			return fmt.Errorf("设置发件人失败: %w", err)
		}
		if err = c.Rcpt(to); err != nil {
			return fmt.Errorf("设置收件人失败: %w", err)
		}
		w, err := c.Data()
		if err != nil {
			return err
		}
		if _, err = w.Write([]byte(msg)); err != nil {
			return err
		}
		if err = w.Close(); err != nil {
			return err
		}
		return c.Quit()
	}
	return smtp.SendMail(addr, auth, cfg.User, []string{to}, []byte(msg))
}
