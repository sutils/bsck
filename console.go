package bsck

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codingeasygo/bsck/dialer"
	"github.com/codingeasygo/util/proxy"
	"github.com/codingeasygo/util/proxy/socks"
	"github.com/codingeasygo/util/xhttp"
	"github.com/codingeasygo/util/xio"
)

//Console is node console to dial connection
type Console struct {
	Client        *xhttp.Client
	SlaverAddress string
	BufferSize    int
	Env           []string
	conns         map[string]net.Conn
	running       map[string]io.Closer
	locker        sync.RWMutex
}

//NewConsole will return new Console
func NewConsole(slaverAddress string) (console *Console) {
	console = &Console{
		SlaverAddress: slaverAddress,
		BufferSize:    32 * 1024,
		conns:         map[string]net.Conn{},
		running:       map[string]io.Closer{},
		locker:        sync.RWMutex{},
	}
	client := &http.Client{
		Transport: &http.Transport{
			Dial: console.dialNet,
		},
	}
	console.Client = xhttp.NewClient(client)
	return
}

//Close will close all connection
func (c *Console) Close() (err error) {
	all := []io.Closer{}
	c.locker.Lock()
	for _, conn := range c.conns {
		all = append(all, conn)
	}
	for _, conn := range c.running {
		all = append(all, conn)
	}
	c.locker.Unlock()
	for _, conn := range all {
		conn.Close()
	}
	return
}

func (c *Console) dialAll(uri string, raw io.ReadWriteCloser) (sid uint64, err error) {
	DebugLog("Console start dial to %v on slaver %v", uri, c.SlaverAddress)
	conn, err := socks.DialType(c.SlaverAddress, 0x05, uri)
	if err != nil {
		if waiter, ok := raw.(ReadyWaiter); ok {
			waiter.Ready(err, nil)
		}
		raw.Close()
		DebugLog("Console dial to %v on slaver %v fail with %v", uri, c.SlaverAddress, err)
		return
	}
	DebugLog("Console dial to %v on slaver %v success", uri, c.SlaverAddress)
	proc := func(err error) {
		if err != nil {
			conn.Close()
			raw.Close()
			return
		}
		go xio.CopyBuffer(conn, raw, make([]byte, c.BufferSize))
		xio.CopyBuffer(raw, conn, make([]byte, c.BufferSize))
		c.locker.Lock()
		delete(c.conns, fmt.Sprintf("%p", conn))
		c.locker.Unlock()
	}
	c.locker.Lock()
	c.conns[fmt.Sprintf("%p", conn)] = conn
	c.locker.Unlock()
	if waiter, ok := raw.(ReadyWaiter); ok {
		waiter.Ready(nil, proc)
	} else {
		go proc(nil)
	}
	return
}

//dialNet is net dialer to router
func (c *Console) dialNet(network, addr string) (conn net.Conn, err error) {
	addr = strings.TrimSuffix(addr, ":80")
	addr = strings.TrimSuffix(addr, ":443")
	if strings.HasPrefix(addr, "base64-") {
		var realAddr []byte
		realAddr, err = base64.RawURLEncoding.DecodeString(strings.TrimPrefix(addr, "base64-"))
		if err != nil {
			return
		}
		addr = string(realAddr)
	}
	conn, raw, err := dialer.CreatePipedConn()
	if err == nil {
		_, err = c.dialAll(addr, raw)
		if err != nil {
			conn.Close()
			raw.Close()
		}
	}
	return
}

//Redirect will redirect connection to reader/writer/closer
func (c *Console) Redirect(uri string, reader io.Reader, writer io.Writer, closer io.Closer) (err error) {
	raw := xio.NewCombinedReadWriteCloser(reader, writer, closer)
	_, err = c.dialAll(uri, raw)
	return
}

//Dial will dial connection by uri
func (c *Console) Dial(uri string) (conn io.ReadWriteCloser, err error) {
	conn, raw, err := dialer.CreatePipedConn()
	if err == nil {
		_, err = c.dialAll(uri, raw)
		if err != nil {
			conn.Close()
			raw.Close()
		}
	}
	return
}

//Ping will start ping to uri
func (c *Console) Ping(uri string, delay time.Duration, max uint64) (err error) {
	if !strings.Contains(uri, "tcp://echo") {
		if len(uri) > 0 {
			uri += "->"
		}
		uri += "tcp://echo"
	}
	var running bool = true
	var runCount uint64
	var conn io.ReadWriteCloser
	buf := make([]byte, 64)
	closer := xio.CloserF(func() (err error) {
		running = false
		if conn != nil {
			conn.Close()
		}
		return
	})
	c.locker.Lock()
	c.running[fmt.Sprintf("%p", closer)] = closer
	c.locker.Unlock()
	for running && (max < 1 || runCount < max) {
		runCount++
		pingStart := time.Now()
		conn, err = c.Dial(uri)
		if err != nil {
			fmt.Printf("Ping to %v fail with dial error %v\n", uri, err)
			time.Sleep(delay)
			continue
		}
		fmt.Fprintf(bytes.NewBuffer(buf), "%v", runCount)
		_, err = conn.Write(buf)
		if err != nil {
			fmt.Printf("Ping to %v fail with write error %v\n", uri, err)
			conn.Close()
			time.Sleep(delay)
			continue
		}
		err = xio.FullBuffer(conn, buf, 64, nil)
		if err != nil {
			fmt.Printf("Ping to %v fail with read error %v\n", uri, err)
			conn.Close()
			time.Sleep(delay)
			continue
		}
		pingUsed := time.Now().Sub(pingStart)
		fmt.Printf("%v Bytes from %v time=%v\n", len(buf), uri, pingUsed)
		conn.Close()
		time.Sleep(delay)
	}
	c.locker.Lock()
	delete(c.running, fmt.Sprintf("%p", closer))
	c.locker.Unlock()
	closer.Close()
	return
}

//PrintState will print state from uri
func (c *Console) PrintState(uri, query string) (err error) {
	if !strings.Contains(uri, "http://state") {
		if len(uri) > 0 {
			uri += "->"
		}
		uri += "http://state"
	}
	if query == "*" || len(query) < 1 {
		query = "*=*"
	}
	state, err := c.Client.GetMap(EncodeWebURI("http://(%v)?%v", uri, query))
	if err != nil {
		return
	}
	fmt.Printf("[Channels]\n")
	channels := state.Map("channels")
	for name := range channels {
		fmt.Printf(" ->%v\n", name)
		bond := channels.Map(name)
		for idx := range bond {
			val := bond.Map(idx)
			idxVal, _ := strconv.ParseInt(strings.Replace(idx, "_", "", -1), 10, 64)
			heartbeat := val.Int64Def(0, "heartbeat")
			hs := time.Unix(0, heartbeat*1e6).Format("2006-01-02 15:04:05")
			fmt.Printf("   %d % 4d   %v   %v\n", idxVal, int(val["used"].(float64)), hs, val["connect"])
		}
	}
	fmt.Printf("\n\n[Table]\n")
	table := state.ArrayStrDef(nil, "table")
	for _, t := range table {
		fmt.Printf(" %v\n", t)
	}
	fmt.Printf("\n")
	return
}

//DialPiper will dial uri on router and return piper
func (c *Console) DialPiper(uri string, bufferSize int) (raw xio.Piper, err error) {
	piper := NewWaitedPiper()
	_, err = c.dialAll(uri, piper)
	raw = piper
	return
}

func (c *Console) parseProxyURI(uri, target string) string {
	targetURI := uri
	if strings.HasPrefix(target, "tcp://tcp-") || strings.HasPrefix(target, "tcp://http-") || strings.HasPrefix(target, "tcp-") || strings.HasPrefix(target, "http-") {
		target = strings.TrimPrefix(target, "tcp://")
		target = strings.TrimSuffix(target, ":80")
		target = strings.TrimSuffix(target, ":443")
		parts := strings.SplitN(target, "-", 2)
		targetURI = strings.ReplaceAll(targetURI, "${HOST}", parts[1])
		targetURI = strings.ReplaceAll(targetURI, "${URI}", parts[0]+"://"+parts[1])
	} else if strings.Contains(target, ".") {
		targetURI = strings.ReplaceAll(targetURI, "${URI}", target)
		targetURI = strings.ReplaceAll(targetURI, "${HOST}", strings.TrimPrefix(target, "tcp://"))
	} else {
		target = strings.TrimPrefix(target, "tcp://")
		target = strings.TrimSuffix(target, ":80")
		target = strings.TrimSuffix(target, ":443")
		targetURI = strings.ReplaceAll(targetURI, "${HOST}", target)
		targetURI = strings.ReplaceAll(targetURI, "${URI}", "http://"+target)
	}
	return targetURI
}

//StartProxy will start proxy local to uri
func (c *Console) StartProxy(loc, uri string) (server *proxy.Server, listener net.Listener, err error) {
	server = proxy.NewServer(xio.PiperDialerF(func(target string, bufferSize int) (raw xio.Piper, err error) {
		targetURI := c.parseProxyURI(uri, target)
		raw, err = c.DialPiper(targetURI, bufferSize)
		return
	}))
	listener, err = server.Start(loc)
	return
}

//Proxy will start shell by runner and rewrite all tcp connection by console
func (c *Console) Proxy(uri string, process func(listener net.Listener) (err error)) (err error) {
	server, listener, err := c.StartProxy("127.0.0.1:0", uri)
	if err == nil {
		c.locker.Lock()
		c.running[fmt.Sprintf("%p", server)] = server
		c.running[fmt.Sprintf("%p", listener)] = listener
		c.locker.Unlock()
		err = process(listener)
		c.locker.Lock()
		delete(c.running, fmt.Sprintf("%p", server))
		delete(c.running, fmt.Sprintf("%p", listener))
		c.locker.Unlock()
		server.Close()
		listener.Close()
	}
	return
}

//ProxyExec will exec command by runner and rewrite all tcp connection by console
func (c *Console) ProxyExec(uri string, stdin io.Reader, stdout, stderr io.Writer, prepare func(listener net.Listener) (env []string, runner string, args []string, err error)) (err error) {
	err = c.Proxy(uri, func(listener net.Listener) (err error) {
		env, runner, args, err := prepare(listener)
		cmd := exec.Command(runner, args...)
		cmd.Env = append(cmd.Env, c.Env...)
		cmd.Env = append(cmd.Env, env...)
		// InfoLog("Console proxy command %v by\narg:%v\nenv:%v\n", runner, args, cmd.Env)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
		c.locker.Lock()
		c.running[fmt.Sprintf("%p", cmd)] = xio.CloserF(cmd.Process.Kill)
		c.locker.Unlock()
		err = cmd.Run()
		c.locker.Lock()
		delete(c.running, fmt.Sprintf("%p", cmd))
		c.locker.Unlock()
		return
	})
	return
}

//ProxyProcess will start process by runner and rewrite all tcp connection by console
func (c *Console) ProxyProcess(uri string, stdin, stdout, stderr *os.File, prepare func(listener net.Listener) (env []string, runner string, args []string, err error)) (err error) {
	err = c.Proxy(uri, func(listener net.Listener) (err error) {
		env, runner, args, err := prepare(listener)
		// InfoLog("Console proxy process %v by\narg:%v\nenv:%v\n", runner, args, env)
		process, err := os.StartProcess(runner, append([]string{runner}, args...), &os.ProcAttr{
			Env:   append(c.Env, env...),
			Files: []*os.File{stdin, stdout, stderr},
		})
		if err == nil {
			c.locker.Lock()
			c.running[fmt.Sprintf("%p", process)] = xio.CloserF(process.Kill)
			c.locker.Unlock()
			_, err = process.Wait()
			c.locker.Lock()
			delete(c.running, fmt.Sprintf("%p", process))
			c.locker.Unlock()
		}
		return
	})
	return
}

//ProxySSH will start ssh client command and connect to uri by proxy command.
//
//it will try load the ssh key from slaver forwarding by bs-ssh-key, bs-ssh-key is forwarding to http server and return ssh key in body by uri argument.
//
func (c *Console) ProxySSH(uri string, stdin io.Reader, stdout, stderr io.Writer, proxyCommand, command string, args ...string) (err error) {
	replaceURI := uri
	if len(replaceURI) < 1 {
		replaceURI = "tcp://127.0.0.1:22"
	}
	replaceURI = strings.ReplaceAll(replaceURI, "->", "_")
	replaceURI = strings.ReplaceAll(replaceURI, "://", "_")
	replaceURI = strings.ReplaceAll(replaceURI, ":", "_")
	for i, a := range args {
		args[i] = strings.ReplaceAll(a, "bshost", replaceURI)
	}
	if !strings.Contains(uri, "tcp://") {
		if len(uri) > 0 {
			uri += "->"
		}
		uri += "tcp://127.0.0.1:22"
	}
	allArgs := []string{}
	allArgs = append(allArgs, "-o", fmt.Sprintf("ProxyCommand=%v", strings.ReplaceAll(proxyCommand, "${URI}", uri)))
	//
	sshKey, err := c.Client.GetBytes("http://ssh-key?uri=%v", url.QueryEscape(uri))
	if err == nil {
		tempFile, tempErr := ioutil.TempFile("", "*")
		if tempErr != nil {
			err = fmt.Errorf("SSH create temp file fail with %v", tempErr)
			return
		}
		defer os.Remove(tempFile.Name())
		tempFile.Write(sshKey)
		tempFile.Close()
		allArgs = append(allArgs, "-i", tempFile.Name())
		InfoLog("Console proxy ssh using remote ssh key from %v", uri)
	}
	if command == "ssh" {
		allArgs = append(allArgs, replaceURI)
	}
	allArgs = append(allArgs, args...)
	cmd := exec.Command(command, allArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	cmd.Env = c.Env
	c.locker.Lock()
	c.running[fmt.Sprintf("%p", cmd)] = xio.CloserF(cmd.Process.Kill)
	c.locker.Unlock()
	err = cmd.Run()
	c.locker.Lock()
	delete(c.running, fmt.Sprintf("%p", cmd))
	c.locker.Unlock()
	return
}
