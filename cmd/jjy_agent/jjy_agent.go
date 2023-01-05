package main

import (
	"ehang.io/nps/client"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/install"
	"ehang.io/nps/lib/version"
	"flag"
	"fmt"
	"github.com/astaxie/beego/logs"
	"github.com/ccding/go-stun/stun"
	"github.com/kardianos/service"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	serverAddr = flag.String("server", "jjy-agent.jijyun.cn:9870", "Server addr (ip:port)")

	//configPath = flag.String("config", "", "Configuration file path")
	configPath = ""

	verifyKey = flag.String("agent_key", "", "Authentication key")

	//logType    = flag.String("log", "stdout", "Log output mode（stdout|file）")
	logType = "stdout"

	//connType       = flag.String("type", "tcp", "Connection type with the server（kcp|tcp）")
	connType = "tcp"

	//proxyUrl       = flag.String("proxy", "", "proxy socks5 url(eg:socks5://111:222@127.0.0.1:9007)")
	proxyUrl = ""

	//logLevel     = flag.String("log_level", "7", "log level 0~7")
	logLevel = "7"

	//registerTime = flag.Int("time", 2, "register time long /h")
	registerTime = 2

	//localPort      = flag.Int("local_port", 2000, "p2p local port")
	localPort = 2000

	//password       = flag.String("password", "", "p2p password flag")
	password = ""

	//target         = flag.String("target", "", "p2p target")
	target = ""

	//localType      = flag.String("local_type", "p2p", "p2p target")
	localType = "p2p"

	//logPath        = flag.String("log_path", "", "npc log path")
	logPath = ""

	//debug          = flag.Bool("debug", true, "npc debug")
	debug = false

	//pprofAddr      = flag.String("pprof", "", "PProf debug addr (ip:port)")
	pprofAddr = ""

	//stunAddr       = flag.String("stun_addr", "stun.stunprotocol.org:3478", "stun server address (eg:stun.stunprotocol.org:3478)")
	stunAddr = "stun.stunprotocol.org:3478"

	ver = flag.Bool("version", false, "show current version")

	//disconnectTime = flag.Int("disconnect_timeout", 60, "not receiving check packet times, until timeout will disconnect the client")
	disconnectTime = 60
)

func main() {
	flag.Parse()
	logs.Reset()
	logs.EnableFuncCallDepth(false)
	logs.SetLogFuncCallDepth(3)
	if *ver {
		common.PrintVersion()
		return
	}
	if logPath == "" {
		logPath = common.GetNpcLogPath()
	}
	if common.IsWindows() {
		logPath = strings.Replace(logPath, "\\", "\\\\", -1)
	}
	if debug {
		logs.SetLogger(logs.AdapterConsole, `{"level":`+logLevel+`,"color":true}`)
	} else {
		logs.SetLogger(logs.AdapterFile, `{"level":`+logLevel+`,"filename":"`+logPath+`","daily":false,"maxlines":100000,"color":true}`)
	}

	// init service
	options := make(service.KeyValue)
	svcConfig := &service.Config{
		Name:        "jjy_agent",
		DisplayName: "集简云内网代理客户端",
		Description: "适用于内网访问集简云Saas接口的内网代理方案",
		Option:      options,
	}
	if !common.IsWindows() {
		svcConfig.Dependencies = []string{
			"Requires=network.target",
			"After=network-online.target syslog.target"}
		svcConfig.Option["SystemdScript"] = install.SystemdScript
		svcConfig.Option["SysvScript"] = install.SysvScript
	}
	for _, v := range os.Args[1:] {
		switch v {
		case "install", "start", "stop", "uninstall", "restart":
			continue
		}
		if !strings.Contains(v, "-service=") && !strings.Contains(v, "-debug=") {
			svcConfig.Arguments = append(svcConfig.Arguments, v)
		}
	}
	//svcConfig.Arguments = append(svcConfig.Arguments, "-debug=false")
	prg := &jjy_agent{
		exit: make(chan struct{}),
	}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		logs.Error(err, "service function disabled")
		run()
		// run without service
		wg := sync.WaitGroup{}
		wg.Add(1)
		wg.Wait()
		return
	}
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "status":
			if len(os.Args) > 2 {
				path := strings.Replace(os.Args[2], "-config=", "", -1)
				client.GetTaskStatus(path)
			}
		case "register":
			flag.CommandLine.Parse(os.Args[2:])
			client.RegisterLocalIp(*serverAddr, *verifyKey, connType, proxyUrl, registerTime)
		//case "update":
		//	install.UpdateNpc()
		//	return
		case "nat":
			c := stun.NewClient()
			c.SetServerAddr(stunAddr)
			nat, host, err := c.Discover()
			if err != nil || host == nil {
				logs.Error("get nat type error", err)
				return
			}
			fmt.Printf("nat type: %s \npublic address: %s\n", nat.String(), host.String())
			os.Exit(0)
		case "start", "stop", "restart":
			// support busyBox and sysV, for openWrt
			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				cmd := exec.Command("/etc/init.d/"+svcConfig.Name, os.Args[1])
				err := cmd.Run()
				if err != nil {
					logs.Error(err)
				}
				return
			}
			err := service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			return
		case "install":
			service.Control(s, "stop")
			service.Control(s, "uninstall")
			install.InstallNpc()
			err := service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				confPath := "/etc/init.d/" + svcConfig.Name
				os.Symlink(confPath, "/etc/rc.d/S90"+svcConfig.Name)
				os.Symlink(confPath, "/etc/rc.d/K02"+svcConfig.Name)
			}
			return
		case "uninstall":
			err := service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				os.Remove("/etc/rc.d/S90" + svcConfig.Name)
				os.Remove("/etc/rc.d/K02" + svcConfig.Name)
			}
			return
		}
	}
	s.Run()
}

type jjy_agent struct {
	exit chan struct{}
}

func (p *jjy_agent) Start(s service.Service) error {
	go p.run()
	return nil
}
func (p *jjy_agent) Stop(s service.Service) error {
	close(p.exit)
	if service.Interactive() {
		os.Exit(0)
	}
	return nil
}

func (p *jjy_agent) run() error {
	defer func() {
		if err := recover(); err != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			logs.Warning("jjy_agent: panic serving %v: %v\n%s", err, string(buf))
		}
	}()
	run()
	select {
	case <-p.exit:
		logs.Warning("Stop jjy_agent ...")
	}
	return nil
}

func run() {
	//p2p or secret command
	logs.Info("The current version is %s", version.GetVersion())
	if *verifyKey != "" && *serverAddr != "" {
		go func() {
			for {
				client.NewRPClient(*serverAddr, *verifyKey, connType, proxyUrl, nil, disconnectTime).Start()
				logs.Info("The client fails to connect to the server. Try again after 10 seconds ")
				time.Sleep(time.Second * 10)
			}
		}()
	} else {
		logs.Error("Parameter error, missing agent_key ")
	}
}