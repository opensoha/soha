package routes

import "github.com/gin-gonic/gin"

func registerManagedContentRoutes(protected gin.IRoutes, deps Dependencies) {
	base := "/network-access/content-transfers"
	if h := deps.ManagedContentDestinations; h != nil {
		protected.GET(base+"/destinations", h.List)
		protected.GET(base+"/destinations/admin", h.AdminList)
		protected.GET(base+"/destinations/review", h.ReviewList)
		protected.GET(base+"/destinations/:destinationID", h.Get)
		protected.PUT(base+"/destinations/:destinationID", h.Save)
		protected.POST(base+"/destinations/:destinationID/publish", h.Publish)
		protected.POST(base+"/destinations/:destinationID/preview", h.Preview)
	}
	if h := deps.ManagedContentTransfers; h != nil {
		protected.POST(base+"/uploads", h.Upload)
		protected.POST(base+"/pastes", h.Paste)
		protected.GET(base+"/files", h.Files)
		protected.POST(base+"/files/:fileID/read", h.Read)
		protected.POST(base+"/files/:fileID/copy", h.Copy)
		protected.POST(base+"/files/:fileID/delete", h.Remove)
		protected.GET(base+"/records", h.Records)
		protected.GET(base+"/records/:recordID", h.Get)
		protected.GET(base+"/destinations/:destinationID/cases", h.Cases)
	}
	if h := deps.ManagedContentWorkflow; h != nil {
		protected.POST(base+"/records/:recordID/authorize", h.Authorize)
		protected.POST(base+"/records/:recordID/appeal", h.Appeal)
		protected.POST(base+"/records/:recordID/review", h.Review)
		protected.GET(base+"/records/:recordID/network", h.NetworkState)
	}
}
