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

// sendVerificationCode 通过 SMTP 发送注册验证码邮件。
// 465 端口走隐式 TLS，其他端口走 STARTTLS（由 smtp.SendMail 自动协商）。
func sendVerificationCode(cfg SMTPConfig, to, code string) error {
	subject := fmt.Sprintf("=?UTF-8?B?%s?=", base64.StdEncoding.EncodeToString([]byte("项目沙盘注册验证码")))
	body := strings.Join([]string{
		"你正在注册「项目沙盘」，邮箱验证码：",
		"",
		"    " + code,
		"",
		"验证码 15 分钟内有效。若非本人操作，请忽略此邮件。",
	}, "\r\n")
	msg := strings.Join([]string{
		"From: " + fmt.Sprintf("=?UTF-8?B?%s?=", base64.StdEncoding.EncodeToString([]byte(cfg.FromName))) + " <" + cfg.User + ">",
		"To: " + to,
		"Subject: " + subject,
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		body,
	}, "\r\n")

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
