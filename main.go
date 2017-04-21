package main

import (
	"flag"
	"fmt"
	"runtime"
)

/*
通过命令行命令对网络主机上的两个目录进行同步。
1. 通过server参数，创建一个同步服务器，通过配置文件定义多个同步目录
2. 通过create参数，在同步服务器上创建一个同步目录
3. 通过delete参数，在同步服务器上取消一个同步目录
4. 通过list参数，列出同步服务器上的同步目录列表
5. 通过get和push参数，向同步服务器拉取和推送目录
*/

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	// 定义flag
	flagServer := flag.Bool("server", false, "启动一个同步服务器, 服务端命令。")
	flagIP := flag.String("ip", "127.0.0.1", "服务器ip，客户端命令。")
	flagPort := flag.Uint("port", 5555, "服务器端口，客户端、服务端命令。")
	flag.Parse()

	if *flagServer {
		// 服务器模式

		fmt.Println("启动服务器中...\n端口:", *flagPort)
		var server tServer
		server.start(*flagPort)

		return
	}

	// 客户端模式

	fmt.Println(flag.Args())
	if flag.NArg() < 1 {
		fmt.Println("缺少命令。")
		return
	}
	args := flag.Args()
	command := args[0]
	client := tClient{ip: *flagIP, port: *flagPort}

	switch command {
	case "create":
		fmt.Println("命令行: sync [-ip=xxx.xxx.xxx.xxx] [-port=xxxx] create <name> <directory>")
		if flag.NArg() != 3 {
			fmt.Println("缺少参数。")
			return
		}

		client.createSyncNode(args[1], args[2])
	case "push":
		fmt.Println("命令行: sync [-ip=xxx.xxx.xxx.xxx] [-port=xxxx] create <name> <directory>")
		if flag.NArg() != 3 {
			fmt.Println("缺少参数。")
			return
		}
		client.pushSyncNode(args[1], args[2])
	default:
		fmt.Println("非法的命令:", command)
		return
	}

}
