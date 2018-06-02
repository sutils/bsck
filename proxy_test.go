package bsck

import (
	"encoding/binary"
	"fmt"
	"log"
	"testing"
	"time"
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	ShowLog = 2
}

type Echo struct {
	Data string
	Recv int
	Send int
	Err  error
	W    chan int
	R    chan int
}

func NewEcho(data string) *Echo {
	return &Echo{
		Data: data,
		W:    make(chan int),
		R:    make(chan int, 100),
	}
}

func (e *Echo) Write(p []byte) (n int, err error) {
	if e.Err != nil {
		err = e.Err
		return
	}
	n = len(p)
	fmt.Printf("RECV:%v\n", string(p))
	e.Recv++
	e.R <- 1
	return
}

func (e *Echo) Read(b []byte) (n int, err error) {
	if e.Err != nil {
		err = e.Err
		return
	}
	<-e.W
	copy(b, []byte(e.Data))
	n = len(e.Data)
	e.Send++
	return
}

func (e *Echo) Close() error {
	if e.Err == nil {
		e.Err = fmt.Errorf("closed")
		fmt.Printf("%v echo is closed\n", e.Data)
		close(e.W)
		e.R <- 1
	}
	return nil
}

func TestProxy(t *testing.T) {
	master := NewProxy("master")
	master.Router.ACL["ms"] = "abc"
	master.Router.ACL["slaver"] = "abc"
	master.Router.ACL["slaver2"] = "abc"
	master.Router.ACL["slaver3"] = "abc"
	master.Heartbeat = 10 * time.Millisecond
	master.StartHeartbeat()
	var masterEcho *Echo
	master.Router.DialRaw = func(sid uint64, uri string) (conn Conn, err error) {
		fmt.Println("master test dail to ", uri)
		if uri == "error" {
			err = fmt.Errorf("error")
		} else {
			conn = NewRawConn(masterEcho, sid, uri)
		}
		// err = fmt.Errorf("error")
		return
	}
	err := master.Listen(":9232")
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		master.Close()
		time.Sleep(time.Second)
	}()
	//
	slaver := NewRouter("slaver")
	slaver.Heartbeat = 10 * time.Millisecond
	slaver.StartHeartbeat()
	var slaverEcho *Echo
	slaver.DialRaw = func(sid uint64, uri string) (conn Conn, err error) {
		fmt.Println("slaver test dail to ", uri)
		conn = NewRawConn(slaverEcho, sid, uri)
		// err = fmt.Errorf("error")
		return
	}
	slaver.Login("", "localhost:9232", "abc", 0)
	//
	slaver2 := NewRouter("slaver2")
	slaver2.Login("", "localhost:9232", "abc", 0)

	{ //test slaver2->master->server
		fmt.Printf("\n\n\ntest slaver2->master->server\n")
		masterEcho = NewEcho("master")
		slaver2Echo := NewEcho("client")
		_, err = slaver2.Dial("master->xx", slaver2Echo)
		if err != nil {
			t.Error(err)
			return
		}
		//client->server
		slaver2Echo.W <- 1
		<-masterEcho.R
		slaver2Echo.W <- 1
		<-masterEcho.R
		//server->client
		masterEcho.W <- 1
		<-slaver2Echo.R
		masterEcho.W <- 1
		<-slaver2Echo.R
		//close
		slaver2Echo.Close()
		<-masterEcho.R
	}
	{ //test slaver2->master->slaver->server
		fmt.Printf("\n\n\ntest slaver2->master->slaver->server\n")
		slaverEcho = NewEcho("slaver")
		slaver2Echo := NewEcho("client")
		_, err = slaver2.Dial("master->slaver->xx", slaver2Echo)
		if err != nil {
			t.Error(err)
			return
		}
		//client->server
		slaver2Echo.W <- 1
		<-slaverEcho.R
		slaver2Echo.W <- 1
		<-slaverEcho.R
		// server->client
		slaverEcho.W <- 1
		<-slaver2Echo.R
		slaverEcho.W <- 1
		<-slaver2Echo.R
		//close
		slaver2Echo.Close()
		<-slaverEcho.R
	}
	{ //multi channel
		fmt.Printf("\n\n\ntest multi channel\n")
		var msEcho *Echo
		ms0 := NewRouter("ms")
		ms0.DialRaw = func(sid uint64, uri string) (conn Conn, err error) {
			fmt.Println("ms test dail to ", uri)
			conn = NewRawConn(msEcho, sid, uri)
			return
		}
		err = ms0.LoginChannel(&ChannelOption{
			Token:  "abc",
			Local:  "0.0.0.0:0",
			Remote: "localhost:9232",
		}, &ChannelOption{
			Token:  "abc",
			Local:  "0.0.0.0:0",
			Remote: "localhost:9232",
		})
		if err != nil {
			t.Error(err)
			return
		}
		for i := 0; i < 10; i++ {
			msEcho = NewEcho("ms")
			slaver2Echo := NewEcho("client")
			_, err = slaver2.Dial("master->ms->xx", slaver2Echo)
			if err != nil {
				t.Error(err)
				return
			}
			//client->server
			slaver2Echo.W <- 1
			<-msEcho.R
			slaver2Echo.W <- 1
			<-msEcho.R
			// server->client
			msEcho.W <- 1
			<-slaver2Echo.R
			msEcho.W <- 1
			<-slaver2Echo.R
			//close
			slaver2Echo.Close()
			<-msEcho.R
		}
	}
	{ //channel close
		fmt.Printf("\n\n\ntest channel close\n")
		slaver3Echo := NewEcho("slaver3")
		slaver3 := NewRouter("slaver3")
		slaver3.DialRaw = func(sid uint64, uri string) (conn Conn, err error) {
			fmt.Println("slaver3 test dail to ", uri)
			conn = NewRawConn(slaver3Echo, sid, uri)
			return
		}
		slaver3.Login("", "localhost:9232", "abc", 0)
		slaver2Echo := NewEcho("client")
		_, err = slaver2.Dial("master->slaver3->xx", slaver2Echo)
		if err != nil {
			t.Error(err)
			return
		}
		//client->server
		slaver2Echo.W <- 1
		<-slaver3Echo.R
		slaver2Echo.W <- 1
		<-slaver3Echo.R
		// server->client
		slaver3Echo.W <- 1
		<-slaver2Echo.R
		slaver3Echo.W <- 1
		<-slaver2Echo.R
		//close
		slaver3.SelectChannel("master").Close()
		<-slaver3Echo.R
		<-slaver2Echo.R
	}
	{ //dial remote fail
		slaver2Echo := NewEcho("client")
		_, err = slaver2.Dial("master->error", slaver2Echo)
		if err != nil {
			t.Error(err)
			return
		}
		<-slaver2Echo.R
		if slaver2Echo.Recv != 0 {
			t.Error("error")
			return
		}
	}
}

func TestError(t *testing.T) {
	master := NewProxy("master")
	master.Router.ACL["ms"] = "abc"
	var masterEcho *Echo
	master.Router.DialRaw = func(sid uint64, uri string) (conn Conn, err error) {
		fmt.Println("master test dail to ", uri)
		if uri == "error" {
			err = fmt.Errorf("error")
		} else {
			conn = NewRawConn(masterEcho, sid, uri)
		}
		// err = fmt.Errorf("error")
		return
	}
	err := master.Listen(":9232")
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		master.Close()
		time.Sleep(time.Second)
	}()
	//
	// slaver := NewRouter("slaver")
	// var slaverEcho *Echo
	// slaver.DialRaw = func(sid uint64, uri string) (conn Conn, err error) {
	// 	fmt.Println("slaver test dail to ", uri)
	// 	conn = NewRawConn(slaverEcho, sid, uri, 4096)
	// 	// err = fmt.Errorf("error")
	// 	return
	// }
	// slaver.Login("", "localhost:9232", "abc", 0)
	//
	{ //test login error
		fmt.Printf("\n\n\ntest login error\n")
		slaverErr := NewRouter("error")
		err = slaverErr.Login("", "localhost:9232", "abc", 0)
		if err == nil {
			t.Error("error")
			return
		}
		slaverErr = NewRouter("error")
		err = slaverErr.Login("", "localhost:9232", "", 0)
		if err == nil {
			t.Error("error")
			return
		}
	}
	{ //login/dial error
		fmt.Printf("\n\n\ntest login/dial error\n")
		//configer error
		masterEcho = NewEcho("master")
		testc := &Channel{
			ReadWriteCloser: NewEcho("testing"),
			name:            "xx",
		}
		testc.Close()
		master.Router.addChannel(testc)
		//
		//test proc login fail
		err = master.Router.procLogin(NewRawConn(NewEcho("data"), 0, "ur"), make([]byte, 1024), 1024)
		if err == nil {
			t.Error("error")
			return
		}
		//
		echo := NewEcho("data")
		err = master.Router.procLogin(&Channel{ReadWriteCloser: echo}, make([]byte, 1024), 1024)
		if err != nil || echo.Recv != 1 {
			t.Error(err)
			return
		}
		//
		//test proc dail fail
		buf := make([]byte, 1024)
		data := []byte{}
		//
		data = []byte("url")
		copy(buf[13:], data)
		echo = NewEcho("data")
		err = master.Router.procDial(&Channel{ReadWriteCloser: echo}, buf, 16)
		if err != nil || echo.Recv != 1 {
			t.Error(err)
			return
		}
		//
		data = []byte("x@error")
		copy(buf[13:], data)
		echo = NewEcho("data")
		err = master.Router.procDial(&Channel{ReadWriteCloser: echo}, buf, uint32(len(data)+13))
		if err != nil || echo.Recv != 1 {
			t.Error(err)
			return
		}
		//
		data = []byte("x@not->error")
		copy(buf[13:], data)
		echo = NewEcho("data")
		err = master.Router.procDial(&Channel{ReadWriteCloser: echo}, buf, uint32(len(data)+13))
		if err != nil || echo.Recv != 1 {
			t.Error(err)
			return
		}
		//
		data = []byte("x@xx->error")
		copy(buf[13:], data)
		echo = NewEcho("data")
		err = master.Router.procDial(&Channel{ReadWriteCloser: echo}, buf, uint32(len(data)+13))
		if err != nil || echo.Recv != 1 {
			t.Error(err)
			return
		}
		//
		//test dial error
		_, err = master.Router.Dial("uri", NewEcho("testing"))
		if err == nil {
			t.Error(err)
			return
		}
		_, err = master.Router.Dial("not->abc", NewEcho("testing"))
		if err == nil {
			t.Error(err)
			return
		}
		_, err = master.Router.Dial("xx->abc", NewEcho("testing"))
		if err == nil {
			t.Error(err)
			return
		}
		//
		//test login error
		slaver := NewRouter("slaver")
		err = slaver.LoginChannel(&ChannelOption{
			Token:  "abc",
			Remote: "loclahost:12",
		})
		if err == nil {
			t.Error(err)
			return
		}
		err = slaver.Login("xxx", "localhost:9232", "abc", 0)
		if err == nil {
			t.Error(err)
			return
		}
		err = slaver.Login("", "localhost:12", "abc", 0)
		if err == nil {
			t.Error(err)
			return
		}
		merr := NewErrReadWriteCloser([]byte("abc"), 10)
		merr.ErrType = 10
		err = slaver.JoinConn(merr, "abc", 0)
		if err == nil {
			t.Error(err)
			return
		}
		merr.ErrType = 20
		err = slaver.JoinConn(merr, "abc", 0)
		if err == nil {
			t.Error(err)
			return
		}
	}
	{ //test proc dial back error
		fmt.Printf("\n\n\ntest proc dail back error\n")
		//
		//test dial fail
		srcRaw := NewErrReadWriteCloser([]byte("error"), 10)
		src := &Channel{ReadWriteCloser: srcRaw}
		dstRaw := NewErrReadWriteCloser([]byte("error"), 10)
		dst := &Channel{ReadWriteCloser: dstRaw}
		master.Router.addTable(src, 1000, dst, 1001)
		buf := make([]byte, 1024)
		copy(buf[13:], []byte("error"))
		//
		binary.BigEndian.PutUint64(buf[5:], 1000)
		err = master.Router.procDialBack(src, buf, 18)
		if err != srcRaw.Err {
			t.Error(err)
			return
		}
		//
		binary.BigEndian.PutUint64(buf[5:], 2000)
		err = master.Router.procDialBack(src, buf, 18)
		if err != srcRaw.Err {
			t.Error(err)
			return
		}
	}
	{ //test loop read raw fail
		buf := make([]byte, 1024)
		//length error
		binary.BigEndian.PutUint32(buf, 6)
		srcRaw := NewErrReadWriteCloser(buf[0:10], 0)
		src := &Channel{ReadWriteCloser: srcRaw}
		master.Router.loopReadRaw(src, 10240)
		//read cmd error
		binary.BigEndian.PutUint32(buf, 100)
		srcRaw = NewErrReadWriteCloser(buf[0:104], 0)
		src = &Channel{ReadWriteCloser: srcRaw}
		master.Router.loopReadRaw(src, 50)
		//cmd error
		binary.BigEndian.PutUint32(buf, 100)
		srcRaw = NewErrReadWriteCloser(buf[0:104], 0)
		src = &Channel{ReadWriteCloser: srcRaw}
		master.Router.loopReadRaw(src, 10240)
	}
	{ //test for cover
		rawConn := NewRawConn(NewEcho("data"), 0, "")
		rawConn.Index()
		func() {
			defer func() {
				recover()
			}()
			rawConn.Read(nil)
		}()
		cmdString(CmdLoginBack)
		echo := NewErrReadWriteCloser([]byte("data"), 0)
		fullBuf(echo, make([]byte, 1024), 8, nil)
	}
}

type ErrReadWriteCloser struct {
	Err     error
	ErrType int
	Data    []byte
}

func NewErrReadWriteCloser(data []byte, errType int) *ErrReadWriteCloser {
	return &ErrReadWriteCloser{
		Data:    data,
		ErrType: errType,
		Err:     fmt.Errorf("mock error"),
	}
}

func (e *ErrReadWriteCloser) Write(p []byte) (n int, err error) {
	if e.ErrType == 10 {
		err = e.Err
	}
	n = len(p)
	fmt.Println("RECV:", string(p))
	return
}

func (e *ErrReadWriteCloser) Read(b []byte) (n int, err error) {
	if e.ErrType == 20 {
		err = e.Err
	}
	n = len(e.Data)
	copy(b, e.Data)
	return
}

func (e *ErrReadWriteCloser) Close() (err error) {
	if e.ErrType == 30 {
		err = e.Err
	}
	return
}

func TestDialTCP(t *testing.T) {
	_, err := DialTCP(10, "tcp://localhost:80")
	if err != nil {
		t.Error("error")
		return
	}
	_, err = DialTCP(10, "tcp:localhost:80")
	if err == nil {
		t.Error("error")
		return
	}
	_, err = DialTCP(10, "tcp://localhost:80%EX%B8%AD%E6%96%87")
	if err == nil {
		t.Error("error")
		return
	}
}
