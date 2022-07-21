# nat-traversal-platform
内网穿透平台，支持多主机多应用穿透

## 时序图

```mermaid
sequenceDiagram
		autonumber
    participant User
    participant CommonServer
    participant appGroup
    participant appServer
    participant Client
    participant LocalApp

    Note over CommonServer, Client: 准备阶段
    CommonServer ->> CommonServer: 监听BindPort
    Client ->> Client: 构建LocalApp对象
    Client ->> CommonServer: 注册appGroup(写入readyGroup)
    
    Note over CommonServer, Client: 初始化阶段
    Client ->> + CommonServer: 连接到Common conn，发送appGroup的信息
    CommonServer ->> CommonServer: 根据已注册的readyGroup取出appGroup对象，校验
    CommonServer -x appGroup: 通知appGroup,将此appGroup写入inUseGroup
    appGroup ->> appGroup: 开启goroutine，监听heartbeat
    appGroup ->> appServer: 通知所有的appServer开始监听端口
    appServer ->> appServer: 开启goroutine，监听ProxyConn，等待User连接
    CommonServer -->> - Client: 告知Client，该appGroup已经准备就绪，可以代理了
    Client ->> Client: 存储appGroup在commonSever里监听的端口
   	par par and loop
   		Client ->> CommonServer: 发送heartbeat
   		CommonServer ->> appGroup: 分发给appGroup
   		appGroup -->> Client: 响应heartbeat
   	end
    
    Note over User, LocalApp: 代理阶段
    User ->> + appServer: User连接appServer，希望开始代理
    appServer ->> appServer: 储存UserConn
    appServer ->> appGroup: 通知appGroup<br/>将「希望开启代理」的信号传给Client
    appGroup ->> + Client: 告知Client「希望开启代理」
    Client ->> Client: 从存储中找到「希望开启代理」的APP信息
    Client ->> + LocalApp: 开启本地端口
    LocalApp -->> - Client: 返回localConn
    Client ->> appServer: 连接到appServer
    appServer ->> appServer: 1.校验LocalAppConn和message<br/>2.从存储中找到UserConn
    par
   		appServer ->> appServer: Join LocalAppConn and UserConn
   	end
		appServer -->> Client: 返回remoteConn
		par
   		Client ->> - Client: Join localConn and remoteConn
   	end
    appServer -->> - User: 返回
```

- User：外部用户
- CommonServer：负责 appGroup 的初始化，具体功能为 `控制信息的通讯` 和 `分发heartbeat` 。
- appGroup：app 组。一个用户可能需要有多个 app 需要代理。每个用户使用一个 appGroup。不允许重名。
- appServer：每个需要代理的 app
- Client：转发来自代理服务器的信息给 LocalApp
- LocalApp：处于内网的被代理的、提供实际业务功能的 app。



## types

```go
type GroupInfo struct {
	Name string
	Apps map[string]*AppInfo
}

type CommonServer struct {
	Name       string
	bindAddr   string
	listenPort int64

	listener *Listener

	readyLock  sync.Mutex // protect follow
	readyGroup map[string]*GroupInfo

	// 为了在断开连接时，快速找到并关闭appGroup
  // CommonServer与appGroup互相引用
	inUseLock  sync.Mutex           // protect follow
	inUseGroup map[string]*appGroup // 对应appGroup的commonServer
	inUseConn  map[*Conn]*appGroup  // 对应appGroup的clientConn
}

type appGroup struct {
	name          string
	wantProxyApps map[string]*AppInfo
	onProxyApps   map[string]*appServer // appServer which is listening its own port
	heartbeatChan chan *Message         // when get heartbeat msg, put msg in
	userConnMap   sync.Map              // map[appServerName]UserConn
	clientConn    *Conn
	commonServer  *CommonServer
}

type appServer struct {
	name     string
	status   ServerStatus
	listener *Listener
}
```
