package proxyproto

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func pipe(t *testing.T, send []byte) (net.Conn, net.IP) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		c, _ := net.Dial("tcp", l.Addr().String())
		if send != nil {
			c.Write(send)
		}
		time.Sleep(time.Second)
		c.Close()
	}()
	c, _ := l.Accept()
	pc, ip, err := Read(c, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	return pc, ip
}

func TestV2(t *testing.T) {
	h := append([]byte{}, v2sig...)
	h = append(h, 0x21, 0x11, 0, 12)
	h = append(h, 203, 0, 113, 7, 10, 0, 0, 1)
	h = binary.BigEndian.AppendUint16(h, 5555)
	h = binary.BigEndian.AppendUint16(h, 443)
	h = append(h, "hello"...)
	c, ip := pipe(t, h)
	if ip.String() != "203.0.113.7" {
		t.Errorf("ip = %s", ip)
	}
	b := make([]byte, 5)
	io.ReadFull(c, b)
	if string(b) != "hello" {
		t.Errorf("payload = %q", b)
	}
}

func TestV1(t *testing.T) {
	_, ip := pipe(t, []byte("PROXY TCP4 198.51.100.2 10.0.0.1 1234 443\r\n"))
	if ip.String() != "198.51.100.2" {
		t.Errorf("ip = %s", ip)
	}
}

func TestNoHeader(t *testing.T) {
	_, ip := pipe(t, nil)
	if !ip.IsLoopback() {
		t.Errorf("ip = %s", ip)
	}
}
