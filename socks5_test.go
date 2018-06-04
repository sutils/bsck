package bsck

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/sutils/dialer"
)

func TestPendingConn(t *testing.T) {
	pending := NewPendingConn(&Echo{})
	go pending.Write(make([]byte, 10))
	time.Sleep(100 * time.Millisecond)
	pending.Start()
	time.Sleep(100 * time.Millisecond)
}

func proxDial(t *testing.T, remote string, port uint16) {
	conn, err := net.Dial("tcp", "localhost:1081")
	if err != nil {
		t.Error(err)
		return
	}
	buf := make([]byte, 1024*64)
	proxyReader := bufio.NewReader(conn)
	_, err = conn.Write([]byte{0x05, 0x01, 0x00})
	if err != nil {
		return
	}
	err = fullBuf(proxyReader, buf, 2, nil)
	if err != nil {
		return
	}
	if buf[0] != 0x05 || buf[1] != 0x00 {
		err = fmt.Errorf("only ver 0x05 / method 0x00 is supported, but %x/%x", buf[0], buf[1])
		return
	}
	buf[0], buf[1], buf[2], buf[3] = 0x05, 0x01, 0x00, 0x03
	buf[4] = byte(len(remote))
	copy(buf[5:], []byte(remote))
	binary.BigEndian.PutUint16(buf[5+len(remote):], port)
	_, err = conn.Write(buf[:buf[4]+7])
	if err != nil {
		return
	}
	readed, err := proxyReader.Read(buf)
	if err != nil {
		return
	}
	fmt.Printf("->%v\n", buf[0:readed])
}

func proxDialIP(t *testing.T, bys []byte, port uint16) {
	conn, err := net.Dial("tcp", "localhost:1081")
	if err != nil {
		t.Error(err)
		return
	}
	buf := make([]byte, 1024*64)
	proxyReader := bufio.NewReader(conn)
	_, err = conn.Write([]byte{0x05, 0x01, 0x00})
	if err != nil {
		return
	}
	err = fullBuf(proxyReader, buf, 2, nil)
	if err != nil {
		return
	}
	if buf[0] != 0x05 || buf[1] != 0x00 {
		err = fmt.Errorf("only ver 0x05 / method 0x00 is supported, but %x/%x", buf[0], buf[1])
		return
	}
	buf[0], buf[1], buf[2], buf[3] = 0x05, 0x01, 0x00, 0x01
	copy(buf[4:], bys)
	binary.BigEndian.PutUint16(buf[8:], port)
	_, err = conn.Write(buf[:10])
	if err != nil {
		return
	}
	readed, err := proxyReader.Read(buf)
	if err != nil {
		return
	}
	fmt.Printf("->%v\n", buf[0:readed])
}

func proxDialIPv6(t *testing.T, bys []byte, port uint16) {
	conn, err := net.Dial("tcp", "localhost:1081")
	if err != nil {
		t.Error(err)
		return
	}
	buf := make([]byte, 1024*64)
	proxyReader := bufio.NewReader(conn)
	_, err = conn.Write([]byte{0x05, 0x01, 0x00})
	if err != nil {
		return
	}
	err = fullBuf(proxyReader, buf, 2, nil)
	if err != nil {
		return
	}
	if buf[0] != 0x05 || buf[1] != 0x00 {
		err = fmt.Errorf("only ver 0x05 / method 0x00 is supported, but %x/%x", buf[0], buf[1])
		return
	}
	buf[0], buf[1], buf[2], buf[3] = 0x05, 0x01, 0x00, 0x04
	copy(buf[4:], bys)
	binary.BigEndian.PutUint16(buf[8:], port)
	_, err = conn.Write(buf[:10])
	if err != nil {
		return
	}
	readed, err := proxyReader.Read(buf)
	if err != nil {
		return
	}
	fmt.Printf("->%v\n", buf[0:readed])
}

func TestSocksProxy(t *testing.T) {
	proxy := NewSocksProxy()
	proxy.Dailer = func(uri string, raw io.ReadWriteCloser) (sid uint64, err error) {
		conn, err := net.Dial("tcp", uri)
		if err == nil {
			go io.Copy(conn, raw)
			go io.Copy(raw, conn)
			time.Sleep(100 * time.Millisecond)
		}
		fmt.Println("dial to ", uri, err)
		return
	}
	err := proxy.Start(":1081")
	if err != nil {
		t.Error(err)
		return
	}
	proxDial(t, "localhost", 80)
	proxDial(t, "localhost", 81)
	proxDialIP(t, make([]byte, 4), 80)
	proxDialIPv6(t, make([]byte, 16), 80)
	{ //test error
		//
		conn, conb, _ := dialer.CreatePipedConn()
		go proxy.procConn(conb)
		conn.Close()
		//
		conn, conb, _ = dialer.CreatePipedConn()
		go proxy.procConn(conb)
		conn.Write([]byte{0x00, 0x00})
		conn.Close()
		//
		conn, conb, _ = dialer.CreatePipedConn()
		go proxy.procConn(conb)
		conn.Write([]byte{0x05, 0x01})
		conn.Close()
		//
		conn, conb, _ = dialer.CreatePipedConn()
		go proxy.procConn(conb)
		conn.Write([]byte{0x05, 0x01, 0x00})
		conn.Close()
		//
		conn, conb, _ = dialer.CreatePipedConn()
		go proxy.procConn(conb)
		conn.Write([]byte{0x05, 0x01, 0x00})
		conn.Read(make([]byte, 1024))
		conn.Close()
		//
		conn, conb, _ = dialer.CreatePipedConn()
		go proxy.procConn(conb)
		conn.Write([]byte{0x05, 0x01, 0x00})
		conn.Read(make([]byte, 1024))
		conn.Write([]byte{0x00, 0x01, 0x00, 0x00, 0x00})
		conn.Close()
		//
		conn, conb, _ = dialer.CreatePipedConn()
		go proxy.procConn(conb)
		conn.Write([]byte{0x05, 0x01, 0x00})
		conn.Read(make([]byte, 1024))
		buf := []byte{0x05, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x010}
		binary.BigEndian.PutUint16(buf[8:], 80)
		conn.Write(buf)
		conn.Close()
		time.Sleep(time.Second)
	}
	proxy.Close()
}