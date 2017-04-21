package main

import (
	"errors"
	"fmt"
	"globaltedinc/framework/network"
	"io/ioutil"
	"path/filepath"

	"os"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
)

type tClient struct {
	ip   string
	port uint

	retCreateSyncNode chan *OpS2C
	retPushSyncNode   chan *OpS2C

	network network.TCPClient
}

func (client *tClient) connect() bool {
	err := client.network.Connect(
		fmt.Sprintf("%s:%d", client.ip, client.port),
		2000,
		client.handleDisconnect,
		client.handlePacket)

	if err != nil {
		fmt.Println(err)
		return false
	}
	fmt.Println("connected.")
	client.retCreateSyncNode = make(chan *OpS2C)
	client.retPushSyncNode = make(chan *OpS2C)

	return true
}

func (client *tClient) handleDisconnect(addr string, err error) {
	fmt.Println("disconnect, addr:", addr, "err:", err)
}

func (client *tClient) handlePacket(packet *network.Packet) {
	//fmt.Println("server message.")
	//c.SendPacket(packet)
	msg := &OpS2C{}
	if err := proto.Unmarshal(packet.GetData(), msg); err != nil {
		client.network.Disconnect()
		glog.Error("error:", err)
		return
	}

	fmt.Println("msg:", msg)
	switch msg.Op {
	case OpID_OP_CREATE_SYNC_NODE:
		client.retCreateSyncNode <- msg
	case OpID_OP_PUSH_SYNC_NODE:
		client.retPushSyncNode <- msg
	case OpID_OP_GET_FILEINFO:
		f, err := os.Open(filepath.Join(msg.GetFileInfo.ClientDir, msg.GetFileInfo.Filename))
		if err != nil {
			fmt.Println("打开文件失败:", filepath.Join(msg.GetFileInfo.ClientDir, msg.GetFileInfo.Filename))
			return
		}
		defer f.Close()
		d, err := ioutil.ReadAll(f)
		if err != nil {
			fmt.Println("读取文件内容失败")
			return
		}

		ret := &OpC2S{}
		ret.Op = OpID_OP_SEND_FILE
		ret.SendFile = &OpSendFile{
			Filename:  msg.GetFileInfo.Filename,
			ClientDir: msg.GetFileInfo.ClientDir,
			ServerDir: msg.GetFileInfo.ServerDir,
			Data:      string(d),
		}
		fmt.Println("len:", len(d))

		data, err := proto.Marshal(ret)
		if err != nil {
			glog.Error("proto marshal error:", err, ". msg:", ret)
			return
		}

		var p network.Packet
		p.Attach(data)
		fmt.Println(client.network.SendPacket(&p))

	default:
		fmt.Println("非法消息.")
		client.network.Disconnect()
	}

}

func (client *tClient) createSyncNode(name string, dir string) bool {

	if !client.connect() {
		return false
	}

	msg := OpC2S{
		Op: OpID_OP_CREATE_SYNC_NODE,
		CreateSyncNode: &OpCreateSyncNode{
			Nodes: &Nodes{
				Nodes: []*Node{&Node{Name: name, Directory: dir}},
			},
		},
	}

	data, err := proto.Marshal(&msg)
	if err != nil {
		glog.Error("proto marshal error:", err, ". msg:", msg)
		return false
	}

	var p network.Packet
	p.Attach(data)
	client.network.SendPacket(&p)

	ret := <-client.retCreateSyncNode

	switch ret.Err {
	case ErrorID_E_OK:
		fmt.Println("Done.")
		return true
	case ErrorID_E_ALREADY_EXIST:
		fmt.Println("此目录已经存在！")
		return false
	}

	return false
}

/*
命令行: sync push d:\backup d:\backup1
每个目录对应一条消息, 子目录递归

客户端生成目录的md5信息发送给服务端，服务端对比本地目录的md5消息，如果文件存在且有差异，则向客户端请求文件，如果文件相同，不处理，如果文件客户端不存在此文件，则需要删除

*/

func generateDirMd5Info(localDir, relative string) *[]*FileMD5Info {
	dir := filepath.Join(localDir, relative)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		fmt.Println("not exist, create dir")
		os.Mkdir(dir, os.ModePerm)
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil
	}

	fi, err := f.Readdir(0)
	if err != nil {
		return nil
	}

	info := make([]*FileMD5Info, len(fi))
	for i, v := range fi {
		info[i] = &FileMD5Info{}
		info[i].Filename = v.Name()
		info[i].IsDir = v.IsDir()
		if !info[i].IsDir {
			md5, err := md5file(filepath.Join(dir, v.Name()))
			if err == nil {
				info[i].Md5 = md5
			} else {
				return nil
			}
		}
	}

	f.Close()

	return &info
}

func (client *tClient) sendDirMd5Info(dir1 string, relativeDir string, dir2 string) error {
	info := generateDirMd5Info(dir1, relativeDir)
	if info == nil {
		return errors.New("Error")
	}
	msg := &OpC2S{
		Op: OpID_OP_PUSH_DIR_MD5,
		PushDirMd5Info: &OpPushdirMd5Info{
			ClientDir: filepath.Join(dir1, relativeDir),
			ServerDir: filepath.Join(dir2, relativeDir),
		},
	}

	msg.PushDirMd5Info.Files = *info

	fmt.Println(msg)

	data, err := proto.Marshal(msg)
	if err != nil {
		glog.Error("proto marshal error:", err, ". msg:", msg)
		return err
	}
	var p network.Packet
	p.Attach(data)
	client.network.SendPacket(&p)

	for _, v := range *info {
		if v.IsDir {
			if err := client.sendDirMd5Info(dir1, relativeDir+"/"+v.Filename, dir2); err != nil {
				return err
			}
		}
	}

	return nil
}

func (client *tClient) pushSyncNode(name string, dir string) bool {
	if !client.connect() {
		return false
	}
	err := client.sendDirMd5Info(name, "", dir)
	fmt.Println(err)

	ret := <-client.retPushSyncNode

	switch ret.Err {
	case ErrorID_E_OK:
		fmt.Println("Done.")
		return true
	case ErrorID_E_ALREADY_EXIST:
		fmt.Println("此目录已经存在！")
		return false
	}

	return false
}
