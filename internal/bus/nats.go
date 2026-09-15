package bus

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
)

// Publish sends one message using the core NATS text protocol. Continuity keeps
// this tiny client for bootstrapping/health checks; production JetStream use is
// delegated to the official NATS client in the agent runtime.
func Publish(addr, subject string, payload []byte) error {
	if addr == "" {
		addr = "127.0.0.1:4222"
	}
	if subject == "" || strings.ContainsAny(subject, " \t\r\n") {
		return fmt.Errorf("invalid NATS subject %q", subject)
	}
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	r := bufio.NewReader(conn)
	line, err := r.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read NATS INFO: %w", err)
	}
	if !strings.HasPrefix(line, "INFO ") {
		return fmt.Errorf("expected NATS INFO, got %q", strings.TrimSpace(line))
	}

	connect, _ := json.Marshal(map[string]any{"verbose": false, "pedantic": false, "tls_required": false, "name": "continuity-smoke", "lang": "go-stdlib", "version": "v2alpha1"})
	if _, err := fmt.Fprintf(conn, "CONNECT %s\r\nPING\r\n", connect); err != nil {
		return err
	}
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			return err
		}
		if strings.HasPrefix(l, "PING") {
			if _, err := fmt.Fprint(conn, "PONG\r\n"); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(l, "PONG") {
			break
		}
		if strings.HasPrefix(l, "-ERR") {
			return fmt.Errorf("NATS: %s", strings.TrimSpace(l))
		}
	}
	if _, err := fmt.Fprintf(conn, "PUB %s %d\r\n", subject, len(payload)); err != nil {
		return err
	}
	if _, err := conn.Write(payload); err != nil {
		return err
	}
	if _, err := fmt.Fprint(conn, "\r\nPING\r\n"); err != nil {
		return err
	}
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			return err
		}
		if strings.HasPrefix(l, "PING") {
			if _, err := fmt.Fprint(conn, "PONG\r\n"); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(l, "PONG") {
			return nil
		}
		if strings.HasPrefix(l, "-ERR") {
			return fmt.Errorf("NATS: %s", strings.TrimSpace(l))
		}
	}
}
