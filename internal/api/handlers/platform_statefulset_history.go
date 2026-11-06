package handlers

import "github.com/gin-gonic/gin"

func (h *statefulSetResourceHandler) ListStatefulSetRolloutHistory(c *gin.Context) {
	writeControllerHistory(c, "statefulSetName", h.reader.ListStatefulSetRolloutHistory)
}
func (h *statefulSetResourceHandler) GetStatefulSetRevisionYAML(c *gin.Context) {
	writeControllerRevision(c, "statefulSetName", h.reader.GetStatefulSetRevisionYAML)
}
