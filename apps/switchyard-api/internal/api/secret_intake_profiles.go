package api

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
)

// Typed profiles initialize their whole group once. They cannot import partial
// values, regenerate one half of a pair, or use the generic random-string path.
func generatedProfileValues(target secretsintake.Target, generate []string, supplied map[string]interface{}) (map[string]interface{}, []string, error) {
	if len(supplied) != 0 || len(generate) != len(target.Keys) {
		return nil, nil, fmt.Errorf("typed staging profile requires all declared keys in generate and no supplied values")
	}
	requested := make(map[string]bool, len(generate))
	for _, raw := range generate {
		key := strings.ToLower(strings.TrimSpace(raw))
		if requested[key] {
			return nil, nil, fmt.Errorf("duplicate key in typed staging generation")
		}
		requested[key] = true
	}
	keys := make([]string, 0, len(target.Keys))
	for _, key := range target.Keys {
		if !requested[key] {
			return nil, nil, fmt.Errorf("typed staging profile requires all declared keys in generate")
		}
		keys = append(keys, strings.ToUpper(key))
	}
	values := make(map[string]interface{}, len(keys))
	switch target.Generate.Profile {
	case secretsintake.KarafielStagingCoreProfile:
		value, err := generateSecretValue(32)
		if err != nil {
			return nil, nil, fmt.Errorf("could not generate staging core")
		}
		values["django_secret_key"] = value
		material := make([]byte, 32)
		if _, err := rand.Read(material); err != nil {
			return nil, nil, fmt.Errorf("could not generate staging encryption key")
		}
		values["encryption_master_key"] = hex.EncodeToString(material)
	case secretsintake.KarafielStagingSessionProfile:
		value, err := generateSecretValue(32)
		if err != nil {
			return nil, nil, fmt.Errorf("could not generate staging session")
		}
		values["admin_session_secret"] = value
	case secretsintake.JanuaStagingCryptoProfile:
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, nil, fmt.Errorf("could not generate staging RSA pair")
		}
		privateDER, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, nil, fmt.Errorf("could not encode staging private key")
		}
		publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			return nil, nil, fmt.Errorf("could not encode staging public key")
		}
		values["jwt_private_key"] = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))
		values["jwt_public_key"] = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
		for _, name := range []string{"jwt_secret_key", "secret_key"} {
			value, err := generateSecretValue(32)
			if err != nil {
				return nil, nil, fmt.Errorf("could not generate staging secret")
			}
			values[name] = value
		}
		fernet := make([]byte, 32)
		if _, err := rand.Read(fernet); err != nil {
			return nil, nil, fmt.Errorf("could not generate staging encryption key")
		}
		// Python cryptography.Fernet requires padded URL-safe base64 of 32 bytes.
		values["field_encryption_key"] = base64.URLEncoding.EncodeToString(fernet)
	case secretsintake.JanuaStagingRedisProfile:
		password, err := generateSecretValue(32)
		if err != nil {
			return nil, nil, fmt.Errorf("could not generate staging Redis password")
		}
		// Fixed same-namespace service name from the isolated staging contract;
		// callers cannot point fresh credentials at production/shared Redis.
		endpoint := url.URL{Scheme: "redis", User: url.UserPassword("", password), Host: "janua-staging-redis:6379", Path: "/0"}
		values["redis_password"], values["redis_url"] = password, endpoint.String()
	default:
		return nil, nil, fmt.Errorf("unknown typed generation profile")
	}
	sort.Strings(keys)
	return values, keys, nil
}
