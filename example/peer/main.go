package main

import (
	"github.com/obgnail/nat-traversal-platform/frp"
)

func main() {
	var serverPort int64 = 8888
	groupName := "test_group"
	wantToProxyApps := []*frp.AppInfo{
		{Name: "SSH", LocalPort: 22, Password: "this is password"},
		{Name: "HTTP", LocalPort: 7777, Password: "password2"},
	}

	frp.NewClient(groupName, 5555, "192.168.3.5", serverPort, wantToProxyApps).Run()
}
