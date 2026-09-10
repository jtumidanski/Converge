package gitx

import (
	"encoding/base64"
	"fmt"
	"net/url"
)

// CredentialEnv builds the GIT_CONFIG_* environment that scopes a Basic
// Authorization header to the clone URL's origin. The token never enters argv.
func CredentialEnv(cloneURL, user, token string) ([]string, error) {
	u, err := url.Parse(cloneURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%w: clone URL must be http(s)", ErrInvalid)
	}
	if user == "" || token == "" {
		return nil, fmt.Errorf("%w: credential user and token are required", ErrInvalid)
	}
	basic := base64.StdEncoding.EncodeToString([]byte(user + ":" + token))
	return []string{
		"GIT_CONFIG_COUNT=1",
		fmt.Sprintf("GIT_CONFIG_KEY_0=http.%s://%s/.extraheader", u.Scheme, u.Host),
		"GIT_CONFIG_VALUE_0=Authorization: Basic " + basic,
	}, nil
}
