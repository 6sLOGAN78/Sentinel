package detection

import (
	"errors"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

func freezeEvidence(_ windowKey, _ *windowState, _ Rule) (*contracts.Alert, []byte, string, error) {
	return nil, nil, "", errors.New("canonical evidence is not implemented")
}
