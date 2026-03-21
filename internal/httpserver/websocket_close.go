package httpserver

import (
	"log/slog"

	"github.com/coder/websocket"
)

func closeWebsocket(logger *slog.Logger, conn *websocket.Conn) {
	if conn == nil {
		return
	}
	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil && logger != nil {
		logger.Debug("websocket close failed", "err", err)
	}
}
