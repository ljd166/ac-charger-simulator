// mock-csms — 本地开发用最小 OCPP 1.6J CSMS:接受一切请求。
// 用途:不依赖真实 R3S 即可本地联调仿真桩/Web Console。
//
//	go run ./tools/mock-csms -port 9999
//	桩 endpoint 配 ws://127.0.0.1:9999/ocpp/<id>
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:  func(r *http.Request) bool { return true },
	Subprotocols: []string{"ocpp1.6"},
}

var txCounter atomic.Int64

func main() {
	port := flag.String("port", "9999", "listen port")
	flag.Parse()
	txCounter.Store(time.Now().Unix())

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		log.Printf("charger connected: %s", r.URL.Path)
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				log.Printf("charger gone: %s (%v)", r.URL.Path, err)
				return
			}
			if mt != websocket.TextMessage {
				continue
			}
			var raw []interface{}
			if json.Unmarshal(data, &raw) != nil || len(raw) < 3 {
				continue
			}
			if mtID, _ := raw[0].(float64); mtID != 2 { // 只回 Call
				continue
			}
			msgID, _ := raw[1].(string)
			action, _ := raw[2].(string)
			var payload interface{}
			switch action {
			case "BootNotification":
				payload = map[string]interface{}{"status": "Accepted", "currentTime": time.Now().UTC().Format(time.RFC3339), "interval": 60}
			case "Authorize":
				payload = map[string]interface{}{"idTagInfo": map[string]string{"status": "Accepted"}}
			case "StartTransaction":
				payload = map[string]interface{}{"idTagInfo": map[string]string{"status": "Accepted"}, "transactionId": txCounter.Add(1)}
			default: // StatusNotification / Heartbeat / MeterValues / StopTransaction ...
				payload = map[string]interface{}{}
			}
			log.Printf("%-20s ← %s", action, r.URL.Path)
			conn.WriteJSON([]interface{}{3, msgID, payload})
		}
	})
	log.Printf("mock-csms listening :%s (accepts everything)", *port)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}
