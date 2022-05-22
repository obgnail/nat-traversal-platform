package main

import (
	"github.com/obgnail/nat-traversal-platform/frp"
)

func main() {
	var serverPort int64 = 8888
	groupName := "test_group"
	groupName2 := "test_group22"
	wantToProxyApps := []*frp.AppInfo{
		{Name: "SSH", LocalPort: 22, Password: "this is password"},
		{Name: "HTTP", LocalPort: 7777, Password: "password2"},
	}

	server, _ := frp.NewServer("common", "0.0.0.0", serverPort)
	server.RegisterGroup(groupName, wantToProxyApps)
	server.RegisterGroup(groupName2, wantToProxyApps)
	server.Serve()
}
