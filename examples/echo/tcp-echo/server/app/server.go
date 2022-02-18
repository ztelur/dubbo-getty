/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	// "flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"

	// "strings"
	"syscall"
	"time"
)

import (
	gxnet "github.com/AlexStocks/goext/net"
	getty "github.com/apache/dubbo-getty"
	"github.com/dubbogo/gost/sync"
)

const (
	pprofPath = "/debug/pprof/"
)

var (
// host  = flag.String("host", "127.0.0.1", "local host address that server app will use")
// ports = flag.String("ports", "12345,12346,12347", "local host port list that the server app will bind")
)

var (
	serverList []getty.Server
	taskPool   gxsync.GenericTaskPool
	log        = getty.GetLogger()
)

func main() {
	// flag.Parse()
	// if *host == "" || *ports == "" {
	// 	panic(fmt.Sprintf("Please intput local host ip or port lists"))
	// }

	initConf()

	initProfiling()

	taskPool = gxsync.NewTaskPoolSimple(0)
	initServer()
	log.Infof("%s starts successfull! its listen ends=%s:%s",
		conf.AppName, conf.Host, conf.Ports)

	initSignal()
}

func initProfiling() {
	var addr string

	// addr = *host + ":" + "10000"
	addr = gxnet.HostAddress(conf.Host, conf.ProfilePort)
	log.Infof("App Profiling startup on address{%v}", addr+pprofPath)
	go func() {
		log.Info(http.ListenAndServe(addr, nil))
	}()
}

func newSession(session getty.Session) error {
	var (
		ok      bool
		tcpConn *net.TCPConn
	)

	if conf.GettySessionParam.CompressEncoding {
		session.SetCompressType(getty.CompressZip)
	}
	// 拿到原本的 TCPConn 进行参数预先设置
	if tcpConn, ok = session.Conn().(*net.TCPConn); !ok {
		panic(fmt.Sprintf("%s, session.conn{%#v} is not tcp connection\n", session.Stat(), session.Conn()))
	}
	// 是否启动 delay，尽可能少发 tcp 包，对小包场景并且低延迟场景不友好
	tcpConn.SetNoDelay(conf.GettySessionParam.TcpNoDelay)
	tcpConn.SetKeepAlive(conf.GettySessionParam.TcpKeepAlive)
	// 如果需要 keep alive 则设置时间
	if conf.GettySessionParam.TcpKeepAlive {
		tcpConn.SetKeepAlivePeriod(conf.GettySessionParam.keepAlivePeriod)
	}
	// 读和写的 buffer
	tcpConn.SetReadBuffer(conf.GettySessionParam.TcpRBufSize)
	tcpConn.SetWriteBuffer(conf.GettySessionParam.TcpWBufSize)

	session.SetName(conf.GettySessionParam.SessionName)
	// 设置最大的消息数量
	session.SetMaxMsgLen(conf.GettySessionParam.MaxMsgLen)
	// 设置数据交互层，也就是 write 和 read
	session.SetPkgHandler(echoPkgHandler)
	// 设置监控接口，也就是OnOpen,OnError,OnClose,OnMessage,OnCron
	session.SetEventListener(echoMsgHandler)
	// 读写 timeout
	session.SetReadTimeout(conf.GettySessionParam.tcpReadTimeout)
	session.SetWriteTimeout(conf.GettySessionParam.tcpWriteTimeout)
	// 心跳
	session.SetCronPeriod((int)(conf.sessionTimeout.Nanoseconds() / 1e6))
	session.SetWaitTime(conf.GettySessionParam.waitTimeout)
	// session.SetTaskPool(taskPool)
	log.Debugf("app accepts new session:%s", session.Stat())

	return nil
}

func initServer() {
	var (
		addr     string
		portList []string
		server   getty.Server
	)

	// if *host == "" {
	// 	panic("host can not be nil")
	// }
	// if *ports == "" {
	// 	panic("ports can not be nil")
	// }

	// portList = strings.Split(*ports, ",")

	portList = conf.Ports
	if len(portList) == 0 {
		panic("portList is nil")
	}
	for _, port := range portList {
		addr = gxnet.HostAddress2(conf.Host, port)
		// options 实现
		serverOpts := []getty.ServerOption{getty.WithLocalAddress(addr)}
		serverOpts = append(serverOpts, getty.WithServerTaskPool(taskPool))
		server = getty.NewTCPServer(serverOpts...)
		// run server
		server.RunEventLoop(newSession)
		log.Debugf("server bind addr{%s} ok!", addr)
		serverList = append(serverList, server)
	}
}

func uninitServer() {
	for _, server := range serverList {
		server.Close()
	}
	if taskPool != nil {
		taskPool.Close()
	}
}

func initSignal() {
	// signal.Notify的ch信道是阻塞的(signal.Notify不会阻塞发送信号), 需要设置缓冲
	signals := make(chan os.Signal, 1)
	// It is not possible to block SIGKILL or syscall.SIGSTOP
	signal.Notify(signals, os.Interrupt, os.Kill, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	for {
		sig := <-signals
		log.Infof("get signal %s", sig.String())
		switch sig {
		case syscall.SIGHUP:
		// reload()
		default:
			go time.AfterFunc(conf.failFastTimeout, func() {
				// log.Warn("app exit now by force...")
				// os.Exit(1)
				log.Info("app exit now by force...")
			})

			// 要么fastFailTimeout时间内执行完毕下面的逻辑然后程序退出，要么执行上面的超时函数程序强行退出
			uninitServer()
			// fmt.Println("app exit now...")
			log.Info("app exit now...")
			return
		}
	}
}
