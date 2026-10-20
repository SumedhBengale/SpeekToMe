package privatevault

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/aws/aws-sdk-go/service/secretsmanager/secretsmanageriface"
	"strings"
	"testing"
)

type secretClient struct {
	secretsmanageriface.SecretsManagerAPI
	calls int
	out   string
	err   error
}

func (c *secretClient) GetSecretValueWithContext(_ aws.Context, _ *secretsmanager.GetSecretValueInput, _ ...request.Option) (*secretsmanager.GetSecretValueOutput, error) {
	c.calls++
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(c.out)}, c.err
}
func TestSingleSecretReadAndSanitizedFailures(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	client := &secretClient{out: `{"master_key":"` + key + `"}`}
	s, e := readSecret(context.Background(), client, "secret-id")
	if e != nil || s.MasterKey != key || client.calls != 1 {
		t.Fatal("startup did not read exactly once")
	}
	client = &secretClient{err: errors.New("secret-value-must-not-leak")}
	_, e = readSecret(context.Background(), client, "secret-id")
	if e == nil || strings.Contains(e.Error(), "secret-value-must-not-leak") || client.calls != 1 {
		t.Fatal("secret retrieval leaked details or retried")
	}
	client = &secretClient{out: `{"master_key":"short"}`}
	if _, e = readSecret(context.Background(), client, "secret-id"); e == nil {
		t.Fatal("invalid key accepted")
	}
}
