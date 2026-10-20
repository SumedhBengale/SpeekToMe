package privatevault

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/aws/aws-sdk-go/service/secretsmanager/secretsmanageriface"
	"os"
)

type Secrets struct {
	MasterKey string `json:"master_key"`
}

// LoadSecrets performs one retrieval per process. No disk cache, refresh timer,
// environment-variable plaintext key, secret-bearing errors or SDK debug logging.
func LoadSecrets(ctx context.Context) (Secrets, error) {
	var s Secrets
	id := os.Getenv("MASTER_KEY_SECRET_ARN")
	if id == "" {
		return s, errors.New("MASTER_KEY_SECRET_ARN is required")
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}
	sess, e := session.NewSession(&aws.Config{Region: aws.String(region), MaxRetries: aws.Int(0)})
	if e != nil {
		return s, ErrUnavailable
	}
	if os.Getenv("PRIVACY_USE_INSTANCE_ROLE") == "true" {
		sess.Config.Credentials = credentials.NewCredentials(&ec2rolecreds.EC2RoleProvider{Client: ec2metadata.New(sess), ExpiryWindow: 0})
	}
	return readSecret(ctx, secretsmanager.New(sess), id)
}
func readSecret(ctx context.Context, client secretsmanageriface.SecretsManagerAPI, id string) (Secrets, error) {
	var s Secrets
	out, e := client.GetSecretValueWithContext(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(id)})
	if e != nil || out.SecretString == nil {
		return s, errors.New("cannot load personal-storage secret")
	}
	if json.Unmarshal([]byte(*out.SecretString), &s) != nil {
		return Secrets{}, errors.New("invalid personal-storage secret format")
	}
	key, e := base64.StdEncoding.DecodeString(s.MasterKey)
	defer clear(key)
	if e != nil || len(key) != 32 {
		return Secrets{}, errors.New("invalid master key encoding")
	}
	return s, nil
}
