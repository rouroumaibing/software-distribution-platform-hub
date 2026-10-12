package notification

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Mailer sends one plaintext email. Errors are logged by the dispatcher, never
// propagated into the triggering code path — email is best-effort by design
// (站内通知中心才是权威渠道，邮件只是镜像).
type Mailer interface {
	Send(to, subject, body string) error
}

// SMTPMailer is a STARTTLS-preferring net/smtp implementation. Works against
// any SMTP relay; credentials optional (open relays in the kind dev env).
type SMTPMailer struct {
	Host     string // e.g. smtp.example.com
	Port     int    // 25 / 465 / 587
	From     string
	Username string
	Password string

	// Timeout bounds the whole dial+send; zero defaults to 10s.
	Timeout time.Duration
}

// NoopMailer satisfies Mailer when SMTP is unconfigured — dispatch stays a
// controlled no-op instead of nil-checking everywhere.
type NoopMailer struct{}

func (NoopMailer) Send(string, string, string) error { return nil }

func (m SMTPMailer) Send(to, subject, body string) error {
	if m.Host == "" || m.From == "" {
		return fmt.Errorf("smtp 未配置（SMTP_HOST/SMTP_FROM）")
	}
	port := m.Port
	if port == 0 {
		port = 587
	}
	timeout := m.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	addr := m.Host + ":" + strconv.Itoa(port)
	from := addrOf(m.From)
	tos := []string{addrOf(to)}

	msg := buildMessage(m.From, to, subject, body)

	con, err := tls.DialWithDialer(&net.Dialer{Timeout: timeout},
		"tcp", addr, &tls.Config{ServerName: m.Host, InsecureSkipVerify: false})
	if err != nil {
		// 465 直连失败 → 回落明文 + STARTTLS（587 常见形态）
		con2, err2 := net.DialTimeout("tcp", addr, timeout)
		if err2 != nil {
			return fmt.Errorf("smtp dial: %w", err)
		}
		return sendWith(con2, m, from, tos, msg)
	}
	defer con.Close()
	return sendWith(con, m, from, tos, msg)
}

func sendWith(con net.Conn, m SMTPMailer, from string, tos []string, msg []byte) error {
	cl, err := smtp.NewClient(con, m.Host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer cl.Close()

	if ok, _ := cl.Extension("STARTTLS"); ok {
		if err := cl.StartTLS(&tls.Config{ServerName: m.Host}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if m.Username != "" {
		auth := smtp.PlainAuth("", m.Username, m.Password, m.Host)
		if ok, _ := cl.Extension("AUTH"); ok {
			if err := cl.Auth(auth); err != nil {
				return fmt.Errorf("smtp auth: %w", err)
			}
		}
	}
	if err := cl.Mail(from); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	for _, t := range tos {
		if err := cl.Rcpt(t); err != nil {
			return fmt.Errorf("smtp rcpt: %w", err)
		}
	}
	w, err := cl.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		w.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return cl.Quit()
}

// addrOf strips a display name ("Foo <a@b.c>" → "a@b.c").
func addrOf(s string) string {
	if i := strings.LastIndex(s, "<"); i >= 0 {
		if j := strings.Index(s[i:], ">"); j > 0 {
			return s[i+1 : i+j]
		}
	}
	return s
}

func buildMessage(from, to, subject, body string) []byte {
	h := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n"+
		"Content-Type: text/plain; charset=UTF-8\r\nDate: %s\r\n\r\n",
		from, to, subject, time.Now().Format(time.RFC1123Z))
	return append([]byte(h), body...)
}
