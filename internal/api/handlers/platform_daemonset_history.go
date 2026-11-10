package handlers

import "github.com/gin-gonic/gin"

func (h *daemonSetResourceHandler) ListDaemonSetRolloutHistory(c *gin.Context) {
	writeControllerHistory(c, "daemonSetName", h.reader.ListDaemonSetRolloutHistory)
}
func (h *daemonSetResourceHandler) GetDaemonSetRevisionYAML(c *gin.Context) {
	writeControllerRevision(c, "daemonSetName", h.reader.GetDaemonSetRevisionYAML)
}
