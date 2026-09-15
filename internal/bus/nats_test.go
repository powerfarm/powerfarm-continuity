package bus

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestPublish(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		fmt.Fprint(c, "INFO {\"server_id\":\"test\"}\r\n")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				done <- err
				return
			}
			if strings.HasPrefix(line, "PING") {
				fmt.Fprint(c, "PONG\r\n")
				continue
			}
			if strings.HasPrefix(line, "PUB continuity.test ") {
				var n int
				if _, err := fmt.Sscanf(strings.TrimSpace(line), "PUB continuity.test %d", &n); err != nil {
					done <- err
					return
				}
				b := make([]byte, n+2)
				if _, err := r.Read(b); err != nil {
					done <- err
					return
				}
				if string(b[:n]) != "hello" {
					done <- fmt.Errorf("payload %q", string(b[:n]))
					return
				}
			}
			if strings.HasPrefix(line, "CONNECT") {
				continue
			}
			if strings.HasPrefix(line, "PING") {
				fmt.Fprint(c, "PONG\r\n")
			}
			if strings.TrimSpace(line) == "PING" {
				fmt.Fprint(c, "PONG\r\n")
			}
			if strings.HasPrefix(line, "PING") {
				continue
			}
			if strings.HasPrefix(line, "PUB") {
				continue
			}
		}
	}()
	if err := Publish(ln.Addr().String(), "continuity.test", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	default:
	}
}
