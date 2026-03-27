package ws

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoopAuth_Validate(t *testing.T) {
	auth := NoopAuth{}
	claims, err := auth.Validate("anything")
	require.NoError(t, err)
	assert.Equal(t, "anonymous", claims.Subject)
}

func TestStaticTokenAuth_Validate_Success(t *testing.T) {
	auth := StaticTokenAuth{Token: "secret-123"}
	claims, err := auth.Validate("secret-123")
	require.NoError(t, err)
	assert.Equal(t, "user", claims.Subject)
}

func TestStaticTokenAuth_Validate_Failure(t *testing.T) {
	auth := StaticTokenAuth{Token: "secret-123"}
	_, err := auth.Validate("wrong-token")
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestStaticTokenAuth_Validate_EmptyToken(t *testing.T) {
	auth := StaticTokenAuth{Token: "secret-123"}
	_, err := auth.Validate("")
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestTokenFromRequest_AuthorizationHeader(t *testing.T) {
	r := &http.Request{
		Header: http.Header{"Authorization": {"Bearer my-token"}},
		URL:    &url.URL{},
	}
	assert.Equal(t, "my-token", TokenFromRequest(r))
}

func TestTokenFromRequest_AuthorizationHeader_ExtraSpaces(t *testing.T) {
	r := &http.Request{
		Header: http.Header{"Authorization": {"Bearer   spaced-token  "}},
		URL:    &url.URL{},
	}
	assert.Equal(t, "spaced-token", TokenFromRequest(r))
}

func TestTokenFromRequest_QueryParam(t *testing.T) {
	r := &http.Request{
		Header: http.Header{},
		URL:    &url.URL{RawQuery: "token=query-token"},
	}
	assert.Equal(t, "query-token", TokenFromRequest(r))
}

func TestTokenFromRequest_HeaderTakesPrecedence(t *testing.T) {
	r := &http.Request{
		Header: http.Header{"Authorization": {"Bearer header-token"}},
		URL:    &url.URL{RawQuery: "token=query-token"},
	}
	assert.Equal(t, "header-token", TokenFromRequest(r))
}

func TestTokenFromRequest_NoToken(t *testing.T) {
	r := &http.Request{
		Header: http.Header{},
		URL:    &url.URL{},
	}
	assert.Equal(t, "", TokenFromRequest(r))
}

func TestTokenFromRequest_NonBearerAuth(t *testing.T) {
	r := &http.Request{
		Header: http.Header{"Authorization": {"Basic abc123"}},
		URL:    &url.URL{RawQuery: "token=fallback"},
	}
	// Non-Bearer auth header is ignored, falls back to query param.
	assert.Equal(t, "fallback", TokenFromRequest(r))
}
