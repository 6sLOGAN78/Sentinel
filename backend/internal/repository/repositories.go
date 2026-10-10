package repository

import "github.com/6sLOGAN78/sentinel/internal/server"

type Repositories struct{}

func NewRepositories(s *server.Server) *Repositories {
	return &Repositories{}
}