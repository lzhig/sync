package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"globaltedinc/framework/network"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
)

func md5file(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}

	m := md5.New()
	io.Copy(m, f)
	f.Close()
	return hex.EncodeToString(m.Sum(nil)), nil
}

type tNode struct {
	name     string
	dir      string
	filesMD5 map[string]string // key为文件名，相对于dir；value为文件md5
}

type tNodes map[string]*tNode // key为tNode中的dir

func (node *tNode) CalculateMD5() error {
	node.filesMD5 = make(map[string]string)
	if err := filepath.Walk(node.dir, func(_path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			fmt.Println("Dir:", _path)
		} else {
			rel, err := filepath.Rel(node.dir, _path)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			fmt.Println("File:", rel)

			m, err := md5file(filepath.Join(node.dir, rel))
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			node.filesMD5[rel] = m
			fmt.Println("md5:", m)
		}

		return nil
	}); err != nil {
		fmt.Println(err)
		return err
	}
	return nil
}

type tServer struct {
	network network.TCPServer

	nodes tNodes
}

func (server *tServer) start(port uint) {

	err := server.network.Start(
		fmt.Sprintf("0.0.0.0:%d", port),
		1000,
		server.onClientConnected,
		server.onClientDisconnected,
		server.onClientMessage)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer server.network.Stop()

	server.nodes = make(tNodes)

	select {}
}

func (server *tServer) onClientConnected(conn *network.Connection) {
	glog.Info("client " + conn.RemoteAddr() + " connected")
}

func (server *tServer) onClientDisconnected(conn *network.Connection, err error) {
	glog.Info("client " + conn.RemoteAddr() + " disconnected")
	glog.Info(err)
}

func (server *tServer) onClientMessage(conn *network.Connection, packet *network.Packet) {
	fmt.Println("onClientMessage")
	msg := &OpC2S{}
	if err := proto.Unmarshal(packet.GetData(), msg); err != nil {
		server.network.Disconnect(conn)
		glog.Error("error:", err)
		return
	}

	fmt.Println("msg:", msg)

	switch msg.Op {
	case OpID_OP_CREATE_SYNC_NODE:
		ret := OpS2C{
			Op:  msg.Op,
			Err: ErrorID_E_OK,
		}

		// 遍历消息体中的同步节点
		for _, v := range msg.CreateSyncNode.Nodes.Nodes {
			// 判断此目录是否已经存在现有的同步
			if _, exist := server.nodes[v.Directory]; exist {
				ret.Err = ErrorID_E_ALREADY_EXIST
				break
			}

			// 添加
			server.nodes[v.Directory] = &tNode{name: v.Name, dir: v.Directory}

			// 判断目录是否存在，如果不存在，则创建
			if _, err := os.Stat(v.Directory); os.IsNotExist(err) {
				fmt.Println("not exist")
				os.Mkdir(v.Directory, os.ModePerm)
			} else {
				// 遍历目录，并计算所有文件的md5值
				server.nodes[v.Directory].CalculateMD5()
			}
		}

		data, err := proto.Marshal(&ret)
		if err != nil {
			glog.Error("proto marshal error:", err, ". msg:", ret)
			return
		}

		var p network.Packet
		p.Attach(data)
		server.network.SendPacket(conn, &p)

	case OpID_OP_PUSH_SYNC_NODE:
		ret := OpS2C{
			Op:  msg.Op,
			Err: ErrorID_E_OK,
		}

		data, err := proto.Marshal(&ret)
		if err != nil {
			glog.Error("proto marshal error:", err, ". msg:", ret)
			return
		}

		var p network.Packet
		p.Attach(data)
		server.network.SendPacket(conn, &p)

	case OpID_OP_PUSH_DIR_MD5:
		// 生成本地对应目录的MD5信息
		clientDir := msg.PushDirMd5Info.ClientDir
		serverDir := msg.PushDirMd5Info.ServerDir
		info := generateDirMd5Info(serverDir, "")

		//updateFiles保存需要更新的文件列表
		updateFiles := make([]string, len(msg.PushDirMd5Info.Files))
		ndx := 0
		// 对比, 注意同名目录和文件
		for _, v := range msg.PushDirMd5Info.Files {
			exist := false
			for _, v1 := range *info {
				if v.Filename == v1.Filename {
					if v.Md5 == v1.Md5 && !v.IsDir && !v1.IsDir || (v.IsDir && v1.IsDir) {
						v1.Operation = FileOperation_FO_NOCHANGE
					} else if v.IsDir && !v1.IsDir {
						v1.Operation = FileOperation_FO_DELETE
					} else if !v.IsDir && v1.IsDir {
						v1.Operation = FileOperation_FO_DELETE
						updateFiles[ndx] = v.Filename
						ndx++
					} else {
						v1.Operation = FileOperation_FO_DIFF
						updateFiles[ndx] = v.Filename
						ndx++
					}
					exist = true
					break
				}
			}

			if !exist {
				updateFiles[ndx] = v.Filename
				ndx++
			}
		}
		fmt.Println(updateFiles, len(updateFiles), ndx)
		fmt.Println(info)

		// 删除多余的文件和目录
		for _, v := range *info {
			f := filepath.Join(serverDir, v.Filename)
			if v.Operation != FileOperation_FO_DIFF && v.Operation != FileOperation_FO_NOCHANGE {
				fmt.Println("删除:", v.Filename)
				if v.IsDir {
					if err := os.RemoveAll(f); err != nil {
						fmt.Println("删除目录失败:", f)
						return
					}
				} else {
					if err := os.Remove(f); err != nil {
						fmt.Println("删除文件失败:", f)
						return
					}
				}
			}
		}

		// 获取文件
		for i, filename := range updateFiles {
			if i >= ndx {
				break
			}
			ret := OpS2C{
				Op:  OpID_OP_GET_FILEINFO,
				Err: ErrorID_E_OK,
				GetFileInfo: &OpGetFileInfo{
					Filename:  filename,
					ClientDir: clientDir,
					ServerDir: serverDir,
				},
			}

			data, err := proto.Marshal(&ret)
			if err != nil {
				glog.Error("proto marshal error:", err, ". msg:", ret)
				return
			}

			var p network.Packet
			p.Attach(data)
			server.network.SendPacket(conn, &p)
		}
	case OpID_OP_SEND_FILE:
		ioutil.WriteFile(filepath.Join(msg.SendFile.ServerDir, msg.SendFile.Filename), []byte(msg.SendFile.Data), os.ModePerm)
		fmt.Println("更新:", filepath.Join(msg.SendFile.ServerDir, msg.SendFile.Filename))
	}
}
