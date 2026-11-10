package cloud

import (
	"context"
	"errors"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

type Identity struct {
	credential azcore.TokenCredential
}

func NewIdentity() (*Identity, error) {
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, errors.New("cannot initialize Azure credential")
	}
	return &Identity{credential: credential}, nil
}

func (i *Identity) Token(ctx context.Context, scope string) (string, error) {
	token, err := i.credential.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{scope}})
	if err != nil {
		return "", errors.New("Azure credential could not acquire token for " + scope)
	}
	return token.Token, nil
}
