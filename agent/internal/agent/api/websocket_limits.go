package api

import "github.com/gorilla/websocket"

const websocketControlMessageReadLimit = 1 << 20

func configureWebSocketReadLimit(conn *websocket.Conn) {
	if conn != nil {
		conn.SetReadLimit(websocketControlMessageReadLimit)
	}
}
