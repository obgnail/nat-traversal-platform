package frp

import (
	"fmt"
	"github.com/juju/errors"
	log "github.com/sirupsen/logrus"
	"io"
	"strings"
	"sync"
	"time"
)

type ServerStatus int

const (
	StatusIdle ServerStatus = iota
	StatusReady
	StatusWork
)

type appServer struct {
	name     string
	status   ServerStatus
	listener *Listener
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

func (group *appGroup) startProxyApp() {
	proxy := func(app *AppInfo) {
		// 如果存在已有的,关闭掉
		if ps, ok := group.onProxyApps[app.Name]; ok {
			ps.listener.Close()
		}

		listener, err := NewListener(group.commonServer.bindAddr, app.ListenPort)
		if err != nil {
			log.Error(errors.ErrorStack(errors.Trace(err)))
			return
		}
		server := &appServer{
			name:     app.Name,
			status:   StatusIdle,
			listener: listener,
		}

		group.onProxyApps[app.Name] = server

		for {
			conn, err := server.listener.GetConn()
			if err != nil {
				log.Error(errors.ErrorStack(errors.Trace(err)))
				return
			}
			log.Info("connect success:", conn.String())

			// connection from client
			if server.status == StatusReady && conn.GetRemoteIP() == group.clientConn.GetRemoteIP() {
				msg, err := conn.ReadMessage()
				if err != nil {
					log.Warnf("proxy client read err:", errors.Trace(err))
					if err == io.EOF {
						log.Errorf("GroupName [%s], server is dead!", server.name)
						group.commonServer.delGroup(group.name)
						return
					}
					continue
				}
				if msg.Type != TypeClientJoin {
					log.Warn("get wrong msg")
					continue
				}

				appProxyPort := msg.Content
				newClientConn, ok := group.userConnMap.Load(appProxyPort)
				if !ok {
					log.Error("userConnMap load failed. appProxyAddrEny:", appProxyPort)
					continue
				}
				group.userConnMap.Delete(appProxyPort)

				waitToJoinClientConn := conn
				waitToJoinUserConn := newClientConn.(*Conn)
				log.Infof("Join two connections, [%s] <====> [%s]", waitToJoinUserConn.String(), waitToJoinClientConn.String())
				go Join(waitToJoinUserConn, waitToJoinClientConn)
				server.status = StatusWork

				// connection from user
			} else {
				group.userConnMap.Store(app.Name, conn)
				time.AfterFunc(JoinConnTimeout, func() {
					userConn, ok := group.userConnMap.Load(app.Name)
					if !ok || userConn == nil {
						return
					}
					if conn == userConn.(*Conn) {
						log.Errorf("GroupName [%s], user conn [%s], join connections timeout", app.Name, conn.GetRemoteAddr())
						conn.Close()
					}
					server.status = StatusIdle
				})

				// 通知client, Dial到此端口
				msg := NewMessage(TypeAppWaitJoin, app.Name, app.Name, nil)
				err := group.clientConn.SendMessage(msg)
				if err != nil {
					log.Warn(errors.ErrorStack(errors.Trace(err)))
					return
				}
				server.status = StatusReady
			}
		}
	}

	for _, app := range group.wantProxyApps {
		go proxy(app)
	}
}

func (group *appGroup) Close() {
	log.Info("close conn: ", group.clientConn.String())
	group.clientConn.Close()
	for _, app := range group.onProxyApps {
		app.listener.Close()
	}
	close(group.heartbeatChan)
	group.commonServer = nil
}

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

	// 为了在断开连接时，快速找到并关闭appGroup,CommonServer与appGroup互相引用
	inUseLock  sync.Mutex           // protect follow
	inUseGroup map[string]*appGroup // 对应appGroup的commonServer
	inUseConn  map[*Conn]*appGroup  // 对应appGroup的clientConn
}

func NewServer(name, bindAddr string, listenPort int64) (*CommonServer, error) {
	listener, err := NewListener(bindAddr, listenPort)
	if err != nil {
		return nil, errors.Trace(err)
	}
	srv := &CommonServer{
		Name:       name,
		bindAddr:   bindAddr,
		listenPort: listenPort,
		listener:   listener,
		readyGroup: make(map[string]*GroupInfo),
		inUseGroup: make(map[string]*appGroup),
		inUseConn:  make(map[*Conn]*appGroup),
	}
	return srv, nil
}

func (srv *CommonServer) groups() {
	log.Info("---------- Ready Group ----------")
	for _, group := range srv.readyGroup {
		log.Infof("%s", group.Name)
		for _, app := range group.Apps {
			log.Infof("[%s]:\t %d", app.Name, app.LocalPort)
		}
	}
	log.Info("---------- InUse Group ----------")
	for _, group := range srv.inUseGroup {
		log.Infof("%s", group.name)
		for _, app := range group.onProxyApps {
			conn, _ := app.listener.GetConn()
			log.Infof("[%s]:\t %s", app.name, conn)
		}
	}
}

func (srv *CommonServer) RegisterGroup(groupName string, apps []*AppInfo) {
	srv.readyLock.Lock()
	defer srv.readyLock.Unlock()

	group := &GroupInfo{Name: groupName, Apps: make(map[string]*AppInfo, len(apps))}
	for _, app := range apps {
		group.Apps[app.Name] = app
	}
	srv.readyGroup[groupName] = group
}

func (srv *CommonServer) delGroup(groupName string) {
	srv.inUseLock.Lock()
	defer srv.inUseLock.Unlock()

	group, ok := srv.inUseGroup[groupName]
	if !ok {
		return
	}
	group.Close()
	delete(srv.inUseGroup, groupName)
	delete(srv.inUseConn, group.clientConn)

	srv.groups()
}

func (srv *CommonServer) delGroupByConn(conn *Conn) {
	if conn == nil {
		return
	}
	group, ok := srv.inUseConn[conn]
	if !ok {
		conn.Close()
		return
	}
	srv.delGroup(group.name)
}

func (srv *CommonServer) getGroupInfoFromMsg(msg *Message) (groupInfo *GroupInfo, err error) {
	if msg.Meta == nil {
		return nil, EmptyError(ModelMessage, ErrArgMeta)
	}
	wantProxyApps := make(map[string]*AppInfo)
	for name, app := range msg.Meta.(map[string]interface{}) {
		port, err := TryGetFreePort(5)
		if err != nil {
			return nil, errors.Trace(err)
		}
		App := app.(map[string]interface{})
		wantProxyApps[name] = &AppInfo{
			Name:       App["Name"].(string),
			LocalPort:  int64(App["LocalPort"].(float64)),
			Password:   App["Password"].(string),
			ListenPort: int64(port),
		}
	}
	return &GroupInfo{
		Name: msg.ProxyName,
		Apps: wantProxyApps,
	}, nil
}

func (srv *CommonServer) checkGroup(clientConn *Conn, msg *Message) (*appGroup, error) {
	infoFromMsg, err := srv.getGroupInfoFromMsg(msg)
	if err != nil {
		return nil, errors.Trace(err)
	}
	infoFromPool, ok := srv.readyGroup[msg.ProxyName]
	if !ok {
		return nil, fmt.Errorf("no such group: %s", msg.ProxyName)
	}
	for _, mApp := range infoFromMsg.Apps {
		pAPP, ok := infoFromPool.Apps[mApp.Name]
		if !ok {
			return nil, fmt.Errorf("no such app: %s", mApp.Name)
		}
		if mApp.Password != pAPP.Password {
			return nil, InvalidPasswordError(mApp.Name)
		}
	}
	group := &appGroup{
		name:          infoFromMsg.Name,
		wantProxyApps: infoFromMsg.Apps,
		onProxyApps:   make(map[string]*appServer, len(infoFromMsg.Apps)),
		heartbeatChan: make(chan *Message, 1),
		clientConn:    clientConn,
		commonServer:  srv,
	}
	srv.inUseLock.Lock()
	srv.inUseGroup[group.name] = group
	srv.inUseConn[group.clientConn] = group
	srv.inUseLock.Unlock()

	return group, nil
}

func (srv *CommonServer) initGroup(clientConn *Conn, msg *Message) {
	group, err := srv.checkGroup(clientConn, msg)
	if err != nil {
		log.Error(errors.ErrorStack(errors.Trace(err)))
		srv.delGroup(group.name)
		return
	}

	// 代理具体服务
	go group.startProxyApp()

	// 告知client这些App可以进行代理
	resp := NewMessage(TypeAppMsg, "", srv.Name, group.wantProxyApps)
	err = clientConn.SendMessage(resp)
	if err != nil {
		log.Error(errors.ErrorStack(errors.Trace(err)))
		srv.delGroup(group.name)
		return
	}

	// 与每一个group保持Heartbeat
	go func() {
		for {
			select {
			case _, ok := <-group.heartbeatChan:
				if ok {
					log.Debug("received heartbeat msg from", clientConn.GetRemoteAddr())
					resp := NewMessage(TypeServerHeartbeat, "", srv.Name, nil)
					if err := clientConn.SendMessage(resp); err != nil {
						log.Warn(SendHeartbeatMessageError())
						log.Warn(errors.ErrorStack(errors.Trace(err)))
						return
					}
				} else {
					log.Warnf("GroupName [%s], user conn [%s] Heartbeat close", srv.Name, clientConn.GetRemoteAddr())
					srv.delGroupByConn(clientConn)
					return
				}
			case <-time.After(HeartbeatTimeout):
				log.Warnf("GroupName [%s], user conn [%s] Heartbeat timeout", srv.Name, clientConn.GetRemoteAddr())
				srv.delGroupByConn(clientConn)
				return
			}
		}
	}()
}

// 将收到的心跳包分发到各自的group
func (srv *CommonServer) dispatchHeartbeat(msg *Message) {
	group, ok := srv.inUseGroup[msg.ProxyName]
	if !ok {
		log.Warn(fmt.Errorf("dispatchHeartbeat, no such appGroup: %s", msg.ProxyName))
		return
	}
	group.heartbeatChan <- msg
}

func (srv *CommonServer) process(clientConn *Conn) {
	for {
		msg, err := clientConn.ReadMessage()
		if err != nil {
			log.Warn(errors.ErrorStack(errors.Trace(err)))
			if err == io.EOF || strings.Contains(err.Error(), "connection reset by peer") {
				log.Infof("GroupName [%s], client is dead!", srv.Name)
				srv.delGroupByConn(clientConn)
			}
			return
		}

		switch msg.Type {
		case TypeInitGroup:
			go srv.initGroup(clientConn, msg)
		case TypeClientHeartbeat:
			go srv.dispatchHeartbeat(msg)
		}
	}
}

func (srv *CommonServer) Serve() {
	if srv == nil {
		err := EmptyError(ModelServer, ErrArgServer)
		log.Fatal(err)
	}
	if srv.listener == nil {
		err := EmptyError(ModelServer, ErrArgListener)
		log.Fatal(err)
	}
	for {
		clientConn, err := srv.listener.GetConn()
		if err != nil {
			log.Warn("proxy get conn err:", errors.Trace(err))
			continue
		}
		go srv.process(clientConn)
	}
}
