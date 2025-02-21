package wsreconn

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var ErrNotConnected = errors.New("websocket: not connected")

type Reconn struct {
	url string

	reconnectCh chan struct{}
	mx          sync.RWMutex

	conn        *websocket.Conn
	isConnected bool
}

func (r *Reconn) IsConnected() bool {
	r.mx.RLock()
	defer r.mx.RUnlock()

	return r.isConnected
}

func (r *Reconn) getConn() *websocket.Conn {
	r.mx.Lock()
	defer r.mx.Unlock()

	return r.conn
}

func NewReconn(ctx context.Context, url string) (*Reconn, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	r := &Reconn{
		conn:        conn,
		url:         url,
		isConnected: true,
		reconnectCh: make(chan struct{}, 1),
	}
	go func() {
		r.runReconnect(context.TODO())
	}()
	return r, nil
}

func (rc *Reconn) SetWriteDeadline(t time.Time) error {
	rc.mx.Lock()
	if !rc.isConnected {
		rc.mx.Unlock()

		return ErrNotConnected
	}
	conn := rc.conn
	rc.mx.Unlock()

	return conn.SetWriteDeadline(t)
}

func (rc *Reconn) SetReadDeadline(t time.Time) error {
	rc.mx.Lock()
	if !rc.isConnected {
		rc.mx.Unlock()

		return ErrNotConnected
	}
	conn := rc.conn
	rc.mx.Unlock()

	return conn.SetReadDeadline(t)
}

func (rc *Reconn) SetPongHandler(h func(string) error) {
	rc.mx.Lock()
	defer rc.mx.Unlock()

	if !rc.isConnected {
		return
	}
	rc.conn.SetPongHandler(h)
}

func (rc *Reconn) WriteMessage(messageType int, data []byte) error {
	if !rc.IsConnected() {
		return ErrNotConnected
	}
	err := rc.getConn().WriteMessage(messageType, data)
	if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		rc.Close()
		return nil
	}
	if err != nil {
		rc.CloseAndReconnect()
	}
	return err
}

func (rc *Reconn) WriteJSON(data any) error {
	if !rc.IsConnected() {
		return ErrNotConnected
	}
	err := rc.getConn().WriteJSON(data)
	if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		rc.Close()
		return nil
	}
	if err != nil {
		rc.CloseAndReconnect()
	}
	return err
}

func (rc *Reconn) ReadMessage() (messageType int, message []byte, err error) {
	if !rc.IsConnected() {
		return 0, nil, ErrNotConnected
	}
	msg, t, err := rc.getConn().ReadMessage()
	if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		rc.Close()
		return 0, nil, nil
	}
	if err != nil {
		rc.CloseAndReconnect()
	}
	return msg, t, err
}
func (rc *Reconn) ReadJSON(msg any) error {
	if !rc.IsConnected() {
		return ErrNotConnected
	}
	err := rc.getConn().ReadJSON(msg)
	if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		rc.Close()
		return nil
	}
	if err != nil {
		rc.CloseAndReconnect()
	}
	return err
}

func (rc *Reconn) Close() {
	rc.mx.Lock()

	if !rc.isConnected {
		rc.mx.Unlock()
		return
	}

	_ = rc.conn.Close()
	rc.isConnected = false
	rc.mx.Unlock()
}

func (rc *Reconn) CloseAndReconnect() {
	rc.mx.Lock()

	if !rc.isConnected {
		rc.mx.Unlock()
		return
	}

	_ = rc.conn.Close()
	rc.isConnected = false
	rc.mx.Unlock()

	rc.reconnectCh <- struct{}{}
}

func (rc *Reconn) runReconnect(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-rc.reconnectCh:
			log.Println("reconnecting")
			conn := rc.dialWithRetry()
			rc.mx.Lock()
			rc.conn = conn
			rc.isConnected = true
			rc.mx.Unlock()
		}
	}
}

func (rc *Reconn) dialWithRetry() *websocket.Conn {
	for {
		conn, _, err := websocket.DefaultDialer.DialContext(context.TODO(), rc.url, nil)
		if err != nil {
			log.Println("dial:", err)
			time.Sleep(time.Second)
			continue
		}
		return conn
	}
}
