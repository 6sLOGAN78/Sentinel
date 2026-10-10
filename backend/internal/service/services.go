package service

import (
	"github.com/6sLOGAN78/sentinel/internal/lib/job"
	"github.com/6sLOGAN78/sentinel/internal/repository"
	"github.com/6sLOGAN78/sentinel/internal/server"
)

type Services struct {
	Auth *AuthService
	Job  *job.JobService
}

func NewServices(s *server.Server, repos *repository.Repositories) (*Services, error) {
	authService := NewAuthService(s)

	return &Services{
		Job:  s.Job,
		Auth: authService,
	}, nil
}