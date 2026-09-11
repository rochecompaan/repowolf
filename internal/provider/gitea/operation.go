package gitea

import (
	"errors"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/config"
)

var ErrInvalidRequest = errors.New("invalid Gitea request")

func ValidateRequest(request *repowolfv1.GiteaRequest) error {
	if request == nil || request.GetRepositoryView() == nil {
		return ErrInvalidRequest
	}
	return nil
}
func Capability(request *repowolfv1.GiteaRequest) (config.Capability, error) {
	if err := ValidateRequest(request); err != nil {
		return "", err
	}
	return config.RepositoryRead, nil
}
func OperationName(request *repowolfv1.GiteaRequest) (string, error) {
	if err := ValidateRequest(request); err != nil {
		return "", err
	}
	return "gitea.repository_view", nil
}
