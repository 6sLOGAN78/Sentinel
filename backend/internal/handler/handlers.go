package handler

import (
	"github.com/6sLOGAN78/sentinel/internal/server"
	"github.com/6sLOGAN78/sentinel/internal/service"
)

type Handlers struct {
	Health  *HealthHandler
	OpenAPI *OpenAPIHandler
}

func NewHandlers(s *server.Server, services *service.Services) *Handlers {
	return &Handlers{
		Health:  NewHealthHandler(s),
		OpenAPI: NewOpenAPIHandler(s),
	}
}