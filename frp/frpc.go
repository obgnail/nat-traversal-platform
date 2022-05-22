package frp

import (
	"github.com/juju/errors"
	log "github.com/sirupsen/logrus"
	"io"
	"strings"
	"time"
)

type ProxyClient struct {
	GroupName     string
	LocalPort     int64
	RemoteAddr    string
	RemotePort    int64
	wantProxyApps map[string]*AppInfo
	onProxyApps   map[string]*AppInfo
	heartbeatChan chan *Message // when get heartbeat msg, put msg in
}

func NewClient(name string, localPort int64, remoteAddr string, remotePort int64, apps []*AppInfo) *ProxyClient {
	wantProxyApps := make(map[string]*AppInfo)
	for _, app := range apps {
		wantProxyApps[app.Name] = app
	}
	pc := &ProxyClient{
		GroupName:     name,
		LocalPort:     localPort,
		RemoteAddr:    remoteAddr,
		RemotePort:    remotePort,
		heartbeatChan: make(chan *Message, 1),
		wantProxyApps: wantProxyApps,
		onProxyApps:   make(map[string]*AppInfo, len(apps)),
	}
	return pc
}

func (clt *ProxyClient) getJoinConnsFromMsg(msg *Message) (localConn, remoteConn *Conn, err error) {
	appProxyName := msg.Content
	if appProxyName == "" {
		err = NotFoundError(ModelClient, ErrArgApp)
		return
	}
	appServer, ok := clt.onProxyApps[appProxyName]
	if !ok {
		err = NotFoundError(ModelServer, ErrArgApp)
		return
	}
	appClient, ok := clt.wantProxyApps[appProxyName]
	if !ok {
		err = NotFoundError(ModelClient, ErrArgClient)
		return
	}

	localConn, err = Dial("127.0.0.1", appClient.LocalPort)
	if err != nil {
		err = errors.Trace(err)
		return
	}

	remoteConn, err = Dial(clt.RemoteAddr, appServer.ListenPort)
	if err != nil {
		err = errors.Trace(err)
		return
	}
	return
}

func (clt *ProxyClient) joinConn(serverConn *Conn, msg *Message) {
	localConn, remoteConn, err := clt.getJoinConnsFromMsg(msg)
	if err != nil {
		log.Warnf("get join connections from msg.", errors.ErrorStack(errors.Trace(err)))
		if localConn != nil {
			localConn.Close()
		}
		if remoteConn != nil {
			remoteConn.Close()
		}
		return
	}

	joinMsg := NewMessage(TypeClientJoin, msg.Content, clt.GroupName, nil)
	err = remoteConn.SendMessage(joinMsg)
	if err != nil {
		log.Errorf(errors.ErrorStack(errors.Trace(err)))
		return
	}

	log.Infof("Join two connections, [%s] <====> [%s]", localConn.String(), remoteConn.String())
	go Join(localConn, remoteConn)
}

func (clt *ProxyClient) sendGroupInitMsg(conn *Conn) {
	if clt.wantProxyApps == nil {
		log.Fatal("has no app client to proxy")
	}

	// 通知server开始监听Group包含app
	msg := NewMessage(TypeInitGroup, "", clt.GroupName, clt.wantProxyApps)
	if err := conn.SendMessage(msg); err != nil {
		log.Fatal("client write init msg err.", errors.ErrorStack(errors.Trace(err)))
	}
}

func (clt *ProxyClient) storeServerApp(conn *Conn, msg *Message) {
	if msg.Meta == nil {
		log.Fatal("has no app to proxy")
	}

	for name, app := range msg.Meta.(map[string]interface{}) {
		appServer := app.(map[string]interface{})
		clt.onProxyApps[name] = &AppInfo{
			Name:       appServer["Name"].(string),
			ListenPort: int64(appServer["ListenPort"].(float64)),
		}
	}

	log.Info("---------- Sever ----------")
	for name, app := range clt.onProxyApps {
		log.Infof("[%s]:\t%s:%d", name, conn.GetRemoteIP(), app.ListenPort)
	}
	log.Info("---------------------------")

	// prepared, start first heartbeat
	clt.heartbeatChan <- msg

	// keep ErrArgHeartbeat
	go func() {
		for {
			select {
			case <-clt.heartbeatChan:
				log.Debug("received heartbeat msg from", conn.GetRemoteAddr())
				time.Sleep(HeartbeatInterval)
				resp := NewMessage(TypeClientHeartbeat, "", clt.GroupName, nil)
				err := conn.SendMessage(resp)
				if err != nil {
					log.Warn(SendHeartbeatMessageError())
					log.Warn(errors.ErrorStack(errors.Trace(err)))
				}
			case <-time.After(HeartbeatTimeout):
				log.Errorf("GroupName [%s], user conn [%s] ErrArgHeartbeat timeout", clt.GroupName, conn.GetRemoteAddr())
				if conn != nil {
					conn.Close()
				}
			}
		}
	}()
}

func (clt *ProxyClient) Run() {
	conn, err := Dial(clt.RemoteAddr, clt.RemotePort)
	if err != nil {
		log.Fatal(err)
	}

	clt.sendGroupInitMsg(conn)
	for {
		msg, err := conn.ReadMessage()
		if err != nil {
			log.Warn(errors.ErrorStack(errors.Trace(err)))
			if err == io.EOF || strings.Contains(err.Error(), "use of closed network connection") {
				log.Infof("GroupName [%s], client is dead!", clt.GroupName)
				return
			}
			continue
		}

		switch msg.Type {
		case TypeServerHeartbeat:
			clt.heartbeatChan <- msg
		case TypeAppMsg:
			clt.storeServerApp(conn, msg)
		case TypeAppWaitJoin:
			go clt.joinConn(conn, msg)
		}
	}
}
